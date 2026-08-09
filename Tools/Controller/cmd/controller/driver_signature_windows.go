//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func verifyZadigExecutable(path string) error {
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	file := &windows.WinTrustFileInfo{
		Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
		FilePath: path16,
	}
	data := &windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_WHOLECHAIN,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(file),
	}
	verifyErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		data,
	)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	closeErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		data,
	)
	if verifyErr != nil {
		return fmt.Errorf("WinVerifyTrust: %w", verifyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close WinVerifyTrust state: %w", closeErr)
	}
	return nil
}

func newDetachedCommand(path string) *exec.Cmd {
	command := exec.Command(path)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
	return command
}
