// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	deep "deepprotocol"
)

func testBrowserBridge(t *testing.T) (string, string, *atomic.Int32) {
	t.Helper()
	var posts atomic.Int32
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "verified", Path: "/", Domain: "test.alpha", Secure: true, HttpOnly: true})
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><link rel="stylesheet" href="/site.css"><p id="check">waiting</p><script src="/site.js"></script>`)
		case "/site.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, `#check { color: rgb(23, 45, 67); }`)
		case "/site.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `const x = new XMLHttpRequest(); x.open("POST","/form",false);
x.setRequestHeader("Content-Type","application/x-www-form-urlencoded"); x.send("test=ok");
const p = document.querySelector("#check");
if (getComputedStyle(p).color === "rgb(23, 45, 67)" && x.responseText === "submit-ok")
    p.textContent = "DEEP browser rendered HTML CSS and submitted with a cookie";`)
		case "/form":
			posts.Add(1)
			c, e := r.Cookie("session")
			if r.Method != "POST" || r.Header.Get("Origin") != "http://"+r.Host || r.FormValue("test") != "ok" || e != nil || c.Value != "verified" {
				http.Error(w, "wrong form or origin", http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, "submit-ok")
		case "/next":
			http.Redirect(w, r, "deep://test.alpha/site.css", http.StatusSeeOther)
		case "/external":
			http.Redirect(w, r, "https://example.invalid/", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(app.Close)
	dir := newTestNode(t, true)
	cfg, err := loadServerConfig(filepath.Join(dir, "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.LoadX509KeyPair(cfg.Certificate, cfg.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := deep.IdentityPin(cert)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := deep.NewHTTPHandler(app.URL, cfg.Authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { upstream.Close() })
	wire, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	life, cancel := context.WithCancel(context.Background())
	server := &deep.Server{Authority: cfg.Authority, TLSConfig: deep.ServerTLSConfig(cert), Handler: upstream}
	wireDone := make(chan error, 1)
	go func() { wireDone <- server.Serve(life, wire) }()
	t.Cleanup(func() {
		cancel()
		wire.Close()
		if err := <-wireDone; err != nil {
			t.Error(err)
		}
	})
	nativeProxy := testNativeBrowserProxy(t, wire.Addr().String(), pin)
	config := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, "{\"version\":2,\"proxy\":{\"address\":%q}}", nativeProxy), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := deep.LoadClientConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	client.MaxBytes = deep.MaxApplicationResponseBytes
	uri, err := deep.ParseURI("deep://test.alpha/")
	if err != nil {
		t.Fatal(err)
	}
	ln, srv, link, err := browserBridge(life, client, uri)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return link, ln.Addr().String(), &posts
}

func testNativeBrowserProxy(t *testing.T, upstream, pin string) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer c.Close()
				var req deep.ProxyRequest
				if deep.ReadProxyMessage(c, &req) != nil || req.Version != 1 || req.Authority != "test.alpha" {
					return
				}
				switch req.Operation {
				case "resolve":
					deep.WriteProxyMessage(c, deep.ProxyResponse{Version: 1, OK: true, PinSHA256: pin})
				case "connect":
					if req.PinSHA256 != pin {
						return
					}
					target, err := net.DialTimeout("tcp", upstream, 3*time.Second)
					if err != nil {
						return
					}
					defer target.Close()
					if deep.WriteProxyMessage(c, deep.ProxyResponse{Version: 1, OK: true, PinSHA256: pin}) != nil {
						return
					}
					done := make(chan struct{})
					go func() { defer close(done); io.Copy(target, c); target.Close() }()
					io.Copy(c, target)
					c.Close()
					<-done
				}
			})
		}
	}()
	t.Cleanup(func() { ln.Close(); <-done; workers.Wait() })
	return ln.Addr().String()
}

func TestBrowserBridgeRestrictsRoutesAndTranslatesResponses(t *testing.T) {
	site, proxy, posts := testBrowserBridge(t)
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: proxy})}
	defer transport.CloseIdleConnections()
	c := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, uri, origin string, status int) *http.Response {
		t.Helper()
		r, err := http.NewRequest(method, uri, nil)
		if err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		res, err := c.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("%s %s status %d, want %d", method, uri, res.StatusCode, status)
		}
		return res
	}
	response := request("GET", site, "", 200)
	if cs := response.Cookies(); len(cs) != 1 || cs[0].Domain != "" || !cs[0].Secure || !cs[0].HttpOnly {
		t.Fatal("cookie scoping was lost")
	}
	response = request("GET", site+"next", "", 303)
	if response.Header.Get("Location") != site+"site.css" {
		t.Fatal("wrong redirect", response.Header.Get("Location"))
	}
	request("GET", site+"external", "", 502)
	request("GET", "http://127.0.0.1:12/", "", 403)
	request("GET", "http://other.deep.localhost/", "", 403)
	request("POST", site+"form", "http://different.localhost", 403)
	if posts.Load() != 0 {
		t.Fatal("cross-origin form reached the site")
	}
	if _, err := c.Get("https://example.invalid/"); err == nil {
		t.Fatal("CONNECT was accepted")
	}
}

func TestBrowserWithChromium(t *testing.T) {
	binary := os.Getenv("DEEP_TEST_BROWSER")
	if binary == "" {
		t.Skip("set DEEP_TEST_BROWSER to a Chromium executable")
	}
	site, proxy, posts := testBrowserBridge(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	dump := func(addr string) (string, error) {
		profile := filepath.Join(t.TempDir(), "profile")
		args := append(browserArgs(profile, proxy), "--headless", "--disable-gpu", "--dump-dom", addr)
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.WaitDelay = 2 * time.Second
		data, err := cmd.CombinedOutput()
		return string(data), err
	}
	data, err := dump(site)
	if err != nil || !strings.Contains(data, `id="check">DEEP browser rendered HTML CSS and submitted with a cookie</p>`) || posts.Load() != 1 {
		t.Fatalf("Chromium did not render or submit through DEEP: %v\n%s", err, data)
	}
	var leaks atomic.Int32
	trap := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaks.Add(1) }))
	defer trap.Close()
	_, _ = dump(trap.URL)
	if leaks.Load() != 0 {
		t.Fatal("Chromium bypassed the bridge for a loopback destination")
	}
}

func TestBrowserWithFirefox(t *testing.T) {
	binary := os.Getenv("DEEP_TEST_FIREFOX")
	if binary == "" {
		t.Skip("set DEEP_TEST_FIREFOX to a Firefox executable")
	}
	site, proxy, posts := testBrowserBridge(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	render := func(link string) error {
		profile := t.TempDir()
		if err := writeFirefoxProfile(profile, proxy); err != nil {
			return err
		}
		screenshot := filepath.Join(profile, "page.png")
		cmd := exec.CommandContext(ctx, binary, "-no-remote", "-profile", profile, "-headless", "-screenshot", screenshot, link)
		cmd.WaitDelay = 2 * time.Second
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("Firefox screenshot: %w\n%s", err, output)
		}
		_, err := os.Stat(screenshot)
		return err
	}
	if err := render(site); err != nil || posts.Load() != 1 {
		t.Fatalf("Firefox did not render and submit through the DEEP proxy: %v (submissions: %d)", err, posts.Load())
	}
	var leaks atomic.Int32
	trap := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaks.Add(1) }))
	defer trap.Close()
	_ = render(trap.URL)
	if leaks.Load() != 0 {
		t.Fatal("Firefox bypassed the bridge for a loopback destination")
	}
}
