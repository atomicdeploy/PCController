//go:build windows

package secretstore

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credentialTypeGeneric         = 1
	credentialPersistLocalMachine = 2
	errorNotFound                 = syscall.Errno(1168)
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

type nativeCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWrittenLow     uint32
	LastWrittenHigh    uint32
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsBackend struct {
	namespace string
}

func newPlatformBackend(namespace string) Backend {
	if namespace == "" {
		return unavailableBackend{}
	}
	return &windowsBackend{namespace: namespace}
}

func (*windowsBackend) Status() Status {
	return Status{
		Provider: "windows-credential-manager", Available: true,
		Durable: true, Scope: "current-user/local-machine",
	}
}

func (backend *windowsBackend) target(name string) (string, error) {
	if !secretNamePattern.MatchString(name) {
		return "", errors.New("invalid secret name")
	}
	return backend.namespace + "/" + name, nil
}

func (backend *windowsBackend) Get(name string) (string, error) {
	target, err := backend.target(name)
	if err != nil {
		return "", err
	}
	targetUTF16, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", err
	}
	var credential *nativeCredential
	result, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(targetUTF16)), credentialTypeGeneric, 0,
		uintptr(unsafe.Pointer(&credential)),
	)
	if result == 0 {
		if errors.Is(callErr, errorNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("CredReadW: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential == nil || credential.CredentialBlobSize == 0 || credential.CredentialBlob == nil {
		return "", ErrNotFound
	}
	value := unsafe.Slice(credential.CredentialBlob, int(credential.CredentialBlobSize))
	return string(append([]byte(nil), value...)), nil
}

func (backend *windowsBackend) Set(name, value string) error {
	target, err := backend.target(name)
	if err != nil {
		return err
	}
	if err := ValidateValue(value); err != nil {
		return err
	}
	targetUTF16, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	blob := []byte(value)
	credential := nativeCredential{
		Type: credentialTypeGeneric, TargetName: targetUTF16,
		CredentialBlobSize: uint32(len(blob)), CredentialBlob: &blob[0],
		Persist: credentialPersistLocalMachine,
	}
	result, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&credential)), 0)
	if result == 0 {
		return fmt.Errorf("CredWriteW: %w", callErr)
	}
	return nil
}

func (backend *windowsBackend) Delete(name string) error {
	target, err := backend.target(name)
	if err != nil {
		return err
	}
	targetUTF16, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	result, _, callErr := procCredDeleteW.Call(
		uintptr(unsafe.Pointer(targetUTF16)), credentialTypeGeneric, 0,
	)
	if result == 0 {
		if errors.Is(callErr, errorNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("CredDeleteW: %w", callErr)
	}
	return nil
}
