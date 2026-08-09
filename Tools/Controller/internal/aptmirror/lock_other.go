//go:build !linux

package aptmirror

import "errors"

func acquireLock(string) (func(), error) {
	return nil, errors.New("APT mirror refresh apply is supported only on Linux")
}
