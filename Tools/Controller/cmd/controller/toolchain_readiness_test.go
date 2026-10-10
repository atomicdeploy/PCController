package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/programmer"
)

func TestArduinoCLIEnvironmentBindsConfiguredDirectories(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "firmware-cli.yaml")
	content := "directories:\n" +
		"  data: C:/shared/arduino-data\n" +
		"  downloads: C:/shared/arduino-downloads\n" +
		"  user: C:/shared/arduino-user\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	environment, err := arduinoCLIEnvironment([]string{
		"PATH=example",
		"ARDUINO_DIRECTORIES_DATA=stale",
		"arduino_directories_user=stale-user",
	}, configPath)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[strings.ToUpper(name)] = value
		}
	}
	for name, want := range map[string]string{
		"ARDUINO_DIRECTORIES_DATA":      "C:/shared/arduino-data",
		"ARDUINO_DIRECTORIES_DOWNLOADS": "C:/shared/arduino-downloads",
		"ARDUINO_DIRECTORIES_USER":      "C:/shared/arduino-user",
	} {
		if got := values[name]; got != want {
			t.Fatalf("%s=%q want %q", name, got, want)
		}
	}
	if runtime.GOOS == "windows" {
		profile := filepath.Join(filepath.Dir(configPath), "cli-profile")
		if got := values["USERPROFILE"]; got != profile {
			t.Fatalf("USERPROFILE=%q want %q", got, profile)
		}
		if info, err := os.Stat(filepath.Join(profile, "Documents")); err != nil || !info.IsDir() {
			t.Fatalf("service Documents directory was not prepared: info=%v err=%v", info, err)
		}
	}
}

func TestSelectLatestCompatibleCLIReusesOneExistingProvider(t *testing.T) {
	providers := []detectedToolchainProvider{
		{name: "arduino-cli", path: "old", version: "1.0.4"},
		{name: "arduino-cli", path: "managed", version: "1.5.1", compatible: true, managed: true},
		{name: "arduino-cli", path: "configured", version: "1.5.1", compatible: true},
		{name: "platformio", path: "pio", version: "6.1.18", compatible: true},
	}
	selected := selectLatestCompatibleCLI(providers, "1.5.1")
	if selected == nil || selected.path != "configured" {
		t.Fatalf("selected=%#v", selected)
	}
}

func TestSelectLatestCompatibleCLIRejectsStaleOrPrereleaseMismatch(t *testing.T) {
	providers := []detectedToolchainProvider{
		{name: "arduino-cli", path: "old", version: "1.0.4"},
		{name: "arduino-cli", path: "future", version: "1.6.0-rc1", compatible: true},
	}
	if selected := selectLatestCompatibleCLI(providers, "1.5.1"); selected != nil {
		t.Fatalf("selected incompatible provider=%#v", selected)
	}
}

func TestPlatformIOAVRInstalledRequiresInstalledBoardMetadata(t *testing.T) {
	if !platformIOAVRInstalled([]byte(`[{"id":"uno","platform":"atmelavr"}]`)) {
		t.Fatal("installed atmelavr board was not recognized")
	}
	for _, content := range [][]byte{
		[]byte(`[{"id":"esp32dev","platform":"espressif32"}]`),
		[]byte(`[]`),
		[]byte(`not-json`),
	} {
		if platformIOAVRInstalled(content) {
			t.Fatalf("unexpected compatible PlatformIO inventory: %s", content)
		}
	}
}

func TestCoreInventoryHasRequiresExactInstalledVersion(t *testing.T) {
	inventory := []byte(`{"platforms":[{"id":"MiniCore:avr","installed_version":"3.1.3","latest_version":"3.1.4"}]}`)
	if !coreInventoryHas(inventory, "MiniCore:avr", "3.1.3") {
		t.Fatal("exact installed core was not recognized")
	}
	for _, test := range []struct {
		id      string
		version string
	}{
		{id: "MiniCore:avr", version: "3.1.4"},
		{id: "arduino:avr", version: "3.1.3"},
	} {
		if coreInventoryHas(inventory, test.id, test.version) {
			t.Fatalf("accepted non-installed core %s@%s", test.id, test.version)
		}
	}
	if coreInventoryHas([]byte(`not-json`), "MiniCore:avr", "3.1.3") {
		t.Fatal("invalid core inventory was accepted")
	}
}

func TestMissingToolchainLibrariesRequiresExactVersions(t *testing.T) {
	inventory := []byte(`{"installed_libraries":[` +
		`{"library":{"name":"OneWire","version":"2.3.8"}},` +
		`{"library":{"name":"DallasTemperature","version":"4.0.5"}}]}`)
	required := []programmer.ToolchainLibrary{
		{Name: "OneWire", Version: "2.3.8"},
		{Name: "DallasTemperature", Version: "4.0.6"},
		{Name: "rc-switch", Version: "2.6.4"},
	}
	want := []string{"DallasTemperature@4.0.6", "rc-switch@2.6.4"}
	if got := missingToolchainLibraries(inventory, required); !reflect.DeepEqual(got, want) {
		t.Fatalf("missing=%v want=%v", got, want)
	}
	if got := missingToolchainLibraries([]byte(`not-json`), required); !reflect.DeepEqual(got, []string{"invalid library inventory"}) {
		t.Fatalf("invalid inventory result=%v", got)
	}
}
