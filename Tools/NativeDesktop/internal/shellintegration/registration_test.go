package shellintegration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrationSpecQuotesExecutableAndURI(t *testing.T) {
	t.Parallel()

	executable := makeTestExecutable(t, "PCController.exe")
	spec, err := makeRegistrationSpec(executable)
	if err != nil {
		t.Fatal(err)
	}
	expectedCommand := `"` + executable + `" /uri "%1"`
	if spec.command != expectedCommand {
		t.Fatalf("command = %q, want %q", spec.command, expectedCommand)
	}
	if len(spec.values) != 4 {
		t.Fatalf("registry value count = %d, want 4", len(spec.values))
	}
	if got := spec.values[len(spec.values)-1]; got.path != protocolOpenPath ||
		got.name != "" || got.value != expectedCommand {
		t.Fatalf("last value = %+v, want ownership command", got)
	}
	if got := spec.values[2].value; got != `"`+executable+`",0` {
		t.Fatalf("icon value = %q", got)
	}
	if got := spec.values[1]; got.name != protocolURLValueName || got.value != "" {
		t.Fatalf("URL Protocol value = %+v", got)
	}
}

func TestRegistrationCommandRejectsUnsafeOrNonFilePaths(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	tests := []string{
		"",
		" \t ",
		filepath.Join(directory, "missing.exe"),
		directory,
		filepath.Join(directory, `bad"name.exe`),
		"bad\x00name.exe",
		"bad\rname.exe",
	}
	for _, path := range tests {
		path := path
		t.Run(strings.ReplaceAll(path, "\\", "_"), func(t *testing.T) {
			t.Parallel()
			if command, err := RegistrationCommand(path); err == nil {
				t.Fatalf("RegistrationCommand(%q) = %q, want error", path, command)
			}
		})
	}
}

func TestRegistrationOwnershipComparisonIsCaseInsensitiveButExact(t *testing.T) {
	t.Parallel()

	owned := `"C:\Program Files\PCControllerDesktop\PCControllerDesktop.exe" /uri "%1"`
	if !registrationOwnedBy(
		"  "+strings.ToUpper(owned)+"  ",
		owned,
	) {
		t.Fatal("equivalent Windows registration was not recognized")
	}
	if registrationOwnedBy(
		`"C:\Other\PCControllerDesktop.exe" /uri "%1"`,
		owned,
	) {
		t.Fatal("different executable was recognized as owner")
	}
	if registrationOwnedBy(
		`"C:\Program Files\PCControllerDesktop\PCControllerDesktop.exe" "%1"`,
		owned,
	) {
		t.Fatal("different activation command was recognized as owner")
	}
}

func TestOwnershipErrorSupportsErrorsIs(t *testing.T) {
	t.Parallel()

	err := &OwnershipError{RegisteredCommand: `"C:\Other\PCControllerDesktop.exe" "%1"`}
	if !errors.Is(err, ErrRegistrationOwned) {
		t.Fatalf("OwnershipError does not wrap ErrRegistrationOwned: %v", err)
	}
	if !strings.Contains(err.Error(), "Other") {
		t.Fatalf("OwnershipError omitted conflicting command: %v", err)
	}
}

func makeTestExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("test executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(absolute)
}
