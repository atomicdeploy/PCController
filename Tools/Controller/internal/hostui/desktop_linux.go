//go:build linux

package hostui

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"pccontroller.local/controller/internal/productidentity"
)

var linuxDesktopExecutable = os.Executable

var linuxDesktopRun = func(name string, arguments ...string) ([]byte, error) {
	return exec.Command(name, arguments...).CombinedOutput()
}

func ensurePlatformDesktopIntegration(
	options DesktopIntegrationOptions,
) (DesktopIntegrationStatus, error) {
	appID, displayName, err := linuxDesktopIdentity(options)
	if err != nil {
		return DesktopIntegrationStatus{Supported: true, LastError: err.Error()}, err
	}
	executable, err := linuxExecutablePath()
	if err != nil {
		return DesktopIntegrationStatus{Supported: true, LastError: err.Error()}, err
	}
	applications, err := linuxApplicationsDirectory()
	if err != nil {
		return DesktopIntegrationStatus{Supported: true, Executable: executable, LastError: err.Error()}, err
	}
	if err := os.MkdirAll(applications, 0o755); err != nil {
		return DesktopIntegrationStatus{Supported: true, Executable: executable, LastError: err.Error()}, err
	}
	desktopName := strings.ToLower(appID) + ".desktop"
	desktopPath := filepath.Join(applications, desktopName)
	status := DesktopIntegrationStatus{Supported: true, Executable: executable, Shortcut: desktopPath}
	if existing, readErr := os.ReadFile(desktopPath); readErr == nil && !linuxDesktopOwned(existing, executable) {
		err := errors.New("existing Linux desktop entry is not owned by this executable")
		status.LastError = err.Error()
		return status, err
	} else if readErr != nil && !os.IsNotExist(readErr) {
		status.LastError = readErr.Error()
		return status, readErr
	}
	content := linuxDesktopEntry(appID, displayName, executable)
	if err := atomicWriteFile(desktopPath, []byte(content), 0o644); err != nil {
		status.LastError = err.Error()
		return status, err
	}
	status.ShortcutReady = true
	output, err := linuxDesktopRun("xdg-mime", "default", desktopName, "x-scheme-handler/"+productidentity.ProtocolScheme)
	if err != nil {
		err = fmt.Errorf("register XDG protocol handler: %w%s", err, boundedDesktopOutput(output))
		status.LastError = err.Error()
		return status, err
	}
	status.ProtocolReady = true
	return status, nil
}

func removePlatformDesktopIntegration(
	options DesktopIntegrationOptions,
) (DesktopIntegrationCleanupStatus, error) {
	appID, _, err := linuxDesktopIdentity(options)
	if err != nil {
		return DesktopIntegrationCleanupStatus{Supported: true, LastError: err.Error()}, err
	}
	executable, err := linuxExecutablePath()
	if err != nil {
		return DesktopIntegrationCleanupStatus{Supported: true, LastError: err.Error()}, err
	}
	applications, err := linuxApplicationsDirectory()
	if err != nil {
		return DesktopIntegrationCleanupStatus{Supported: true, LastError: err.Error()}, err
	}
	desktopName := strings.ToLower(appID) + ".desktop"
	desktopPath := filepath.Join(applications, desktopName)
	status := DesktopIntegrationCleanupStatus{Supported: true, Shortcut: desktopPath}
	info, statErr := os.Lstat(desktopPath)
	if os.IsNotExist(statErr) {
		return status, nil
	}
	if statErr != nil {
		status.LastError = statErr.Error()
		return status, statErr
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		status.Skipped = append(status.Skipped, "desktop-entry-not-a-regular-file")
		return status, nil
	}
	content, err := os.ReadFile(desktopPath)
	if err != nil {
		status.LastError = err.Error()
		return status, err
	}
	if !linuxDesktopOwned(content, executable) {
		status.Skipped = append(status.Skipped, "desktop-entry-not-owned")
		return status, nil
	}
	if err := os.Remove(desktopPath); err != nil {
		status.LastError = err.Error()
		return status, err
	}
	status.ShortcutRemoved = true
	status.AppIdentityRemoved = true
	removed, cleanupErr := removeLinuxMimeAssociation(desktopName)
	status.ProtocolRemoved = removed || status.ShortcutRemoved
	if cleanupErr != nil {
		status.LastError = cleanupErr.Error()
	}
	return status, cleanupErr
}

