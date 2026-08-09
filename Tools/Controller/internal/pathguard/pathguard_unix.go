//go:build !windows

package pathguard

import (
	"io/fs"
)

func validatePlatformAbsoluteSyntax(string) error { return nil }
func validatePlatformRelativeSyntax(string) error { return nil }

func platformComponentIsLink(_ string, info fs.FileInfo) (bool, error) {
	return info.Mode()&fs.ModeSymlink != 0, nil
}
