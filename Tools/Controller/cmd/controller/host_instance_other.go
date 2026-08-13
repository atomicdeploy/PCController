//go:build !windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

type fileHostInstanceLock struct {
	file *os.File
}

func platformHostInstanceUserKey() (string, error) {
	return os.UserConfigDir()
}

func platformTryHostInstanceLock(
	_ string,
	path string,
) (platformHostInstanceLock, bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &fileHostInstanceLock{file: file}, true, nil
}

func (lock *fileHostInstanceLock) Close() error {
	if lock == nil {
		return nil
	}
	if lock.file == nil {
		return nil
	}
	err := lock.file.Close()
	lock.file = nil
	return err
}
