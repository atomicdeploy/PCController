//go:build windows

package pathguard

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func validatePlatformAbsoluteSyntax(path string) error {
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, `\\?\`) || strings.HasPrefix(lower, `\\.\`) ||
		strings.HasPrefix(lower, `\??\`) {
		return errors.New("extended or device Windows path namespaces are not allowed")
	}
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' {
		return errors.New("only drive-rooted local Windows paths are allowed")
	}
	return validateWindowsComponents(strings.TrimPrefix(path, volume+`\`))
}

func validatePlatformRelativeSyntax(path string) error {
	return validateWindowsComponents(strings.ReplaceAll(path, "/", `\`))
}

func resolvePlatformAbsolute(path string) (string, error) { return path, nil }

func validateWindowsComponents(path string) error {
	for _, component := range strings.Split(path, `\`) {
		if component == "" {
			continue
		}
		if strings.Contains(component, ":") || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return errors.New("Windows path contains an alternate stream or trailing-dot/space alias")
		}
		base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
		reserved := base == "CON" || base == "PRN" || base == "AUX" || base == "NUL"
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			reserved = true
		}
		if reserved {
			return errors.New("Windows path contains a reserved device name")
		}
	}
	return nil
}

func platformComponentIsLink(path string, info fs.FileInfo) (bool, error) {
	value, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, err := windows.GetFileAttributes(value)
	if err != nil {
		return false, err
	}
	return info.Mode()&fs.ModeSymlink != 0 || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}

func platformComponentIsDirectory(_ string, info fs.FileInfo) (bool, error) {
	return info.IsDir(), nil
}
