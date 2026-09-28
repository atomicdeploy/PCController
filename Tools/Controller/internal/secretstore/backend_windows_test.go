//go:build windows

package secretstore

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestWindowsCredentialManagerRoundTrip(t *testing.T) {
	resolver := New(fmt.Sprintf("controller-test/%d/%d", os.Getpid(), time.Now().UnixNano()))
	if status := resolver.Status(); !status.Available || status.Provider != "windows-credential-manager" {
		t.Fatalf("unexpected status: %#v", status)
	}
	reference := "os:roundtrip"
	if err := resolver.Set(reference, "credential-manager-roundtrip"); err != nil {
		if credentialManagerUnavailableForLogon(err) {
			t.Skipf("Windows Credential Manager round-trip unavailable for this logon session (ERROR_NO_SUCH_LOGON_SESSION 1312): %v", err)
		}
		t.Fatal(err)
	}
	defer func() { _ = resolver.Delete(reference) }()
	if got, err := resolver.Resolve(reference); err != nil || got != "credential-manager-roundtrip" {
		t.Fatalf("resolve=%q err=%v", got, err)
	}
	if err := resolver.Delete(reference); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(reference); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted credential error=%v", err)
	}
}
func TestCredentialCallErrorMapping(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		result    uintptr
		callErr   error
		notFound  bool
		want      error
	}{
		{name: "success", operation: "CredWriteW", result: 1, callErr: syscall.Errno(0)},
		{name: "read missing", operation: "CredReadW", callErr: errorNotFound, notFound: true, want: ErrNotFound},
		{name: "delete missing", operation: "CredDeleteW", callErr: errorNotFound, notFound: true, want: ErrNotFound},
		{name: "write missing is not remapped", operation: "CredWriteW", callErr: errorNotFound, want: errorNotFound},
		{name: "access denied", operation: "CredWriteW", callErr: syscall.Errno(5), want: syscall.Errno(5)},
		{name: "invalid parameter", operation: "CredWriteW", callErr: syscall.Errno(87), want: syscall.Errno(87)},
		{name: "no logon session", operation: "CredWriteW", callErr: errorNoSuchLogonSession, want: errorNoSuchLogonSession},
		{name: "logon failure", operation: "CredWriteW", callErr: syscall.Errno(1326), want: syscall.Errno(1326)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mapCredentialCallError(test.operation, test.result, test.callErr, test.notFound)
			if test.want == nil {
				if got != nil {
					t.Fatalf("mapCredentialCallError()=%v, want nil", got)
				}
				return
			}
			if !errors.Is(got, test.want) {
				t.Fatalf("mapCredentialCallError()=%v, want wrapped %v", got, test.want)
			}
		})
	}
}

func TestCredentialManagerUnavailableForLogonIsExact(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "exact missing session", err: errorNoSuchLogonSession, want: true},
		{name: "wrapped missing session", err: fmt.Errorf("CredWriteW: %w", errorNoSuchLogonSession), want: true},
		{name: "access denied", err: syscall.Errno(5)},
		{name: "invalid parameter", err: syscall.Errno(87)},
		{name: "logon failure", err: syscall.Errno(1326)},
		{name: "not found", err: ErrNotFound},
		{name: "unexpected", err: errors.New("unexpected credential manager failure")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := credentialManagerUnavailableForLogon(test.err); got != test.want {
				t.Fatalf("credentialManagerUnavailableForLogon(%v)=%v, want %v", test.err, got, test.want)
			}
		})
	}
}

func TestWindowsCredentialManagerMalformedReferenceStillFails(t *testing.T) {
	resolver := New("controller-test")
	for _, reference := range []string{"os:../secret", "os:has space", "plain-secret"} {
		err := resolver.Set(reference, "value")
		if err == nil {
			t.Fatalf("malformed reference %q was accepted", reference)
		}
		if credentialManagerUnavailableForLogon(err) {
			t.Fatalf("malformed reference %q was misclassified as an unavailable logon vault: %v", reference, err)
		}
	}
}
