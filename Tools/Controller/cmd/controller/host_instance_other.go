//go:build !windows

package main

import (
	"errors"
	"os"
)

type fileHostInstanceLock struct {
	file *os.File
	path string
}

func platformHostInstanceUserKey() (string, error) {
	return os.UserConfigDir()
}

func platformTryHostInstanceLock(
	_ string,
	path string,
) (platformHostInstanceLock, bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &fileHostInstanceLock{file: file, path: path}, true, nil
}

func (lock *fileHostInstanceLock) Close() error {
	if lock == nil {
		return nil
	}
	var err error
	if lock.file != nil {
		err = lock.file.Close()
		lock.file = nil
	}
	return errors.Join(err, os.Remove(lock.path))
}
