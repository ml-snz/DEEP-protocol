// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type desktopBrowser struct {
	binary  string
	firefox bool
}

func findDesktopBrowser(ctx context.Context) (desktopBrowser, error) {
	if path := os.Getenv("DEEP_BROWSER"); path != "" {
		if !filepath.IsAbs(path) {
			return desktopBrowser{}, errors.New("DEEP_BROWSER must be an absolute browser executable path")
		}
		binary, err := exec.LookPath(path)
		firefox := strings.HasPrefix(strings.ToLower(filepath.Base(path)), "firefox")
		return desktopBrowser{binary, firefox}, err
	}
	chromium := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "brave-browser", "brave-browser-stable", "brave", "microsoft-edge", "microsoft-edge-stable", "chrome", "msedge"}
	firefox := []string{"firefox", "firefox-esr"}
	if runtime.GOOS == "windows" {
		if browser, ok := knownWindowsBrowser(defaultWindowsBrowser(ctx)); ok {
			return browser, nil
		}
	}
	if runtime.GOOS == "linux" {
		lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		name, _ := exec.CommandContext(lookup, "xdg-settings", "get", "default-web-browser").Output()
		for _, candidate := range linuxBrowserCandidates(string(name)) {
			if path, err := exec.LookPath(candidate); err == nil {
				return desktopBrowser{path, strings.HasPrefix(candidate, "firefox")}, nil
			}
		}
	}
	for _, candidate := range append(chromium, firefox...) {
		if path, err := exec.LookPath(candidate); err == nil {
			return desktopBrowser{path, strings.HasPrefix(candidate, "firefox")}, nil
		}
	}
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			for _, sub := range []string{"Google/Chrome/Application/chrome.exe", "BraveSoftware/Brave-Browser/Application/brave.exe", "Microsoft/Edge/Application/msedge.exe", "Mozilla Firefox/firefox.exe"} {
				if path, err := exec.LookPath(filepath.Join(base, sub)); err == nil {
					return desktopBrowser{path, strings.HasSuffix(sub, "/firefox.exe")}, nil
				}
			}
		}
	}
	if runtime.GOOS == "darwin" {
		for _, app := range []string{"Google Chrome", "Chromium", "Brave Browser", "Microsoft Edge", "Firefox"} {
			if path, err := exec.LookPath(filepath.Join("/Applications", app+".app", "Contents", "MacOS", app)); err == nil {
				return desktopBrowser{path, app == "Firefox"}, nil
			}
		}
	}
	return desktopBrowser{}, errors.New("install Firefox, Chrome, Chromium, Brave or Edge, or set DEEP_BROWSER to its absolute executable path")
}

// Use only browser executables which accept DEEP's isolated launch settings.
func knownWindowsBrowser(path string) (desktopBrowser, bool) {
	if !filepath.IsAbs(path) {
		return desktopBrowser{}, false
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "firefox.exe", "chrome.exe", "chromium.exe", "brave.exe", "msedge.exe":
		binary, err := exec.LookPath(path)
		return desktopBrowser{binary, strings.EqualFold(filepath.Base(path), "firefox.exe")}, err == nil
	default:
		return desktopBrowser{}, false
	}
}

func linuxBrowserCandidates(entry string) []string {
	// Resolve only known desktop IDs to known executables. Running a desktop
	// file directly could start a regular profile without DEEP's restrictions.
	switch strings.ToLower(strings.TrimSpace(entry)) {
	case "firefox.desktop", "firefox_firefox.desktop", "org.mozilla.firefox.desktop":
		return []string{"firefox"}
	case "firefox-esr.desktop":
		return []string{"firefox-esr"}
	case "google-chrome.desktop", "google-chrome-stable.desktop", "com.google.chrome.desktop":
		return []string{"google-chrome-stable", "google-chrome"}
	case "google-chrome-beta.desktop":
		return []string{"google-chrome-beta"}
	case "google-chrome-unstable.desktop":
		return []string{"google-chrome-unstable"}
	case "chromium.desktop", "chromium-browser.desktop", "chromium_chromium.desktop", "org.chromium.chromium.desktop":
		return []string{"chromium", "chromium-browser"}
	case "brave-browser.desktop", "brave-browser-stable.desktop", "com.brave.browser.desktop":
		return []string{"brave-browser", "brave-browser-stable", "brave"}
	case "brave-browser-beta.desktop":
		return []string{"brave-browser-beta"}
	case "brave-browser-nightly.desktop":
		return []string{"brave-browser-nightly"}
	case "microsoft-edge.desktop", "microsoft-edge-stable.desktop", "com.microsoft.edge.desktop":
		return []string{"microsoft-edge-stable", "microsoft-edge", "msedge"}
	case "microsoft-edge-beta.desktop":
		return []string{"microsoft-edge-beta"}
	case "microsoft-edge-dev.desktop":
		return []string{"microsoft-edge-dev"}
	default:
		return nil
	}
}

func (b desktopBrowser) launchArgs(profile, proxy, link string) ([]string, error) {
	if !b.firefox {
		// Use a normal window so the user can open another deep:// address
		// from its address bar. The existing proxy is still site-specific.
		return append(browserArgs(profile, proxy), "--new-window", link), nil
	}
	if err := writeFirefoxProfile(profile, proxy); err != nil {
		return nil, err
	}
	args := []string{"-no-remote", "-profile", profile, "-new-window", link}
	if runtime.GOOS == "windows" {
		args = append([]string{"-wait-for-browser"}, args...)
	}
	return args, nil
}

func writeFirefoxProfile(profile, proxy string) error {
	host, rawPort, err := net.SplitHostPort(proxy)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return err
	}
	// Explicit HTTP and HTTPS proxying makes disallowed connections hit
	// the bridge's rejection path, including destinations on localhost.
	prefs := []struct {
		key   string
		value any
	}{
		{"network.proxy.type", 1},
		{"network.proxy.http", host},
		{"network.proxy.http_port", port},
		{"network.proxy.ssl", host},
		{"network.proxy.ssl_port", port},
		{"network.proxy.no_proxies_on", ""},
		{"network.proxy.allow_hijacking_localhost", true},
		{"network.proxy.allow_bypass", false},
		{"network.proxy.failover_direct", false},
		{"network.trr.mode", 5},
		{"network.dns.disablePrefetch", true},
		{"network.prefetch-next", false},
		{"network.predictor.enabled", false},
		{"network.http.speculative-parallel-limit", 0},
		{"network.http.http3.enable", false},
		{"media.peerconnection.enabled", false},
		{"dom.serviceWorkers.enabled", false},
		{"browser.shell.checkDefaultBrowser", false},
		{"browser.startup.homepage_override.mstone", "ignore"},
		{"browser.aboutwelcome.enabled", false},
		{"browser.search.suggest.enabled", false},
		{"browser.urlbar.suggest.searches", false},
		{"browser.urlbar.speculativeConnect.enabled", false},
		{"network.captive-portal-service.enabled", false},
		{"network.connectivity-service.enabled", false},
		{"datareporting.policy.dataSubmissionEnabled", false},
		{"toolkit.telemetry.enabled", false},
	}
	var config strings.Builder
	for _, pref := range prefs {
		value, err := json.Marshal(pref.value)
		if err != nil {
			return err
		}
		fmt.Fprintf(&config, "user_pref(%q, %s);\n", pref.key, value)
	}
	return os.WriteFile(filepath.Join(profile, "user.js"), []byte(config.String()), 0600)
}
