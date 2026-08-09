//go:build windows

package programmer

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Keep the kernel-lock byte outside the small JSON owner record so contenders
// can read diagnostics while Windows denies access to the locked byte range.
const compileLockByteOffset = uint32(0x7FFFFFFF)

func tryLockCompileFile(file *os.File) (bool, error) {
	overlapped := &windows.Overlapped{Offset: compileLockByteOffset}
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return false, err
}

func unlockCompileFile(file *os.File) error {
	return windows.UnlockFileEx(
		windows.Handle(file.Fd()),
		0,
		1,
		0,
		&windows.Overlapped{Offset: compileLockByteOffset},
	)
}
