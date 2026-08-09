// Package pathguard centralizes filesystem namespace and link-traversal
// checks for destructive or trust-sensitive host operations.
package pathguard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CleanAbsolute returns one canonical absolute path after rejecting platform
// namespace aliases that cannot be reasoned about lexically.
func CleanAbsolute(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", errors.New("path must be a non-empty absolute path")
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	if err := validatePlatformAbsoluteSyntax(path); err != nil {
		return "", err
	}
	return path, nil
}

// ValidateRelative rejects traversal, device aliases, alternate data streams,
// reserved names, and non-local relative paths before filepath.Join is used.
func ValidateRelative(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsRune(value, '\x00') || !filepath.IsLocal(value) {
		return errors.New("path is not a local relative path")
	}
	return validatePlatformRelativeSyntax(value)
}

// ValidateComponents rejects a symlink or Windows reparse point in every
// existing component. A missing suffix is allowed only for a caller that will
// create it and re-run this check after creation.
func ValidateComponents(value string, allowMissingSuffix bool) error {
	path, err := CleanAbsolute(value)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(path)
	root := volume + string(os.PathSeparator)
	if volume == "" {
		root = string(os.PathSeparator)
	}
	current := root
	parts := strings.FieldsFunc(strings.TrimPrefix(path, root), func(char rune) bool {
		return char == '/' || char == '\\'
	})
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) && allowMissingSuffix {
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("inspect path component %s: %w", current, statErr)
		}
		if err := ValidateComponent(current, info); err != nil {
			return err
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("path component %s is not a directory", current)
		}
	}
	return nil
}

// ValidateComponent rejects link-like metadata for one already-lstat'd path.
func ValidateComponent(path string, info fs.FileInfo) error {
	linked, err := platformComponentIsLink(path, info)
	if err != nil {
		return fmt.Errorf("inspect path component attributes %s: %w", path, err)
	}
	if linked {
		return fmt.Errorf("path component %s is a symbolic link or reparse point", path)
	}
	return nil
}

// ValidateTree rejects all links and reparse points in an existing tree before
// recursive removal. It deliberately fails closed on unreadable entries.
func ValidateTree(root string) error {
	if err := ValidateComponents(root, false); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return ValidateComponent(path, info)
	})
}

// MkdirAll creates only through components that were link-free and verifies
// the completed path again, closing the normal create-then-use trust gap.
func MkdirAll(path string, mode fs.FileMode) error {
	if err := ValidateComponents(path, true); err != nil {
		return err
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	return ValidateComponents(path, false)
}
