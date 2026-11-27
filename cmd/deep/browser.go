// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	deep "deepprotocol"
)

func browserArgs(profile, proxy string) []string {
	return []string{
		"--user-data-dir=" + profile,
		"--proxy-server=http://" + proxy,
		"--proxy-bypass-list=<-loopback>",
		"--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--disable-quic", "--disable-background-networking", "--disable-background-mode", "--disable-sync",
		"--disable-extensions", "--disable-features=MediaRouter", "--no-first-run", "--no-default-browser-check",
	}
}

func browserBridge(ctx context.Context, client *deep.Client, uri deep.URI) (net.Listener, *http.Server, string, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, nil, "", err
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	origin := &url.URL{Scheme: "http", Host: "deep-" + hex.EncodeToString(nonce[:]) + ".localhost"}
	p := &browserProxy{client: client, authority: uri.Authority, origin: origin, slots: make(chan struct{}, 8)}
	srv := &http.Server{Handler: p, BaseContext: func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	link := origin.String() + uri.Path
	if uri.Query != "" {
		link += "?" + uri.Query
	}
	if uri.Fragment != "" {
		link += "#" + uri.Fragment
	}
	return ln, srv, link, nil
}

func browserCommand(ctx context.Context, args []string, in io.Reader, out, diagnostic io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var raw string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		raw, args = args[0], args[1:]
	}
	flags := newFlags("deep browser", diagnostic)
	config := flags.String("config", filepath.Join(filepath.Dir(exe), "config.json"), "DEEP network and adapter registry")
	desktop := flags.Bool("desktop", false, "Use the active HellNode desktop session for .hell links when enabled")
	parent := flags.Bool("parent-stdin", false, "Close the browser when the launching program closes stdin")
	proceed, err := parseFlags(flags, args)
	if err != nil || !proceed {
		return err
	}
	if raw == "" {
		return errors.New("usage: deep browser deep://node.network/ [--config client.json]")
	}
	uri, err := deep.ParseURI(raw)
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("a Linux desktop session is required to open a browser window; use deep preview in a headless terminal")
	}
	browser, err := findDesktopBrowser(ctx)
	if err != nil {
		return err
	}
	client, err := loadDesktopClient(uri, *config, *desktop)
	if err != nil {
		return err
	}
	if client.MaxBytes <= 0 || client.MaxBytes > deep.MaxApplicationResponseBytes {
		client.MaxBytes = deep.MaxApplicationResponseBytes
	}
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	if *parent {
		go func() { var b [1]byte; _, _ = in.Read(b[:]); cancel() }()
	}
	ln, srv, link, err := browserBridge(life, client, uri)
	if err != nil {
		return err
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	defer func() { cancel(); srv.Close(); <-done }()
	profile, err := os.MkdirTemp("", "deep-browser-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	browserArguments, err := browser.launchArgs(profile, ln.Addr().String(), link)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(life, browser.binary, browserArguments...)
	cmd.WaitDelay = 2 * time.Second
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("could not start the DEEP browser: %w", err)
	}
	if _, err = fmt.Fprintln(out, "DEEP browser started."); err != nil {
		cancel()
		_ = cmd.Wait()
		return err
	}
	err = cmd.Wait()
	if life.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("the DEEP browser exited: %w", err)
	}
	return nil
}
