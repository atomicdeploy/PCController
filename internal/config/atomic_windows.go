//go:build windows

package config

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	advapi32                    = syscall.NewLazyDLL("advapi32.dll")
	procMoveFileExW             = kernel32.NewProc("MoveFileExW")
	procLocalFree               = kernel32.NewProc("LocalFree")
	procConvertStringSecuritySD = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procSetFileSecurityW        = advapi32.NewProc("SetFileSecurityW")
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
	daclSecurityInformation = 0x4
	sddlRevision1           = 1
)

func atomicReplace(source, destination string) error {
	from, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	result, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(from)),
		uintptr(unsafe.Pointer(to)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if result == 0 {
		return nonzeroWindowsError(callErr, "MoveFileExW failed")
	}
	return nil
}

func secureFile(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}

	// A protected DACL granting full access only to OWNER RIGHTS is the
	// Windows equivalent of a POSIX 0600 file. The temporary file is created
	// by this process, so its owner is the current user.
	sddl, err := syscall.UTF16PtrFromString("D:P(A;;FA;;;OW)")
	if err != nil {
		return err
	}
	var descriptor uintptr
	result, _, callErr := procConvertStringSecuritySD.Call(
		uintptr(unsafe.Pointer(sddl)),
		sddlRevision1,
		uintptr(unsafe.Pointer(&descriptor)),
		0,
	)
	if result == 0 {
		return nonzeroWindowsError(callErr, "ConvertStringSecurityDescriptorToSecurityDescriptorW failed")
	}
	defer procLocalFree.Call(descriptor)

	fileName, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, callErr = procSetFileSecurityW.Call(
		uintptr(unsafe.Pointer(fileName)),
		daclSecurityInformation,
		descriptor,
	)
	if result == 0 {
		return nonzeroWindowsError(callErr, "SetFileSecurityW failed")
	}
	return nil
}

func syncDirectory(string) error {
	// MOVEFILE_WRITE_THROUGH asks Windows to flush the move before returning.
	return nil
}

func nonzeroWindowsError(err error, fallback string) error {
	if err != nil && !errors.Is(err, syscall.Errno(0)) {
		return err
	}
	return errors.New(fallback)
}
