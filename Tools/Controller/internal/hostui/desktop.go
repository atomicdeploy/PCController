package hostui

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pccontroller.local/controller/winres"
)

const ToastLogoFileName = "toast-logo.png"

var toastEventIconNames = [...]string{
	NotificationIconInfo,
	NotificationIconWarning,
	NotificationIconError,
	NotificationIconDoor,
	NotificationIconHot,
	NotificationIconTemperatureCleared,
	NotificationIconMotion,
	NotificationIconRelay,
	NotificationIconRadio,
}

func toastEventIconFileName(icon string) string { return "toast-event-" + icon + ".png" }

type DesktopIntegrationOptions struct {
	AppID       string
	DisplayName string
	// Executable selects the exact owned host binary. Empty preserves the
	// existing behavior and resolves the currently running executable.
	Executable string
}

type DesktopIntegrationStatus struct {
	// Shortcut/ShortcutReady describe the Start Menu entry; the actual user
	// Desktop is reported separately. Neither field claims a taskbar pin.
	Supported            bool     `json:"supported"`
	ProtocolReady        bool     `json:"protocol_ready"`
	ShortcutReady        bool     `json:"shortcut_ready"`
	DesktopShortcutReady bool     `json:"desktop_shortcut_ready"`
	Executable           string   `json:"executable,omitempty"`
	Shortcut             string   `json:"shortcut,omitempty"`
	DesktopShortcut      string   `json:"desktop_shortcut,omitempty"`
	Skipped              []string `json:"skipped,omitempty"`
	Logo                 string   `json:"logo,omitempty"`
	LastError            string   `json:"last_error,omitempty"`
}

// ResolveToastLogoPath prefers an explicit assets/toast override. Windows needs
// a file URI, so built-in artwork is materialized in a content-addressed cache.
func ResolveToastLogoPath(executable string) (string, error) {
	return resolveToastAsset(executable, ToastLogoFileName)
}

func resolveToastAsset(executable, name string) (string, error) {
	resolved, err := resolveDesktopExecutable(executable)
	if err != nil {
		return "", err
	}
	override := filepath.Join(filepath.Dir(resolved), "assets", "toast", name)
	if _, err := os.Lstat(override); err == nil {
		return override, nil // The notifier validates file type, bounds and PNG signature.
	} else if !os.IsNotExist(err) {
		return "", err
	}
	embeddedName := name
	if name == ToastLogoFileName {
		embeddedName = "icon.png"
	}
	data, err := winres.ToastImages.ReadFile(embeddedName)
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(cache, "PCController", "assets", "toast", fmt.Sprintf("%x", sha256.Sum256(data)))
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, name)
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return path, nil
	}
	file, err := os.CreateTemp(directory, ".toast-*.png")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

// DesktopIntegrationCleanupStatus reports only artifacts that were positively
// identified as belonging to this executable. A false Removed field can mean
// either that the artifact was already absent or that it was deliberately
// preserved because ownership could not be established; Skipped distinguishes
// the latter case.
type DesktopIntegrationCleanupStatus struct {
	// ShortcutRemoved is the Start Menu link. DesktopShortcutRemoved is the
	// separately owned link in the user's Desktop known folder.
	Supported              bool     `json:"supported"`
	ProtocolRemoved        bool     `json:"protocol_removed"`
	AppIdentityRemoved     bool     `json:"app_identity_removed"`
	ShortcutRemoved        bool     `json:"shortcut_removed"`
	DesktopShortcutRemoved bool     `json:"desktop_shortcut_removed"`
	Shortcut               string   `json:"shortcut,omitempty"`
	DesktopShortcut        string   `json:"desktop_shortcut,omitempty"`
	Skipped                []string `json:"skipped,omitempty"`
	LastError              string   `json:"last_error,omitempty"`
}

func EnsureDesktopIntegration(options DesktopIntegrationOptions) (DesktopIntegrationStatus, error) {
	return ensurePlatformDesktopIntegration(options)
}

// RemoveDesktopIntegration removes the current executable's per-user desktop
// integration. It is intentionally conservative and idempotent: registrations
// or shortcuts owned by another executable are left untouched.
func RemoveDesktopIntegration(
	options DesktopIntegrationOptions,
) (DesktopIntegrationCleanupStatus, error) {
	return removePlatformDesktopIntegration(options)
}

func resolveDesktopExecutable(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		var err error
		value, err = os.Executable()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(value)
}
