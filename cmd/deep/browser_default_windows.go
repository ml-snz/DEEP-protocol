// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Ask the Windows shell for the effective per-user HTTPS association.
// Retrieve only the executable, as the association's normal arguments can
// route the link into the browser's regular profile.
func defaultWindowsBrowser(ctx context.Context) string {
	const assocfIsProtocol = 0x1000
	const assocstrExecutable = 2

	windows := os.Getenv("SystemRoot")
	if ctx.Err() != nil || !filepath.IsAbs(windows) {
		return ""
	}
	query := syscall.NewLazyDLL(filepath.Join(windows, "System32", "shlwapi.dll")).NewProc("AssocQueryStringW")
	if err := query.Find(); err != nil {
		return ""
	}
	scheme := [...]uint16{'h', 't', 't', 'p', 's', 0}
	var path [32768]uint16
	length := uint32(len(path))
	result, _, _ := query.Call(assocfIsProtocol, assocstrExecutable,
		uintptr(unsafe.Pointer(&scheme[0])), 0, uintptr(unsafe.Pointer(&path[0])), uintptr(unsafe.Pointer(&length)))
	if result != 0 || length == 0 || length > uint32(len(path)) {
		return ""
	}
	return syscall.UTF16ToString(path[:length])
}
