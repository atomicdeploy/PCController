//go:build windows

package secretstore

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestWindowsCredentialManagerRoundTrip(t *testing.T) {
	resolver := New(fmt.Sprintf("controller-test/%d/%d", os.Getpid(), time.Now().UnixNano()))
	if status := resolver.Status(); !status.Available || status.Provider != "windows-credential-manager" {
		t.Fatalf("unexpected status: %#v", status)
	}
	reference := "os:roundtrip"
	defer func() { _ = resolver.Delete(reference) }()
	if err := resolver.Set(reference, "credential-manager-roundtrip"); err != nil {
		t.Fatal(err)
	}
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
