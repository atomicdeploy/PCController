package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAcceptsQuotedAndExportedValues(t *testing.T) {
	values, err := parse("ALPHA=one # comment\nexport BETA=\"two words\"\nGAMMA='three # literal'\n", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := values["ALPHA"], "one"; got != want {
		t.Fatalf("ALPHA=%q want %q", got, want)
	}
	if got, want := values["BETA"], "two words"; got != want {
		t.Fatalf("BETA=%q want %q", got, want)
	}
	if got, want := values["GAMMA"], "three # literal"; got != want {
		t.Fatalf("GAMMA=%q want %q", got, want)
	}
}

func TestParseRejectsMalformedAssignment(t *testing.T) {
	_, err := parse("NO_EQUALS\n", "fixture.env")
	if err == nil || !strings.Contains(err.Error(), "fixture.env:1: expected KEY=VALUE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectFileDoesNotSearchPastAStandaloneWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	path, err := projectFile(directory, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(directory, ".env"); path != want {
		t.Fatalf("path=%q want %q", path, want)
	}
}

func TestLoadProcessAppliesFileWithoutOverridingInheritedValues(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".env"), []byte("ENVFILE_TEST_INHERITED=file\nENVFILE_TEST_VALUE=loaded\nENVFILE_TEST_APPLIED=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	t.Setenv("PCCONTROLLER_ENV_FILE", "")
	t.Setenv("ENVFILE_TEST_INHERITED", "process")
	t.Setenv("ENVFILE_TEST_VALUE", "")
	result, err := LoadProcess()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Loaded || result.Path != filepath.Join(directory, ".env") {
		t.Fatalf("unexpected result: %#v", result)
	}
	if got, want := os.Getenv("ENVFILE_TEST_INHERITED"), "process"; got != want {
		t.Fatalf("inherited=%q want %q", got, want)
	}
	if got, want := os.Getenv("ENVFILE_TEST_VALUE"), ""; got != want {
		t.Fatalf("empty inherited value must win, got %q", got)
	}
	if got, want := os.Getenv("ENVFILE_TEST_APPLIED"), "yes"; got != want {
		t.Fatalf("applied=%q want %q", got, want)
	}
}
