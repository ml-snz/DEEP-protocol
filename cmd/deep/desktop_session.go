// SPDX-License-Identifier: Apache-2.0
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	deep "deepprotocol"
)

func loadDesktopClient(uri deep.URI, config string, desktop bool) (*deep.Client, error) {
	if desktop && uri.Network == "hell" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, "DEEP", "hellnet-browser", "active.json")
		client, found, err := readDesktopSession(path)
		if err != nil {
			return nil, fmt.Errorf("active HellNode browser session: %w", err)
		}
		if found {
			return client, nil
		}
	}
	return deep.LoadClientConfig(config)
}

func readDesktopSession(path string) (*deep.Client, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("session descriptor must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(info, opened) {
		return nil, false, errors.New("session descriptor changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, false, err
	}
	if len(raw) > 4096 {
		return nil, false, errors.New("session descriptor exceeds 4 KiB")
	}
	var session struct {
		Version int    `json:"version"`
		Address string `json:"address"`
		Pin     string `json:"pin_sha256"`
	}
	if err := deep.DecodeConfigJSON(raw, &session); err != nil {
		return nil, false, err
	}
	if session.Version != 1 {
		return nil, false, errors.New("unsupported session descriptor version")
	}
	client, err := deep.NewPinnedProxyClient("hell", session.Address, session.Pin)
	return client, true, err
}
