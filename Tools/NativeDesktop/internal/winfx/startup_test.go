//go:build windows

package winfx

import (
	"errors"
	"testing"
)

func TestQuoteRunCommand(t *testing.T) {
	got, err := quoteRunCommand(`C:\Program Files\PCController\PCControllerDesktop.exe`)
	if err != nil {
		t.Fatal(err)
	}
	want := `"C:\Program Files\PCController\PCControllerDesktop.exe"`
	if got != want {
		t.Fatalf("quoteRunCommand = %q, want %q", got, want)
	}
}

func TestQuoteRunCommandRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"", `relative\PCControllerDesktop.exe`, "C:\\bad\"name.exe", "C:\\bad\nname.exe"} {
		t.Run(path, func(t *testing.T) {
			_, err := quoteRunCommand(path)
			if !errors.Is(err, ErrInvalidExecutablePath) {
				t.Fatalf("quoteRunCommand(%q) error = %v", path, err)
			}
		})
	}
}

func TestDecideStartWithWindowsActionPreservesForeignValues(t *testing.T) {
	t.Parallel()

	expected := `"C:\Apps\PCControllerDesktop\PCControllerDesktop.exe"`
	foreign := `"D:\Portable\PCControllerDesktop.exe"`
	tests := []struct {
		name    string
		enabled bool
		current string
		exists  bool
		want    startWithWindowsAction
	}{
		{"enable absent", true, "", false, startWithWindowsSet},
		{"disable absent", false, "", false, startWithWindowsNoop},
		{"enable owned", true, expected, true, startWithWindowsNoop},
		{"disable owned", false, expected, true, startWithWindowsDelete},
		{
			"enable owned different casing",
			true,
			`"c:\apps\pccontrollerdesktop\pccontrollerdesktop.exe"`,
			true,
			startWithWindowsNoop,
		},
		{"enable foreign", true, foreign, true, startWithWindowsConflict},
		{"disable foreign", false, foreign, true, startWithWindowsNoop},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := decideStartWithWindowsAction(
				test.enabled,
				test.current,
				test.exists,
				expected,
			)
			if got != test.want {
				t.Fatalf("action = %d, want %d", got, test.want)
			}
		})
	}
}

func TestStartWithWindowsPersistenceDoesNotMutateOnStartup(t *testing.T) {
	t.Parallel()

	var calls []bool
	persistence, err := newStartWithWindowsPersistence(
		`C:\Apps\PCControllerDesktop\PCControllerDesktop.exe`,
		false,
		func(enabled bool, _ string) error {
			calls = append(calls, enabled)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("constructor registry calls = %v", calls)
	}
	if err := persistence.ApplySaved(false); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("unchanged saved setting registry calls = %v", calls)
	}
	if err := persistence.ApplySaved(true); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !calls[0] {
		t.Fatalf("changed saved setting registry calls = %v", calls)
	}
}

func TestReconcileStartWithWindowsSettingIsReadOnlyAndFailSafe(t *testing.T) {
	t.Parallel()

	var calls int
	got, err := reconcileStartWithWindowsSetting(
		`C:\Apps\PCControllerDesktop\PCControllerDesktop.exe`,
		false,
		func(string) (bool, error) {
			calls++
			return true, nil
		},
	)
	if err != nil || !got {
		t.Fatalf("owned reconciliation = %v, %v", got, err)
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}

	injected := errors.New("injected read failure")
	got, err = reconcileStartWithWindowsSetting(
		`C:\Apps\PCControllerDesktop\PCControllerDesktop.exe`,
		true,
		func(string) (bool, error) {
			return false, injected
		},
	)
	if !got || !errors.Is(err, injected) {
		t.Fatalf("failed reconciliation = %v, %v; want configured true", got, err)
	}
}

func TestStartWithWindowsPersistenceDoesNotRetryOnUnrelatedSave(t *testing.T) {
	t.Parallel()

	injected := errors.New("injected registry failure")
	var calls int
	persistence, err := newStartWithWindowsPersistence(
		`C:\Apps\PCControllerDesktop\PCControllerDesktop.exe`,
		false,
		func(bool, string) error {
			calls++
			return injected
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.ApplySaved(true); !errors.Is(err, injected) {
		t.Fatalf("first change error = %v", err)
	}
	if err := persistence.ApplySaved(true); err != nil {
		t.Fatalf("unchanged retry error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("registry calls = %d, want 1", calls)
	}
}

func TestStartWithWindowsOwnershipErrorUnwraps(t *testing.T) {
	t.Parallel()

	err := &StartWithWindowsOwnershipError{
		Command: `"D:\Portable\PCControllerDesktop.exe"`,
	}
	if !errors.Is(err, ErrStartWithWindowsOwnedByAnotherCommand) {
		t.Fatalf("ownership error does not unwrap: %v", err)
	}
}
