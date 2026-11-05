//go:build windows

package shellintegration

import (
	"errors"
	"testing"
)

func TestSetAppUserModelIDUsesStableIdentity(t *testing.T) {
	original := setProcessAppUserModelID
	t.Cleanup(func() {
		setProcessAppUserModelID = original
	})

	var got string
	setProcessAppUserModelID = func(value string) error {
		got = value
		return nil
	}
	if err := SetAppUserModelID(); err != nil {
		t.Fatal(err)
	}
	if got != AppUserModelID {
		t.Fatalf("AppUserModelID = %q", got)
	}
	if AppUserModelID != "DRSDavidSoft.PCController" {
		t.Fatalf("stable AppUserModelID changed to %q", AppUserModelID)
	}
}

func TestSetAppUserModelIDPropagatesFailure(t *testing.T) {
	original := setProcessAppUserModelID
	t.Cleanup(func() {
		setProcessAppUserModelID = original
	})

	want := errors.New("injected AppUserModelID failure")
	setProcessAppUserModelID = func(string) error { return want }
	if err := SetAppUserModelID(); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