func linuxDesktopIdentity(options DesktopIntegrationOptions) (string, string, error) {
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		appID = productidentity.StableAppID
	}
	if appID == "" || len(appID) > 128 {
		return "", "", errors.New("desktop application ID must contain 1..128 characters")
	}
	for _, character := range appID {
		if !(unicode.IsLetter(character) || unicode.IsDigit(character) || character == '.' || character == '_' || character == '-') {
			return "", "", errors.New("desktop application ID may contain only letters, digits, dot, underscore, or hyphen")
		}
	}
	displayName := productidentity.Title(options.DisplayName)
	if displayName == "" || len([]rune(displayName)) > 128 || strings.ContainsAny(displayName, "\r\n\x00") {
		return "", "", errors.New("desktop display name must be one line with 1..128 characters")
	}
	return appID, displayName, nil
}

func linuxExecutablePath() (string, error) {
	executable, err := linuxDesktopExecutable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	return filepath.Clean(executable), nil
}

func linuxApplicationsDirectory() (string, error) {
	dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", errors.New("XDG_DATA_HOME and the current user's home directory are unavailable")
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataHome, err := filepath.Abs(dataHome)
	if err != nil {
		return "", err
	}
	return filepath.Join(dataHome, "applications"), nil
}

func linuxDesktopEntry(appID, displayName, executable string) string {
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Version=1.0",
		"Name=" + desktopValue(displayName),
		"Comment=PCController host and WebUI",
		"Exec=" + desktopExecQuote(executable) + " uri %u",
		"Terminal=false",
		"MimeType=x-scheme-handler/" + productidentity.ProtocolScheme + ";",
		"Categories=Utility;System;",
		"StartupNotify=true",
		"StartupWMClass=" + desktopValue(appID),
		"X-PCController-ExecutableSHA256=" + executableDigest(executable),
		"",
	}, "\n")
}

func desktopValue(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\t", "\\t").Replace(value)
}

func desktopExecQuote(value string) string {
	value = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$").Replace(value)
	return "\"" + value + "\""
}

func executableDigest(executable string) string {
	value := sha256.Sum256([]byte(filepath.Clean(executable)))
	return hex.EncodeToString(value[:])
}

func linuxDesktopOwned(content []byte, executable string) bool {
	marker := "X-PCController-ExecutableSHA256=" + executableDigest(executable)
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
}

func atomicWriteFile(path string, content []byte, mode os.FileMode) (resultErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pccontroller-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if resultErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func removeLinuxMimeAssociation(desktopName string) (bool, error) {
	var paths []string
	if configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configHome != "" {
		paths = append(paths, filepath.Join(configHome, "mimeapps.list"))
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "mimeapps.list"))
	}
	if applications, err := linuxApplicationsDirectory(); err == nil {
		paths = append(paths, filepath.Join(applications, "mimeapps.list"))
	}
	removed := false
	var failures []error
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("read %s: %w", path, err))
			continue
		}
		updated, changed := removeMimeDefault(content, "x-scheme-handler/"+productidentity.ProtocolScheme, desktopName)
		if !changed {
			continue
		}
		if err := atomicWriteFile(path, updated, 0o600); err != nil {
			failures = append(failures, fmt.Errorf("update %s: %w", path, err))
			continue
		}
		removed = true
	}
	return removed, errors.Join(failures...)
}

func removeMimeDefault(content []byte, mimeType, desktopName string) ([]byte, bool) {
	var output []string
	changed := false
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
		}
		key, value, found := strings.Cut(line, "=")
		if section == "[Default Applications]" && found && strings.TrimSpace(key) == mimeType {
			var retained []string
			for _, item := range strings.Split(value, ";") {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				if item == desktopName {
					changed = true
					continue
				}
				retained = append(retained, item)
			}
			if len(retained) == 0 {
				continue
			}
			line = strings.TrimSpace(key) + "=" + strings.Join(retained, ";") + ";"
		}
		output = append(output, line)
	}
	if !changed {
		return content, false
	}
	return []byte(strings.Join(output, "\n") + "\n"), true
}

func boundedDesktopOutput(output []byte) string {
	value := strings.Join(strings.Fields(strings.ToValidUTF8(string(output), "")), " ")
	if len(value) > 256 {
		value = value[:256] + "..."
	}
	if value == "" {
		return ""
	}
	return ": " + value
}
