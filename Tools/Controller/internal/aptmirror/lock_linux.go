//go:build linux

package aptmirror

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const AdoptionLockPath = "/run/lock/pccontroller-apt-mirror-adoption.lock"

// AcquireAdoptionLock serializes the complete root-owned source adoption
// transaction. The fixed path cannot be redirected through a candidate or
// installed config, and flock ownership is tied to the open descriptor so a
// killed Controller process cannot leave a stale lock behind.
func AcquireAdoptionLock() (func(), error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("APT mirror adoption lock requires root")
	}
	return acquireOperationLock(AdoptionLockPath, "APT mirror adoption")
}

func acquireLock(path string) (func(), error) {
	return acquireOperationLock(path, "APT mirror refresh")
}

func acquireOperationLock(path, operation string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	directory, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open trusted APT mirror lock directory: %w", err)
	}
	defer unix.Close(directory)
	var directoryStatus unix.Stat_t
	if err := unix.Fstat(directory, &directoryStatus); err != nil {
		return nil, fmt.Errorf("inspect trusted APT mirror lock directory: %w", err)
	}
	if directoryStatus.Uid != uint32(os.Geteuid()) || directoryStatus.Mode&0o022 != 0 {
		return nil, errors.New("APT mirror lock directory must be owned by the invoking identity and not group/world writable")
	}
	fd, err := unix.Openat(directory, filepath.Base(path), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open trusted APT mirror lock file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open trusted APT mirror lock file")
	}
	var fileStatus unix.Stat_t
	if err := unix.Fstat(fd, &fileStatus); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect trusted APT mirror lock file: %w", err)
	}
	if fileStatus.Mode&unix.S_IFMT != unix.S_IFREG || fileStatus.Uid != uint32(os.Geteuid()) ||
		fileStatus.Mode&0o022 != 0 || fileStatus.Nlink != 1 {
		_ = file.Close()
		return nil, errors.New("APT mirror lock must be a singly linked regular file owned by the invoking identity and not group/world writable")
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%s is already running", operation)
		}
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
	}, nil
}
