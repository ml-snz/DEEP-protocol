// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const deepDesktopID = "org.deepprotocol.Browser.desktop"
const deepURLAssociation = "x-scheme-handler/deep"

func browserDesktopEntry(executable string) (string, error) {
	if !filepath.IsAbs(executable) || strings.ContainsAny(executable, "=\x00\r\n\t") {
		return "", errors.New("install DEEP at an absolute path without '=' or control characters")
	}
	// Desktop-entry string escaping is applied before Exec quoting and field
	// expansion. Only the trailing %u expands to the incoming link.
	execPath := strings.NewReplacer(
		"\\", "\\\\\\\\", "\"", "\\\\\"", "`", "\\\\`", "$", "\\\\$", "%", "%%",
	).Replace(executable)
	return fmt.Sprintf("[Desktop Entry]\nType=Application\nName=DEEP Browser\nExec=\"%s\" open-browser-uri %%u\nTerminal=false\nNoDisplay=true\nMimeType=%s;\n", execPath, deepURLAssociation), nil
}

func registerBrowser(ctx context.Context, args []string, out, diagnostic io.Writer) error {
	flags := newFlags("deep browser-register", diagnostic)
	replace := flags.Bool("replace-existing", false, "Replace another registered deep:// application")
	proceed, err := parseFlags(flags, args)
	if err != nil || !proceed {
		return err
	}
	if runtime.GOOS != "linux" {
		return errors.New("browser-register requires a Linux desktop; on Windows use scripts/register.ps1")
	}
	xdg, err := exec.LookPath("xdg-mime")
	if err != nil {
		return errors.New("install xdg-utils to register deep:// on this Linux desktop")
	}
	current, err := exec.CommandContext(ctx, xdg, "query", "default", deepURLAssociation).Output()
	if err != nil {
		return fmt.Errorf("query the desktop's DEEP handler: %w", err)
	}
	if id := strings.TrimSpace(string(current)); id != "" && id != deepDesktopID && !*replace {
		return errors.New("deep:// already has another handler; use --replace-existing to switch to the DEEP browser")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := browserDesktopEntry(executable)
	if err != nil {
		return err
	}
	dataDir := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dataDir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(dataDir, "applications")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".deep-browser-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.WriteString(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), filepath.Join(dir, deepDesktopID)); err != nil {
		return err
	}
	if err := exec.CommandContext(ctx, xdg, "default", deepDesktopID, deepURLAssociation).Run(); err != nil {
		return fmt.Errorf("register the DEEP browser with the desktop: %w", err)
	}
	check, err := exec.CommandContext(ctx, xdg, "query", "default", deepURLAssociation).Output()
	if err != nil {
		return fmt.Errorf("verify the desktop's DEEP handler: %w", err)
	}
	if !bytes.Equal(bytes.TrimSpace(check), []byte(deepDesktopID)) {
		return errors.New("the desktop did not select the DEEP Browser handler; check your desktop's default applications")
	}
	fmt.Fprintln(out, "Registered deep:// links for this desktop user.")
	fmt.Fprintln(out, "Client configuration:", filepath.Join(filepath.Dir(executable), "config.json"))
	return nil
}
