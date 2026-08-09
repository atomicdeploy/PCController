//go:build !linux

package aptmirror

import "errors"

func AcquireAdoptionLock() (func(), error) {
	return nil, errors.New("APT mirror adoption is supported only on Linux")
}

func AcquirePackageManagerLocks() (func(), error) {
	return nil, errors.New("APT/dpkg quiescence locks are supported only on Linux")
}

func acquireLock(string) (func(), error) {
	return nil, errors.New("APT mirror refresh apply is supported only on Linux")
}
