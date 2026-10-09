package programmer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntelHexParseInspectAndCanonicalRoundTrip(t *testing.T) {
	image := &IntelHexImage{
		data: map[uint32]byte{
			0x0000: 0x11, 0x0001: 0x22, 0x0012: 0x33,
			0x10002: 0x44, 0x10003: 0x55,
		},
		startLinear: uint32Pointer(0x12345678),
	}
	canonical, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(canonical, []byte(":020000040001F9\n")) ||
		!bytes.Contains(canonical, []byte(":0400000512345678E3\n")) {
		t.Fatalf("canonical output lacks address/start records:\n%s", canonical)
	}
	parsed, err := ParseIntelHex(bytes.NewReader(canonical))
	if err != nil {
		t.Fatal(err)
	}
	second, err := parsed.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, second) {
		t.Fatalf("canonical round trip drifted:\n%s\n%s", canonical, second)
	}
	inspection, err := parsed.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if inspection.DataBytes != 5 || inspection.MinimumAddress != 0 ||
		inspection.MaximumAddress != 0x10003 || len(inspection.Segments) != 3 {
		t.Fatalf("unexpected inspection: %#v", inspection)
	}
}

func TestLoadIntelHexCountsNonEmptyRecordsForManifestMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firmware.hex")
	content := []byte("\n:0100000001FE\r\n\r\n:00000001FF\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	document, err := LoadIntelHex(path)
	if err != nil {
		t.Fatal(err)
	}
	if document.Records != 2 {
		t.Fatalf("record count=%d, want 2", document.Records)
	}
}

func TestIntelHexParserRejectsCorruptionAndAmbiguity(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"checksum", ":0100000001FF\n:00000001FF\n", "checksum"},
		{"missing EOF", ":0100000001FE\n", "missing EOF"},
		{"after EOF", ":00000001FF\n:0100000001FE\n", "after EOF"},
		{"overlap", ":0100000001FE\n:0100000002FD\n:00000001FF\n", "overlapping"},
		{"record shape", ":0100000400FB\n:00000001FF\n", "malformed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseIntelHex(strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestApplyIntelHexPatchPlanGuardsAndHashes(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.hex")
	image := &IntelHexImage{data: map[uint32]byte{
		0x100: 0x10, 0x101: 0x20, 0x102: 0x30, 0x103: 0x40,
	}}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "patched.hex")
	plan := IntelHexPatchPlan{
		SourceSHA256: sha256Hex(content),
		Regions:      []NamedPatchRegion{{Name: "identity", Start: 0x100, Length: 4}},
		Patches: []RegionPatch{{
			Region: "identity", Offset: 1,
			ExpectedOld: []byte{0x20, 0x30}, Replacement: []byte{0xAA, 0xBB},
		}},
	}
	result, err := ApplyIntelHexPatchPlan(
		source, output, DefaultATmega328PFlashLayout(), plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceSHA256 != sha256Hex(content) || result.OutputSHA256 == result.SourceSHA256 ||
		len(result.Patches) != 1 || result.Patches[0].Address != 0x101 {
		t.Fatalf("unexpected patch result: %#v", result)
	}
	patched, err := LoadIntelHex(output)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := patched.Image.BytesAt(0x100, 4)
	if err != nil || !bytes.Equal(actual, []byte{0x10, 0xAA, 0xBB, 0x40}) {
		t.Fatalf("patched bytes=%X err=%v", actual, err)
	}
	if _, err := ApplyIntelHexPatchPlan(
		source, output, DefaultATmega328PFlashLayout(), plan,
	); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected no-overwrite error, got %v", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(directory, ".pccontroller-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("atomic writer left temporary files: %v %v", leftovers, err)
	}
}

func TestApplyIntelHexPatchPlanRejectsEveryUnsafeVariation(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.hex")
	image := &IntelHexImage{data: map[uint32]byte{
		0x100: 0x10, 0x101: 0x20,
		0x7F00: 0xA0, 0x7F01: 0xB0,
	}}
	content, _ := image.Canonical()
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	base := IntelHexPatchPlan{
		SourceSHA256: sha256Hex(content),
		Regions:      []NamedPatchRegion{{Name: "app", Start: 0x100, Length: 2}},
		Patches:      []RegionPatch{{Region: "app", ExpectedOld: []byte{0x10}, Replacement: []byte{0x11}}},
	}
	tests := []struct {
		name, want string
		mutate     func(*IntelHexPatchPlan)
	}{
		{"missing source hash", "64 hexadecimal", func(p *IntelHexPatchPlan) { p.SourceSHA256 = "" }},
		{"wrong source hash", "mismatch", func(p *IntelHexPatchPlan) { p.SourceSHA256 = strings.Repeat("0", 64) }},
		{"size change", "preserve size", func(p *IntelHexPatchPlan) { p.Patches[0].Replacement = []byte{1, 2} }},
		{"expected mismatch", "expected-old mismatch", func(p *IntelHexPatchPlan) { p.Patches[0].ExpectedOld = []byte{0x99} }},
		{"region bounds", "exceeds region", func(p *IntelHexPatchPlan) { p.Patches[0].Offset = 2 }},
		{"unknown region", "unknown region", func(p *IntelHexPatchPlan) { p.Patches[0].Region = "nope" }},
		{"empty plan", "contains no patches", func(p *IntelHexPatchPlan) { p.Patches = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := clonePatchPlan(base)
			test.mutate(&plan)
			_, err := ApplyIntelHexPatchPlan(
				source, filepath.Join(directory, test.name+".hex"),
				DefaultATmega328PFlashLayout(), plan,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
	bootPlan := IntelHexPatchPlan{
		SourceSHA256: sha256Hex(content),
		Regions:      []NamedPatchRegion{{Name: "boot", Start: 0x7F00, Length: 2}},
		Patches:      []RegionPatch{{Region: "boot", ExpectedOld: []byte{0xA0}, Replacement: []byte{0xA1}}},
	}
	_, err := ApplyIntelHexPatchPlan(
		source, filepath.Join(directory, "boot-denied.hex"),
		DefaultATmega328PFlashLayout(), bootPlan,
	)
	if err == nil || !strings.Contains(err.Error(), "bootloader") {
		t.Fatalf("expected bootloader guard, got %v", err)
	}
	bootPlan.BootloaderWhitelist = []string{"boot"}
	if _, err := ApplyIntelHexPatchPlan(
		source, filepath.Join(directory, "boot-allowed.hex"),
		DefaultATmega328PFlashLayout(), bootPlan,
	); err != nil {
		t.Fatalf("explicit bootloader whitelist failed: %v", err)
	}
}

func clonePatchPlan(input IntelHexPatchPlan) IntelHexPatchPlan {
	result := input
	result.Regions = append([]NamedPatchRegion(nil), input.Regions...)
	result.BootloaderWhitelist = append([]string(nil), input.BootloaderWhitelist...)
	result.Patches = make([]RegionPatch, len(input.Patches))
	for index, patch := range input.Patches {
		result.Patches[index] = patch
		result.Patches[index].ExpectedOld = append([]byte(nil), patch.ExpectedOld...)
		result.Patches[index].Replacement = append([]byte(nil), patch.Replacement...)
	}
	return result
}
