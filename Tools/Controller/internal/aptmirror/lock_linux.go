//go:build linux

package aptmirror

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

const AdoptionLockPath = "/run/lock/pccontroller-apt-mirror-adoption.lock"

var packageManagerLockPaths = []string{
	"/var/lib/dpkg/lock-frontend",
	"/var/lib/dpkg/lock",
	"/var/lib/apt/lists/lock",
	"/var/cache/apt/archives/lock",
}

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

// AcquirePackageManagerLocks takes the same POSIX record locks used by
// APT/dpkg and retains them for the complete source-adoption transaction. The
// acquisition is non-blocking: an active package operation causes a fail-
// closed result and no package process is signaled or killed.
func AcquirePackageManagerLocks() (func(), error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("APT/dpkg quiescence locks require root")
	}
	return acquirePackageManagerLocks(packageManagerLockPaths, uint32(os.Geteuid()), validateRootOwnedAncestors)
}

func acquirePackageManagerLocks(paths []string, expectedUID uint32, validateAncestors func(string) error) (func(), error) {
	files := make([]*os.File, 0, len(paths))
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			for index := len(files) - 1; index >= 0; index-- {
				unlock := unix.Flock_t{Type: unix.F_UNLCK, Whence: 0, Start: 0, Len: 0}
				_ = unix.FcntlFlock(files[index].Fd(), unix.F_SETLK, &unlock)
				_ = files[index].Close()
			}
		})
	}
	fail := func(err error) (func(), error) {
		release()
		return nil, err
	}

	for _, path := range paths {
		directoryPath := filepath.Dir(path)
		if validateAncestors != nil {
			if err := validateAncestors(directoryPath); err != nil {
				return fail(fmt.Errorf("validate APT/dpkg lock path %s: %w", path, err))
			}
		}
		directory, err := unix.Open(directoryPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return fail(fmt.Errorf("open APT/dpkg lock directory %s: %w", directoryPath, err))
		}
		var directoryStatus unix.Stat_t
		if err := unix.Fstat(directory, &directoryStatus); err != nil {
			_ = unix.Close(directory)
			return fail(fmt.Errorf("inspect APT/dpkg lock directory %s: %w", directoryPath, err))
		}
		if directoryStatus.Uid != expectedUID || directoryStatus.Mode&0o022 != 0 {
			_ = unix.Close(directory)
			return fail(fmt.Errorf("APT/dpkg lock directory %s is not owned by the expected identity or is writable by another identity", directoryPath))
		}
		fd, err := unix.Openat(directory, filepath.Base(path), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o640)
		_ = unix.Close(directory)
		if err != nil {
			return fail(fmt.Errorf("open APT/dpkg lock %s: %w", path, err))
		}
		file := os.NewFile(uintptr(fd), path)
		if file == nil {
			_ = unix.Close(fd)
			return fail(fmt.Errorf("open APT/dpkg lock %s", path))
		}
		var status unix.Stat_t
		if err := unix.Fstat(fd, &status); err != nil {
			_ = file.Close()
			return fail(fmt.Errorf("inspect APT/dpkg lock %s: %w", path, err))
		}
		if status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != expectedUID || status.Nlink != 1 || status.Mode&0o022 != 0 {
			_ = file.Close()
			return fail(fmt.Errorf("APT/dpkg lock %s is not a singly linked, safely owned regular file", path))
		}
		lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
		if err := unix.FcntlFlock(file.Fd(), unix.F_SETLK, &lock); err != nil {
			if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EAGAIN) {
				holder := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
				pid := int32(0)
				if queryErr := unix.FcntlFlock(file.Fd(), unix.F_GETLK, &holder); queryErr == nil && holder.Type != unix.F_UNLCK {
					pid = holder.Pid
				}
				_ = file.Close()
				if pid > 0 {
					return fail(fmt.Errorf("APT/dpkg lock %s is held by pid %d", path, pid))
				}
				return fail(fmt.Errorf("APT/dpkg lock %s is held by another process", path))
			}
			_ = file.Close()
			return fail(fmt.Errorf("lock APT/dpkg path %s: %w", path, err))
		}
		files = append(files, file)
	}
	return release, nil
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
