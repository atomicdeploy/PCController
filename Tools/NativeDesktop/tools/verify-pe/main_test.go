package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/terminal"
)

func TestMachineForArchitecture(t *testing.T) {
	t.Parallel()
	cases := map[string]uint16{
		"amd64": pe.IMAGE_FILE_MACHINE_AMD64,
		"386":   pe.IMAGE_FILE_MACHINE_I386,
		"arm64": pe.IMAGE_FILE_MACHINE_ARM64,
	}
	for architecture, expected := range cases {
		architecture, expected := architecture, expected
		t.Run(architecture, func(t *testing.T) {
			t.Parallel()
			actual, err := machineForArchitecture(architecture)
			if err != nil {
				t.Fatal(err)
			}
			if actual != expected {
				t.Fatalf("machine = %#x, want %#x", actual, expected)
			}
		})
	}
	if _, err := machineForArchitecture("mips"); err == nil {
		t.Fatal("unsupported architecture unexpectedly succeeded")
	}
}

func TestVerifySubsystemRequiresWindowsConsole(t *testing.T) {
	t.Parallel()
	if err := verifySubsystem(pe.IMAGE_SUBSYSTEM_WINDOWS_CUI); err != nil {
		t.Fatalf("console subsystem was rejected: %v", err)
	}
	err := verifySubsystem(pe.IMAGE_SUBSYSTEM_WINDOWS_GUI)
	if err == nil {
		t.Fatal("GUI subsystem unexpectedly passed console verification")
	}
	if !strings.Contains(err.Error(), "console/CUI") {
		t.Fatalf("subsystem error = %q, want console/CUI explanation", err)
	}
}

func TestVerifyEditionRequiresMatchingNativeAndWebDebugPayload(t *testing.T) {
	t.Parallel()
	develop := executable{
		fileData: []byte(
			`prefix{"edition":"develop","developer_tools":true}suffix`,
		),
		buildTags:     "pccontroller_desktop_develop",
		debugSections: true,
		symbolCount:   12,
	}
	if err := verifyEdition(develop, "develop"); err != nil {
		t.Fatalf("valid development edition was rejected: %v", err)
	}

	production := executable{
		fileData: []byte(
			`prefix{"edition":"production","developer_tools":false}suffix`,
		),
		buildTags:   "pccontroller_desktop_production",
		symbolCount: 0,
	}
	if err := verifyEdition(production, "production"); err != nil {
		t.Fatalf("valid production edition was rejected: %v", err)
	}

	cases := map[string]struct {
		image   executable
		edition string
	}{
		"develop stripped": {
			image: executable{
				fileData:    develop.fileData,
				buildTags:   develop.buildTags,
				symbolCount: 0,
			},
			edition: "develop",
		},
		"production has dwarf": {
			image: executable{
				fileData:      production.fileData,
				buildTags:     production.buildTags,
				debugSections: true,
				symbolCount:   0,
			},
			edition: "production",
		},
		"wrong native tag": {
			image: executable{
				fileData:      develop.fileData,
				buildTags:     "pccontroller_desktop_production",
				debugSections: true,
				symbolCount:   1,
			},
			edition: "develop",
		},
		"wrong web marker": {
			image: executable{
				fileData:    []byte("no edition marker"),
				buildTags:   production.buildTags,
				symbolCount: 0,
			},
			edition: "production",
		},
		"both build tags": {
			image: executable{
				fileData: production.fileData,
				buildTags: "pccontroller_desktop_develop," +
					"pccontroller_desktop_production",
				symbolCount: 0,
			},
			edition: "production",
		},
		"unknown": {
			image:   production,
			edition: "nightly",
		},
	}
	for name, test := range cases {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := verifyEdition(test.image, test.edition); err == nil {
				t.Fatal("invalid edition unexpectedly passed verification")
			}
		})
	}
}

