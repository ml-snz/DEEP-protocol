// SPDX-License-Identifier: Apache-2.0
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	deep "deepprotocol"
)

func TestDesktopConfigOnlyOverridesHellWhenRequested(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config.json")
	if err = os.WriteFile(config, []byte("{\"version\":2,\"proxy\":{\"address\":\"127.0.0.1:4747\"}}"), 0600); err != nil {
		t.Fatal(err)
	}
	test := func(network string, enabled bool, want string) {
		t.Helper()
		client, err := loadDesktopClient(deep.URI{Network: network}, config, enabled)
		if want == "" {
			if err != nil || client == nil {
				t.Fatalf("%s: %v", network, err)
			}
			return
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: got %v, want %q", network, err, want)
		}
	}
	test("hell", true, "")
	file := filepath.Join(base, "DEEP", "hellnet-browser", "active.json")
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, []byte("{\"version\":1,\"address\":\"192.0.2.1:443\",\"pin_sha256\":\""+strings.Repeat("0", 64)+"\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	test("hell", true, "numeric loopback")
	test("hell", false, "")
	test("alpha", true, "")
}
