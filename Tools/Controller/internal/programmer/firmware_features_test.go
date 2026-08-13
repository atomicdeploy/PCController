package programmer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeFirmwareFeaturesRejectsRawCompilerInput(t *testing.T) {
	features, err := NormalizeFirmwareFeatures([]string{
		" EEPROM-MENU-LABELS ", "eeprom-boot-opcodes", "eeprom-boot-opcodes",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []FirmwareFeature{FirmwareFeatureEEPROMBootOpcodes, FirmwareFeatureEEPROMMenuLabels}
	if len(features) != len(want) {
		t.Fatalf("features=%v want=%v", features, want)
	}
	for index := range want {
		if features[index] != want[index] {
			t.Fatalf("features=%v want=%v", features, want)
		}
	}
	for _, invalid := range []string{"", "-DUNSAFE=1", "eeprom-boot-opcodes --evil", "unknown"} {
		if _, err := NormalizeFirmwareFeatures([]string{invalid}); err == nil {
			t.Fatalf("feature %q unexpectedly accepted", invalid)
		}
	}
}

func TestFirmwareFeaturesAreIdentityBoundAndBecomeOnlyKnownDefines(t *testing.T) {
	root := firmwareFeatureCompileFixture(t)
	t.Setenv("PCCONTROLLER_ARDUINO_CACHE", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("PCCONTROLLER_BUILD_TIMESTAMP", "35019D5D")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	baseline, baselineIdentity, err := PlanCompile(Options{
		Method: MethodCompile, SketchPath: root, ArduinoCLI: executable,
	})
	if err != nil {
		t.Fatal(err)
	}
	rawHash, rawSHA256, rawFiles, err := firmwareSourceDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if baselineIdentity.SourceHash != rawHash || baselineIdentity.SourceSHA256 != rawSHA256 || baselineIdentity.SourceFiles != rawFiles {
		t.Fatalf("feature-off identity changed: %#v raw=%08X/%s/%d", baselineIdentity, rawHash, rawSHA256, rawFiles)
	}

	enabled, enabledIdentity, err := PlanCompile(Options{
		Method: MethodCompile, SketchPath: root, ArduinoCLI: executable,
		FirmwareFeatures: []FirmwareFeature{FirmwareFeatureEEPROMMenuLabels, FirmwareFeatureEEPROMBootOpcodes},
	})
	if err != nil {
		t.Fatal(err)
	}
	if enabledIdentity.SourceHash == baselineIdentity.SourceHash || enabledIdentity.SourceSHA256 == baselineIdentity.SourceSHA256 {
		t.Fatalf("feature selection did not change identity: baseline=%#v enabled=%#v", baselineIdentity, enabledIdentity)
	}
	if baselineIdentity.BuildPath == enabledIdentity.BuildPath || baselineIdentity.SketchPath == enabledIdentity.SketchPath {
		t.Fatalf("feature selection reused the feature-off compile cache: baseline=%#v enabled=%#v", baselineIdentity, enabledIdentity)
	}
	if got := firmwareFeatureNames(enabledIdentity.Features); strings.Join(got, ",") != "eeprom-boot-opcodes,eeprom-menu-labels" {
		t.Fatalf("identity features=%v", got)
	}
	permuted, permutedIdentity, err := PlanCompile(Options{
		Method: MethodCompile, SketchPath: root, ArduinoCLI: executable,
		FirmwareFeatures: []FirmwareFeature{
			FirmwareFeatureEEPROMBootOpcodes,
			FirmwareFeatureEEPROMMenuLabels,
			FirmwareFeatureEEPROMBootOpcodes,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if permutedIdentity.SourceHash != enabledIdentity.SourceHash ||
		permutedIdentity.SourceSHA256 != enabledIdentity.SourceSHA256 ||
		permutedIdentity.BuildPath != enabledIdentity.BuildPath ||
		permutedIdentity.SketchPath != enabledIdentity.SketchPath {
		t.Fatalf("permuted identity=%#v enabled=%#v", permutedIdentity, enabledIdentity)
	}
	permuted.FirmwareFeatures = []FirmwareFeature{FirmwareFeatureEEPROMMenuLabels}
	if _, err := Build(permuted); err == nil ||
		!strings.Contains(err.Error(), "changed after planning") {
		t.Fatalf("planned feature mutation error=%v", err)
	}

	baselineCommand, err := Build(baseline)
	if err != nil {
		t.Fatal(err)
	}
	enabledCommand, err := Build(enabled)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(baselineCommand.Args, " "); strings.Contains(got, "PCCONTROLLER_ENABLE_EEPROM_") {
		t.Fatalf("feature-off command leaked EEPROM feature macro: %s", got)
	}
	for _, define := range []string{
		"-DPCCONTROLLER_ENABLE_EEPROM_BOOT_OPCODES=1",
		"-DPCCONTROLLER_ENABLE_EEPROM_MENU_LABELS=1",
	} {
		if got := strings.Join(enabledCommand.Args, " "); !strings.Contains(got, define) {
			t.Fatalf("feature-on command missing %q: %s", define, got)
		}
	}
}

func firmwareFeatureCompileFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "PCController.ino"), []byte("void setup() {}\nvoid loop() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
