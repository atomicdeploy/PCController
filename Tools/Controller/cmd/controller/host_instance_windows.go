//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"

	"pccontroller.local/controller/internal/productidentity"
)

type windowsHostInstanceLock struct {
	handle windows.Handle
}

func platformHostInstanceUserKey() (string, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	if user == nil || user.User.Sid == nil {
		return "", errors.New("current Windows token has no user SID")
	}
	return user.User.Sid.String(), nil
}

func platformTryHostInstanceLock(
	name, _ string,
) (platformHostInstanceLock, bool, error) {
	name = windowsHostInstanceMutexName(name)
	nameValue, err := windows.UTF16PtrFromString(`Global\` + name)
	if err != nil {
		return nil, false, fmt.Errorf("encode per-user host mutex name: %w", err)
	}
	handle, createErr := windows.CreateMutex(nil, false, nameValue)
	if errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, false, nil
	}
	if createErr != nil {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, false, fmt.Errorf("create per-user host mutex: %w", createErr)
	}
	return &windowsHostInstanceLock{handle: handle}, true, nil
}

func windowsHostInstanceMutexName(name string) string {
	// Default production identities used to contain the current user's SID.
	// A real SCM owner runs under a virtual account, so that scheme allowed an
	// interactive user to claim a second board-owning primary. Collapse only
	// product default names to a machine-wide singleton; explicit test/custom
	// lock names retain their isolation.
	if strings.HasPrefix(name, productidentity.StableAppID+".Host.") {
		return productidentity.StableAppID + ".Host.Machine"
	}
	return name
}

func (lock *windowsHostInstanceLock) Close() error {
	if lock == nil || lock.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(lock.handle)
	lock.handle = 0
	return err
}
