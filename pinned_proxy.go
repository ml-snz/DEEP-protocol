// SPDX-License-Identifier: Apache-2.0
package deep

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"time"
)

type pinnedProxyAdapter struct {
	address string
	dial    func(context.Context, string, string) (net.Conn, error)
}

func (p pinnedProxyAdapter) Resolve(ctx context.Context, authority string) (Endpoint, error) {
	if err := ValidateAuthority(authority); err != nil {
		return Endpoint{}, err
	}
	c, response, err := proxyHandshakeWithDialer(ctx, p.address, 25*time.Second, ProxyRequest{
		Version: 1, Operation: "resolve", Authority: authority,
	}, p.dial)
	if err != nil {
		return Endpoint{}, err
	}
	c.Close()
	return Endpoint{Address: proxyEndpointAddress(p.address, authority), PinSHA256: response.PinSHA256, Transport: ProxyTransport}, nil
}

// NewPinnedProxyClient uses Proxy/1 over a server-authenticated TLS connection
// to a loopback listener. It trusts the exact SPKI of the local proxy and then
// checks the DEEP site's own identity inside the resulting stream.
func NewPinnedProxyClient(network, address, pin string) (*Client, error) {
	if err := ValidateProxyAddress(address); err != nil {
		return nil, err
	}
	rawPin, err := hex.DecodeString(pin)
	if err != nil || len(rawPin) != sha256.Size {
		return nil, errors.New("invalid local proxy TLS pin")
	}
	expected := [sha256.Size]byte(rawPin)
	dial := (&tls.Dialer{Config: &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // This loopback endpoint is authenticated by its explicit SPKI pin.
		VerifyConnection: func(s tls.ConnectionState) error {
			if len(s.PeerCertificates) != 1 || sha256.Sum256(s.PeerCertificates[0].RawSubjectPublicKeyInfo) != expected {
				return errors.New("local proxy TLS identity changed")
			}
			return nil
		},
	}}).DialContext
	registry := NewRegistry()
	if err := registry.Register(network, pinnedProxyAdapter{address, dial}); err != nil {
		return nil, err
	}
	client := NewClient(registry)
	client.Timeout = 55 * time.Second
	client.Dialers[ProxyTransport] = func(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
		target, authority, err := parseProxyEndpoint(endpoint)
		if err != nil {
			return nil, err
		}
		if target != address {
			return nil, errors.New("local proxy endpoint changed")
		}
		c, _, err := proxyHandshakeWithDialer(ctx, address, 25*time.Second, ProxyRequest{
			Version: 1, Operation: "connect", Authority: authority, PinSHA256: strings.ToLower(endpoint.PinSHA256),
		}, dial)
		return c, err
	}
	return client, nil
}
