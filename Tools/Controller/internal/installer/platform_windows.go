//go:build windows

package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"

	"pccontroller.local/controller/internal/ownedstorage"
	"pccontroller.local/controller/internal/pathguard"
)

type lifecycleLock struct {
	file       *os.File
	overlapped windows.Overlapped
	locked     bool
}

func acquireLifecycleLock(ctx context.Context, path string) (*lifecycleLock, error) {
	if err := pathguard.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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
		err = windows.LockFileEx(
			windows.Handle(file.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, &lock.overlapped,
		)
		if err == nil {
			lock.locked = true
			return lock, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
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
		result = windows.UnlockFileEx(windows.Handle(lock.file.Fd()), 0, 1, 0, &lock.overlapped)
		lock.locked = false
	}
	result = errors.Join(result, lock.file.Close())
	lock.file = nil
	return result
}

func replaceFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		from, to,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

func platformOwnerID() (string, error) {
	return ownedstorage.CurrentOwnerID()
}
