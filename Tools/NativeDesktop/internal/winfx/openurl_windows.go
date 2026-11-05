//go:build windows

package winfx

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const shellShowNormal = 1

var (
	openURLShell32      = windows.NewLazySystemDLL("shell32.dll")
	openURLShellExecute = openURLShell32.NewProc("ShellExecuteW")
)

// OpenURL opens one ordinary HTTP(S) URL using the user's registered browser.
// Validation deliberately excludes file paths, custom URI handlers,
// credentials, and control characters because callers use this for local
// application surfaces rather than arbitrary shell execution.
func OpenURL(raw string) error {
	normalized, err := validateOpenURL(raw)
	if err != nil {
		return err
	}
	if err := openURLShellExecute.Find(); err != nil {
		return fmt.Errorf("ShellExecuteW is unavailable: %w", err)
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(normalized)
	if err != nil {
		return err
	}
	result, _, callErr := openURLShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		shellShowNormal,
	)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(target)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW failed with code %d: %w", result, callErr)
	}
	return nil
}

func validateOpenURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.IndexFunc(raw, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) >= 0 {
		return "", fmt.Errorf("invalid browser URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse browser URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("browser URL must use HTTP or HTTPS")
	}
	if parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("browser URL must contain a host and no credentials")
	}
	return parsed.String(), nil
}
