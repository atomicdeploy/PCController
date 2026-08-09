//go:build !windows

package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type lifecycleLock struct {
	file   *os.File
	locked bool
}

func acquireLifecycleLock(ctx context.Context, path string) (*lifecycleLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	lock := &lifecycleLock{file: file}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			lock.locked = true
			return lock, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("lock installation lifecycle: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, fmt.Errorf("wait for installation lifecycle lock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (lock *lifecycleLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	var result error
	if lock.locked {
		result = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
		lock.locked = false
	}
	result = errors.Join(result, lock.file.Close())
	lock.file = nil
	return result
}

func replaceFile(source, destination string) error { return os.Rename(source, destination) }

func platformOwnerID() (string, error) {
	value, err := user.Current()
	if err != nil {
		return "", err
	}
	return "uid:" + value.Uid, nil
}