func TestSourceManifestDetachedConsolePolicyPlacement(t *testing.T) {
	t.Parallel()
	manifestPath := filepath.Join("..", "..", "resources", "PCControllerDesktop.exe.manifest")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyDetachedConsolePolicy(manifest); err != nil {
		t.Fatal(err)
	}
}

func TestDetachedConsolePolicyRequiresExactHierarchyAndNamespace(t *testing.T) {
	t.Parallel()
	valid := []byte(`
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <consoleAllocationPolicy xmlns="http://schemas.microsoft.com/SMI/2024/WindowsSettings">detached</consoleAllocationPolicy>
    </windowsSettings>
  </application>
</assembly>`)
	if err := verifyDetachedConsolePolicy(valid); err != nil {
		t.Fatalf("valid policy was rejected: %v", err)
	}

	cases := map[string]string{
		"wrong namespace": strings.Replace(
			string(valid),
			windowsSettings2024Schema,
			"http://schemas.microsoft.com/SMI/2020/WindowsSettings",
			1,
		),
		"wrong hierarchy": strings.NewReplacer(
			"<windowsSettings>", "<settings>",
			"</windowsSettings>", "</settings>",
		).Replace(string(valid)),
		"wrong value": strings.Replace(string(valid), ">detached<", ">attach<", 1),
	}
	for name, manifest := range cases {
		name, manifest := name, manifest
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := verifyDetachedConsolePolicy([]byte(manifest)); err == nil {
				t.Fatal("invalid detached-console policy unexpectedly passed")
			}
		})
	}
}

func TestReadResourceTypes(t *testing.T) {
	t.Parallel()
	data := make([]byte, 16+4*8)
	binary.LittleEndian.PutUint16(data[14:16], 4)
	for index, resourceType := range []uint32{3, 14, 16, 24} {
		offset := 16 + index*8
		binary.LittleEndian.PutUint32(data[offset:offset+4], resourceType)
		binary.LittleEndian.PutUint32(data[offset+4:offset+8], 0x80000040)
	}
	actual, err := readResourceTypes(data)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[uint32]bool{3: true, 14: true, 16: true, 24: true}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("resource types = %#v, want %#v", actual, expected)
	}
}

func TestReadResourceTypesRejectsTruncatedDirectory(t *testing.T) {
	t.Parallel()
	data := make([]byte, 16)
	binary.LittleEndian.PutUint16(data[14:16], 1)
	if _, err := readResourceTypes(data); err == nil {
		t.Fatal("truncated resource directory unexpectedly succeeded")
	}
}

func TestReadResourceIDs(t *testing.T) {
	t.Parallel()
	const childOffset = 64
	data := make([]byte, childOffset+16+2*8)
	binary.LittleEndian.PutUint16(data[14:16], 1)
	binary.LittleEndian.PutUint32(data[16:20], 14)
	binary.LittleEndian.PutUint32(data[20:24], 0x80000000|childOffset)
	binary.LittleEndian.PutUint16(data[childOffset+14:childOffset+16], 2)
	binary.LittleEndian.PutUint32(data[childOffset+16:childOffset+20], 2)
	binary.LittleEndian.PutUint32(data[childOffset+24:childOffset+28], applicationIconResourceID)

	actual, err := readResourceIDs(data, 14)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[uint32]bool{2: true, applicationIconResourceID: true}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("resource IDs = %#v, want %#v", actual, expected)
	}

	missing, err := readResourceIDs(data, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing resource type returned IDs: %#v", missing)
	}
}

func TestReadResourceIDsRejectsMalformedChildDirectory(t *testing.T) {
	t.Parallel()
	data := make([]byte, 24)
	binary.LittleEndian.PutUint16(data[14:16], 1)
	binary.LittleEndian.PutUint32(data[16:20], 14)
	binary.LittleEndian.PutUint32(data[20:24], 0x80000040)
	if _, err := readResourceIDs(data, 14); err == nil {
		t.Fatal("truncated child resource directory unexpectedly succeeded")
	}
}

