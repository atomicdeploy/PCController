//go:build windows

package hostui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"

	"pccontroller.local/controller/internal/productidentity"
)

type registryWriter interface {
	Set(path, name, value string) error
}

type windowsRegistryWriter struct{}

func (windowsRegistryWriter) Set(path, name, value string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(name, value)
}

type registryCleaner interface {
	String(path, name string) (string, error)
	DeleteTree(path string) error
}

type windowsRegistryCleaner struct{}

func (windowsRegistryCleaner) String(path, name string) (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	return value, err
}

func (windowsRegistryCleaner) DeleteTree(path string) error {
	return deleteCurrentUserRegistryTree(path)
}

func ensurePlatformDesktopIntegration(
	options DesktopIntegrationOptions,
) (DesktopIntegrationStatus, error) {
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		appID = productidentity.StableAppID
	}
	displayName := productidentity.Title(options.DisplayName)
	executable, err := resolveDesktopExecutable(options.Executable)
	if err != nil {
		return DesktopIntegrationStatus{Supported: true, LastError: err.Error()}, err
	}
	status := DesktopIntegrationStatus{Supported: true, Executable: executable}
	logo, err := ResolveToastLogoPath(executable)
	if err != nil {
		status.LastError = err.Error()
		return status, err
	}
	if _, err := resolveWindowsToastLogo(logo); err != nil {
		status.LastError = err.Error()
		return status, err
	}
	status.Logo = logo
	if err := ensureProtocolRegistry(windowsRegistryWriter{}, executable, logo, appID, displayName); err != nil {
		status.LastError = err.Error()
		return status, err
	}
	status.ProtocolReady = true
	appData := os.Getenv("APPDATA")
	if strings.TrimSpace(appData) == "" {
		err := fmt.Errorf("APPDATA is unavailable")
		status.LastError = err.Error()
		return status, err
	}
	shortcut := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", shortcutFileName(displayName)+".lnk")
	if err := os.MkdirAll(filepath.Dir(shortcut), 0o755); err != nil {
		status.LastError = err.Error()
		return status, err
	}
	if err := createWindowsShortcut(executable, shortcut, appID, displayName); err != nil {
		status.Shortcut = shortcut
		status.LastError = err.Error()
		return status, err
	}
	status.Shortcut, status.ShortcutReady = shortcut, true
	return status, nil
}

func removePlatformDesktopIntegration(
	options DesktopIntegrationOptions,
) (DesktopIntegrationCleanupStatus, error) {
	appID := strings.TrimSpace(options.AppID)
	if appID == "" {
		appID = productidentity.StableAppID
	}
	if strings.ContainsAny(appID, "\\/\x00") {
		err := errors.New("desktop AppUserModelID contains an invalid path separator")
		return DesktopIntegrationCleanupStatus{Supported: true, LastError: err.Error()}, err
	}
	displayName := productidentity.Title(options.DisplayName)
	executable, err := resolveDesktopExecutable(options.Executable)
	if err != nil {
		return DesktopIntegrationCleanupStatus{Supported: true, LastError: err.Error()}, err
	}
	status := DesktopIntegrationCleanupStatus{Supported: true}
	var cleanupErr error

	protocolRemoved, identityRemoved, skipped, registryErr := removeOwnedRegistryIntegration(
		windowsRegistryCleaner{}, executable, appID,
	)
	status.ProtocolRemoved = protocolRemoved
	status.AppIdentityRemoved = identityRemoved
	status.Skipped = append(status.Skipped, skipped...)
	cleanupErr = errors.Join(cleanupErr, registryErr)

	shortcut, shortcutErr := desktopShortcutPath(displayName)
	if shortcutErr != nil {
		cleanupErr = errors.Join(cleanupErr, shortcutErr)
	} else {
		status.Shortcut = shortcut
		removed, preserved, removeErr := removeOwnedShortcut(executable, shortcut)
		status.ShortcutRemoved = removed
		if preserved {
			status.Skipped = append(status.Skipped, "start-menu-shortcut-not-owned")
		}
		cleanupErr = errors.Join(cleanupErr, removeErr)
	}
	if cleanupErr != nil {
		status.LastError = cleanupErr.Error()
	}
	return status, cleanupErr
}

