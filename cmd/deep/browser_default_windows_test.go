// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"testing"
)

func TestWindowsUsesSupportedDefaultBrowser(t *testing.T) {
	t.Setenv("DEEP_BROWSER", "")
	path := defaultWindowsBrowser(t.Context())
	want, supported := knownWindowsBrowser(path)
	if !supported {
		t.Skip("Windows has no supported default browser")
	}
	got, err := findDesktopBrowser(context.Background())
	if err != nil || got != want {
		t.Fatalf("selected %+v (%v); expected the default browser %+v", got, err, want)
	}
	t.Logf("Selected configured Windows browser %s", got.binary)
}
