//go:build !windows

package pathguard

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

func validatePlatformAbsoluteSyntax(string) error { return nil }
func validatePlatformRelativeSyntax(string) error { return nil }

func resolvePlatformAbsolute(path string) (string, error) {
	current := path
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func platformComponentIsLink(path string, info fs.FileInfo) (bool, error) {
	if info.Mode()&fs.ModeSymlink == 0 {
		return false, nil
	}
	trusted, err := trustedSystemSymlink(path, info)
	if err != nil {
		return false, err
	}
	return !trusted, nil
}

func platformComponentIsDirectory(path string, info fs.FileInfo) (bool, error) {
	if info.IsDir() {
		return true, nil
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return false, nil
	}
	trusted, err := trustedSystemSymlink(path, info)
	if err != nil || !trusted {
		return false, err
	}
	target, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return target.IsDir(), nil
}
