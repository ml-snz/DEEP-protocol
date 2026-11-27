//go:build !windows

// SPDX-License-Identifier: Apache-2.0
package main

import "context"

func defaultWindowsBrowser(context.Context) string { return "" }
