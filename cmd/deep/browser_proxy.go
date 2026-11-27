// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	deep "deepprotocol"
)

const browserPolicy = "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; media-src 'self'; connect-src 'self'; " +
	"object-src 'none'; frame-src 'none'; worker-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

type browserProxy struct {
	client    *deep.Client
	authority string
	origin    *url.URL
	slots     chan struct{}
}

func (p *browserProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", browserPolicy)
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), display-capture=()")
	// This listener only accepts absolute-form proxy requests for the one
	// random origin of the current window. It never tunnels or forwards HTTP.
	if r.Method == http.MethodConnect || r.URL.Scheme != p.origin.Scheme ||
		r.URL.Host != p.origin.Host || r.Host != p.origin.Host || r.URL.User != nil ||
		r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Upgrade") != "" {
		http.Error(w, "This window only serves its DEEP site.", http.StatusForbidden)
		return
	}
	if !deep.ValidApplicationMethod(r.Method) {
		http.Error(w, "Unsupported DEEP method.", http.StatusMethodNotAllowed)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != p.origin.String() {
		http.Error(w, "Cross-origin DEEP request refused.", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != p.origin.String() {
		http.Error(w, "A same-origin browser request is required.", http.StatusForbidden)
		return
	}
	uri := "deep://" + p.authority + r.URL.RequestURI()
	if _, err := deep.ParseURI(uri); err != nil {
		http.Error(w, "Invalid DEEP resource.", http.StatusBadRequest)
		return
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		http.Error(w, "Too many active DEEP requests.", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, deep.MaxApplicationRequestBytes))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "DEEP request exceeds 1 MiB.", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Could not read browser request.", http.StatusBadRequest)
		return
	}
	headers := p.requestHeaders(r.Header)
	if err := deep.ValidateApplicationHeaders(headers); err != nil {
		http.Error(w, "Request headers exceed DEEP limits.", http.StatusRequestHeaderFieldsTooLarge)
		return
	}
	request := deep.ApplicationRequest{Method: r.Method, Headers: headers, Body: body}
	var payload bytes.Buffer
	result, err := exchangeBrowserResource(r.Context(), p.client, uri, request, &payload)
	if err != nil {
		var remote *deep.RemoteError
		if errors.As(err, &remote) && remote.Code == "NOT_FOUND" {
			http.Error(w, "DEEP resource unavailable.", http.StatusNotFound)
			return
		}
		message := "DEEP could not verify the response."
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			message += " For a failed submission, check whether it succeeded before retrying."
		} else if strings.HasSuffix(p.authority, ".hell") {
			message += " Check that HellNode is connected, desktop browser links are enabled, and the site is available."
		}
		http.Error(w, message, http.StatusBadGateway)
		return
	}
	p.writeResponse(w, r, result, payload.Bytes())
}

func exchangeBrowserResource(ctx context.Context, client *deep.Client, uri string, request deep.ApplicationRequest, body *bytes.Buffer) (deep.ApplicationResult, error) {
	result, err := client.Exchange(ctx, uri, request, body)
	var remote *deep.RemoteError
	if request.Method == http.MethodGet && errors.As(err, &remote) && remote.Code == "UNSUPPORTED_OPERATION" {
		body.Reset()
		result.Result, err = client.Fetch(ctx, uri, body)
		result.Status = http.StatusOK
	}
	return result, err
}

func (p *browserProxy) requestHeaders(h http.Header) []deep.Header {
	headers := make([]deep.Header, 0, len(h))
	for name, values := range h {
		name = strings.ToLower(name)
		switch name {
		case "accept", "authorization", "content-type", "cookie", "if-match", "if-none-match", "if-modified-since", "if-unmodified-since", "range", "x-csrf-token", "x-csrftoken", "x-requested-with":
			for _, v := range values {
				headers = append(headers, deep.Header{Name: name, Value: v})
			}
		case "origin", "referer":
			for _, v := range values {
				u, err := url.Parse(v)
				if err == nil && u.Scheme == p.origin.Scheme && u.Host == p.origin.Host && u.User == nil {
					u.Scheme, u.Host = "deep", p.authority
					headers = append(headers, deep.Header{Name: name, Value: u.String()})
				}
			}
		}
	}
	return headers
}

func (p *browserProxy) redirect(r *http.Request, loc string) (string, bool) {
	ref, err := url.Parse(loc)
	if err != nil || ref.User != nil || strings.ContainsAny(loc, "\\\r\n") {
		return "", false
	}
	base := *r.URL
	base.Scheme, base.Host = "deep", p.authority
	target := base.ResolveReference(ref)
	if target.Scheme != "deep" || !strings.EqualFold(target.Host, p.authority) {
		return "", false
	}
	if _, err := deep.ParseURI(target.String()); err != nil {
		return "", false
	}
	target.Scheme, target.Host = p.origin.Scheme, p.origin.Host
	return target.String(), true
}

func (p *browserProxy) writeResponse(w http.ResponseWriter, r *http.Request, res deep.ApplicationResult, body []byte) {
	h := w.Header()
	for _, f := range res.Headers {
		if f.Name == "location" {
			target, ok := p.redirect(r, f.Value)
			if !ok && res.Status >= 300 && res.Status < 400 && res.Status != http.StatusNotModified {
				http.Error(w, "Redirect to another origin refused.", http.StatusBadGateway)
				return
			}
			if ok {
				h.Set("Location", target)
			}
		}
	}
	for _, f := range res.Headers {
		switch f.Name {
		case "set-cookie":
			c, err := http.ParseSetCookie(f.Value)
			if err != nil || (c.Domain != "" && !strings.EqualFold(strings.TrimPrefix(c.Domain, "."), p.authority)) {
				continue
			}
			c.Domain = ""
			http.SetCookie(w, c)
		case "content-disposition", "content-language", "content-range", "accept-ranges", "etag", "last-modified", "content-security-policy":
			h.Add(f.Name, f.Value)
		}
	}
	h.Set("Content-Type", res.MediaType)
	w.WriteHeader(res.Status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