func removeOwnedRegistryIntegration(
	registry registryCleaner,
	executable, appID string,
) (protocolRemoved, identityRemoved bool, skipped []string, err error) {
	protocolPath := `Software\Classes\` + productidentity.ProtocolScheme
	commandPath := protocolPath + `\shell\open\command`
	command, commandErr := registry.String(commandPath, "")
	switch {
	case commandErr == nil && strings.EqualFold(strings.TrimSpace(command), protocolCommand(executable)):
		if deleteErr := registry.DeleteTree(protocolPath); deleteErr != nil && !isNotExist(deleteErr) {
			err = errors.Join(err, fmt.Errorf("remove protocol registration: %w", deleteErr))
		} else if deleteErr == nil {
			protocolRemoved = true
		}
	case commandErr == nil:
		skipped = append(skipped, "protocol-registration-not-owned")
	case !isNotExist(commandErr):
		err = errors.Join(err, fmt.Errorf("inspect protocol registration: %w", commandErr))
	}

	identityPath := `Software\Classes\AppUserModelId\` + appID
	iconURI, identityErr := registry.String(identityPath, "IconUri")
	switch {
	case identityErr == nil && (sameWindowsPath(iconURI, executable) || sameWindowsPath(iconURI, filepath.Join(filepath.Dir(executable), ToastLogoFileName))):
		if deleteErr := registry.DeleteTree(identityPath); deleteErr != nil && !isNotExist(deleteErr) {
			err = errors.Join(err, fmt.Errorf("remove application identity: %w", deleteErr))
		} else if deleteErr == nil {
			identityRemoved = true
		}
	case identityErr == nil:
		skipped = append(skipped, "app-identity-registration-not-owned")
	case !isNotExist(identityErr):
		err = errors.Join(err, fmt.Errorf("inspect application identity: %w", identityErr))
	}
	return protocolRemoved, identityRemoved, skipped, err
}

func desktopShortcutPath(displayName string) (string, error) {
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if appData == "" {
		return "", errors.New("APPDATA is unavailable")
	}
	programs, err := filepath.Abs(filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs"))
	if err != nil {
		return "", fmt.Errorf("resolve Start Menu programs directory: %w", err)
	}
	shortcut := filepath.Join(programs, shortcutFileName(displayName)+".lnk")
	relative, err := filepath.Rel(programs, shortcut)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("resolved shortcut is outside the Start Menu programs directory")
	}
	return shortcut, nil
}

func removeOwnedShortcut(executable, shortcut string) (removed, preserved bool, err error) {
	info, statErr := os.Lstat(shortcut)
	if isNotExist(statErr) {
		return false, false, nil
	}
	if statErr != nil {
		return false, false, fmt.Errorf("inspect Start Menu shortcut: %w", statErr)
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 || isWindowsReparsePoint(info) {
		return false, true, nil
	}
	link, inspectErr := inspectWindowsShortcut(shortcut)
	if inspectErr != nil {
		return false, false, fmt.Errorf("inspect Start-menu shortcut ownership: %w", inspectErr)
	}
	if !shortcutOwnedBy(executable, link) {
		return false, true, nil
	}
	if removeErr := os.Remove(shortcut); removeErr != nil {
		return false, false, fmt.Errorf("remove owned Start-menu shortcut: %w", removeErr)
	}
	_, statErr = os.Lstat(shortcut)
	if isNotExist(statErr) {
		return true, false, nil
	}
	if statErr != nil {
		return false, false, fmt.Errorf("verify Start Menu shortcut removal: %w", statErr)
	}
	return false, true, nil
}

func isWindowsReparsePoint(info os.FileInfo) bool {
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && attributes.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func deleteCurrentUserRegistryTree(path string) error {
	key, err := registry.OpenKey(
		registry.CURRENT_USER, path,
		registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE,
	)
	if err != nil {
		return err
	}
	children, readErr := key.ReadSubKeyNames(-1)
	closeErr := key.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	for _, child := range children {
		if child == "" || strings.ContainsAny(child, "\\/\x00") {
			return errors.New("registry subtree contains an invalid child name")
		}
		if err := deleteCurrentUserRegistryTree(path + `\` + child); err != nil {
			return err
		}
	}
	return registry.DeleteKey(registry.CURRENT_USER, path)
}

func sameWindowsPath(left, right string) bool {
	left = strings.Trim(strings.TrimSpace(left), `"`)
	right = strings.Trim(strings.TrimSpace(right), `"`)
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func isNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}

func ensureProtocolRegistry(
	registry registryWriter,
	executable, logo, appID, displayName string,
) error {
	values := []struct{ path, name, value string }{
		{`Software\Classes\` + productidentity.ProtocolScheme, "", "URL:" + displayName + " Protocol"},
		{`Software\Classes\` + productidentity.ProtocolScheme, "URL Protocol", ""},
		{`Software\Classes\` + productidentity.ProtocolScheme + `\DefaultIcon`, "", quoteWindowsArgument(executable) + ",0"},
		{`Software\Classes\` + productidentity.ProtocolScheme + `\shell\open\command`, "", protocolCommand(executable)},
		{`Software\Classes\AppUserModelId\` + appID, "DisplayName", displayName},
		{`Software\Classes\AppUserModelId\` + appID, "IconUri", logo},
	}
	for _, value := range values {
		if err := registry.Set(value.path, value.name, value.value); err != nil {
			return fmt.Errorf("register desktop integration %s: %w", value.path, err)
		}
	}
	return nil
}

func shortcutFileName(displayName string) string {
	name := strings.Map(func(value rune) rune {
		switch value {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '-'
		default:
			return value
		}
	}, strings.TrimSpace(displayName))
	name = strings.Trim(name, " .")
	if name == "" {
		return productidentity.DefaultAppTitle()
	}
	return name
}

func protocolCommand(executable string) string {
	return quoteWindowsArgument(executable) + ` uri "%1"`
}

func quoteWindowsArgument(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
