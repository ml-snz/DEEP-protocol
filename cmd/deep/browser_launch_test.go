// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLinuxDesktopBrowserSelection(t *testing.T) {
	for desktopID, want := range map[string][]string{
		"org.mozilla.firefox.desktop\n": {"firefox"},
		"firefox-esr.desktop":           {"firefox-esr"},
		"google-chrome-beta.desktop":    {"google-chrome-beta"},
		"com.brave.Browser.desktop":     {"brave-browser", "brave-browser-stable", "brave"},
		"unrelated-firefox.desktop":     nil,
	} {
		if got := linuxBrowserCandidates(desktopID); !slices.Equal(got, want) {
			t.Errorf("default %q => %v, want %v", desktopID, got, want)
		}
	}
}

func TestBrowserDesktopDispatchArguments(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--help"},
		{"https://example.invalid"},
		{"deep://site.alpha/", "--config", "other.json"},
	} {
		if err := run(context.Background(), append([]string{"open-browser-uri"}, args...), io.Discard, io.Discard); err == nil {
			t.Fatalf("desktop dispatch accepted %q", args)
		}
	}
	exe, err := filepath.Abs(filepath.Join("DEEP Install %", "deep"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := browserDesktopEntry(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry, "\" open-browser-uri %u\n") ||
		!strings.Contains(entry, "%%") || !strings.Contains(entry, "MimeType=x-scheme-handler/deep;") {
		t.Fatalf("unexpected desktop entry: %s", entry)
	}
	if _, err := browserDesktopEntry(exe + "\nExec=bad"); err == nil {
		t.Fatal("accepted a path with a line break")
	}
}

func TestFirefoxBrowserProfileHasNoDirectFallback(t *testing.T) {
	profile := t.TempDir()
	browser := desktopBrowser{firefox: true}
	args, err := browser.launchArgs(profile, "127.0.0.1:32123", "http://deep-test.localhost/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), "-no-remote -profile "+profile+" -new-window http://deep-test.localhost/") {
		t.Fatalf("launch may reuse another Firefox profile: %q", args)
	}
	raw, err := os.ReadFile(filepath.Join(profile, "user.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pref := range []string{
		"user_pref(\"network.proxy.type\", 1);",
		"user_pref(\"network.proxy.http_port\", 32123);",
		"user_pref(\"network.proxy.ssl_port\", 32123);",
		"user_pref(\"network.proxy.allow_hijacking_localhost\", true);",
		"user_pref(\"network.proxy.allow_bypass\", false);",
		"user_pref(\"network.proxy.failover_direct\", false);",
		"user_pref(\"media.peerconnection.enabled\", false);",
	} {
		if !strings.Contains(string(raw), pref) {
			t.Fatalf("missing Firefox proxy preference %q", pref)
		}
	}
}
