//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

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
	if err := validateHostInstanceLockDirectory(filepath.Dir(path)); err != nil {
		return nil, false, err
	}
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, false, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, errors.New("open per-user host lock: invalid file descriptor")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("inspect per-user host lock: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
		_ = file.Close()
		return nil, false, errors.New("per-user host lock must be a regular, singly-linked file owned by the current user")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("secure per-user host lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("acquire per-user host lock: %w", err)
	}
	return &fileHostInstanceLock{file: file}, true, nil
}

func validateHostInstanceLockDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect per-user host state directory: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("per-user host state path is not a real directory")
	}
	if int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("per-user host state directory is owned by UID %d, not current UID %d", stat.Uid, os.Geteuid())
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("secure per-user host state directory permissions %04o: %w", info.Mode().Perm(), err)
		}
	}
	return nil
}

func (lock *fileHostInstanceLock) Close() error {
	if lock == nil {
		return nil
	}
	var err error
	if lock.file != nil {
		err = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
		err = errors.Join(err, lock.file.Close())
		lock.file = nil
	}
	return err
}
