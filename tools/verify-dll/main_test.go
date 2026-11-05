package main

import (
	"debug/pe"
	"testing"
)

func TestMachineForArchitecture(t *testing.T) {
	t.Parallel()

	tests := map[string]uint16{
		"amd64": pe.IMAGE_FILE_MACHINE_AMD64,
		"386":   pe.IMAGE_FILE_MACHINE_I386,
	}
	for architecture, expected := range tests {
		actual, err := machineForArchitecture(architecture)
		if err != nil {
			t.Fatalf("%s: %v", architecture, err)
		}
		if actual != expected {
			t.Fatalf("%s machine = %#x, want %#x", architecture, actual, expected)
		}
	}
	if _, err := machineForArchitecture("arm64"); err == nil {
		t.Fatal("unsupported ARM64 SDK architecture was accepted")
	}
}

func TestCompareExportsIsExactAndOrderIndependent(t *testing.T) {
	t.Parallel()

	if err := compareExports(
		[]string{"Second", "First"},
		[]string{"First", "Second"},
	); err != nil {
		t.Fatalf("equal exports: %v", err)
	}
	if err := compareExports(
		[]string{"First", "PrivateGlue"},
		[]string{"First"},
	); err == nil {
		t.Fatal("unexpected glue export was accepted")
	}
}

func TestUTF16LE(t *testing.T) {
	t.Parallel()

	actual := utf16LE("A⚡")
	expected := []byte{0x41, 0x00, 0xa1, 0x26}
	if string(actual) != string(expected) {
		t.Fatalf("UTF-16LE = %v, want %v", actual, expected)
	}
}
