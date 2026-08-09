//go:build linux

package aptmirror

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func validateMirrorApply(executable string, managedPaths []string, backupRoot string) error {
	if os.Geteuid() != 0 {
		return errors.New("APT mirror profile --apply requires root")
	}
	info, err := os.Lstat(executable)
	if err != nil {
		return err
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("current Controller executable must be a root-owned regular file, not a symlink")
	}
	paths := append(append([]string(nil), managedPaths...), backupRoot)
	for _, path := range paths {
		if err := validateRootOwnedAncestors(filepath.Dir(path)); err != nil {
			return fmt.Errorf("validate root-owned managed path %s: %w", path, err)
		}
	}
	if err := validateRootOwnedAncestors(backupRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("validate root-owned backup root %s: %w", backupRoot, err)
	}
	return nil
}

func validateRootOwnedAncestors(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			parent := filepath.Dir(current)
			if parent == current {
				return err
			}
			current = parent
			continue
		}
		if err != nil {
			return err
		}
		status, ok := info.Sys().(*syscall.Stat_t)
		if !ok || status.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s is not a root-owned real directory", current)
		}
		if current == string(filepath.Separator) {
			return nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}
