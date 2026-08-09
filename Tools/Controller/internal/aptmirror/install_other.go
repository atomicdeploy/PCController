//go:build !linux

package aptmirror

import "errors"

func validateMirrorApply(string, []string, string) error {
	return errors.New("APT mirror profile --apply is supported only on Linux")
}
