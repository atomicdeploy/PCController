package programmer

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionFirmwareFeatureMatrixMatchesSourceAndWireContract(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	matrix, err := ResolveFirmwareFeatureMatrix(root)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Profile.ID != "full-peripheral" || matrix.Profile.Value != 0 ||
		matrix.BuildFlags != 0xD9 || matrix.BuildFlagsHex != "0xD9" ||
		matrix.CapabilityMask != 0x957DFFBF || matrix.CapabilitiesHex != "0x957DFFBF" {
		t.Fatalf("unexpected production feature matrix: %#v", matrix)
	}
	want := FirmwareFeatureDefaults()
	if len(matrix.Features) != len(want) || len(matrix.Capabilities) != 32 {
		t.Fatalf("features=%d want=%d capabilities=%d", len(matrix.Features), len(want), len(matrix.Capabilities))
	}
	for _, feature := range matrix.Features {
		if feature.Enabled != want[feature.Macro] {
			t.Fatalf("%s enabled=%v want=%v", feature.Macro, feature.Enabled, want[feature.Macro])
		}
		if feature.Docs == "" || feature.Source != firmwareConfigSource {
			t.Fatalf("feature lacks linked ownership: %#v", feature)
		}
	}
}

func TestFirmwareFeatureMatrixRendersUnicodeMarkdownAndJSON(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	matrix, err := ResolveFirmwareFeatureMatrix(root)
	if err != nil {
		t.Fatal(err)
	}
	var unicode bytes.Buffer
	if err := RenderFirmwareFeatureMatrix(&unicode, matrix, FirmwareFeatureRenderOptions{Format: "unicode"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"┌", "✓ included", "— excluded", "PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES", "0x957DFFBF"} {
		if !strings.Contains(unicode.String(), want) {
			t.Fatalf("unicode output lacks %q:\n%s", want, unicode.String())
		}
	}
	var markdown bytes.Buffer
	if err := RenderFirmwareFeatureMatrix(&markdown, matrix, FirmwareFeatureRenderOptions{
		Format: "markdown", RepositoryURL: "https://example.test/example/controller", Revision: "deadbeef",
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| ✅ included |", "| ❌ excluded |", "/blob/deadbeef/ProjectConfig.h",
		"docs/Firmware-Features-and-Profiles.md#local-audio-cues", "Runtime capability bits",
	} {
		if !strings.Contains(markdown.String(), want) {
			t.Fatalf("Markdown output lacks %q:\n%s", want, markdown.String())
		}
	}
	var document bytes.Buffer
	if err := RenderFirmwareFeatureMatrix(&document, matrix, FirmwareFeatureRenderOptions{Format: "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(document.String(), `"format": "pccontroller-firmware-feature-matrix/v1"`) {
		t.Fatalf("JSON output lacks format: %s", document.String())
	}
}

func TestFirmwareCompileSelectionResolvesNamedProfilesAndFeatureOverrides(t *testing.T) {
	defines, err := ResolveFirmwareCompileSelection("motion-macro", []string{"local-audio-cues=off", "force-silent=on"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(defines, " ")
	for _, want := range []string{
		"PCCONTROLLER_ENABLE_INA219=0", "PCCONTROLLER_ENABLE_DS18B20=0",
		"PCCONTROLLER_ENABLE_PCA9685=0", "PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES=0",
		"PCCONTROLLER_FORCE_SILENT=1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("defines %q lack %q", joined, want)
		}
	}
	if _, err := ResolveFirmwareCompileSelection("source", []string{"not-real=on"}); err == nil {
		t.Fatal("unknown feature was accepted")
	}
}
