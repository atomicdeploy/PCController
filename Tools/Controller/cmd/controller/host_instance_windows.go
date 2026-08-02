//go:build windows

package main

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
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

func (lock *windowsHostInstanceLock) Close() error {
	if lock == nil || lock.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(lock.handle)
	lock.handle = 0
	return err
}