func TestParsePELayoutRejectsMalformedHeaders(t *testing.T) {
	t.Parallel()

	truncatedDOS := []byte("MZ")

	badPEOffset := make([]byte, 0x40)
	copy(badPEOffset, "MZ")
	binary.LittleEndian.PutUint32(badPEOffset[0x3c:0x40], 0xfffffff0)

	badSignature := make([]byte, 0x40+24)
	copy(badSignature, "MZ")
	binary.LittleEndian.PutUint32(badSignature[0x3c:0x40], 0x40)
	copy(badSignature[0x40:], "NOPE")

	truncatedOptional := make([]byte, 0x40+24)
	copy(truncatedOptional, "MZ")
	binary.LittleEndian.PutUint32(truncatedOptional[0x3c:0x40], 0x40)
	copy(truncatedOptional[0x40:], []byte{'P', 'E', 0, 0})
	binary.LittleEndian.PutUint16(truncatedOptional[0x46:0x48], 1)
	binary.LittleEndian.PutUint16(truncatedOptional[0x54:0x56], 0xf0)

	cases := map[string][]byte{
		"truncated DOS header":      truncatedDOS,
		"out-of-range PE offset":    badPEOffset,
		"invalid PE signature":      badSignature,
		"truncated optional header": truncatedOptional,
	}
	for name, data := range cases {
		name, data := name, data
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := parsePELayout(data); err == nil {
				t.Fatal("malformed PE image unexpectedly succeeded")
			}
		})
	}
}

func TestUTF16LE(t *testing.T) {
	t.Parallel()
	actual := utf16LE("A💡")
	expected := []byte{0x41, 0x00, 0x3d, 0xd8, 0xa1, 0xdc}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("UTF-16LE = %x, want %x", actual, expected)
	}
}

func TestPackedSectionDetection(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"UPX0", "upx1", ".upx"} {
		if !isPackedSection(name) {
			t.Errorf("packed section %q was not detected", name)
		}
	}
	for _, name := range []string{".text", ".rsrc", "UPSTREAM"} {
		if isPackedSection(name) {
			t.Errorf("ordinary section %q was detected as packed", name)
		}
	}
}

func TestPrintStatusUsesPlainASCIIFallbackWhenRedirected(t *testing.T) {
	clearTerminalEnvironment(t)
	var output bytes.Buffer

	printStatus(&output, terminal.BoldGreen, "✅", "[OK]", "release ready")

	if got, want := output.String(), "[OK] release ready\n"; got != want {
		t.Fatalf("redirected status = %q, want %q", got, want)
	}
	if strings.Contains(output.String(), "\x1b") {
		t.Fatal("redirected status leaked an escape character")
	}
	if strings.Contains(output.String(), "✅") {
		t.Fatal("redirected status leaked emoji without an explicit override")
	}
}

func TestPrintStatusUsesCompleteResetWhenForced(t *testing.T) {
	clearTerminalEnvironment(t)
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("PCCONTROLLER_DESKTOP_EMOJI", "always")
	var output bytes.Buffer

	printStatus(&output, terminal.BoldGreen, "✅", "[OK]", "release ready")

	want := terminal.BoldGreen + "✅" + terminal.Reset + " release ready\n"
	if got := output.String(); got != want {
		t.Fatalf("forced status = %q, want %q", got, want)
	}
	if strings.Count(output.String(), "\x1b[") != 2 {
		t.Fatalf("forced status has an incomplete SGR sequence: %q", output.String())
	}
}

func clearTerminalEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"NO_COLOR",
		"NO_EMOJI",
		"TERM",
		"CLICOLOR",
		"CLICOLOR_FORCE",
		"FORCE_COLOR",
		"PCCONTROLLER_DESKTOP_EMOJI",
	} {
		value, present := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if present {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}
