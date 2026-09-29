package programmer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildUrclock(t *testing.T) {
	command, err := Build(Options{
		Method: MethodUrclock, Port: "COM18", HexPath: `C:\build\app.hex`,
		Avrdude:     `C:\avr\bin\avrdude.exe`,
		AvrdudeConf: `C:\avr\etc\avrdude.conf`,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	for _, expected := range []string{
		"-patmega328p", "-curclock", "-PCOM18", "-b115200",
		"-xbootsize=384", "-xeepromrw",
		"-xnometadata", `-Uflash:w:C:\build\app.hex:i`,
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %q", expected, joined)
		}
	}
}

func TestBuildUSBaspDoesNotInventEEPROMFile(t *testing.T) {
	hex := filepath.Join("build", "PCController.ino.with_bootloader.hex")
	command, err := Build(Options{
		Method: MethodUSBasp, HexPath: hex,
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	if strings.Contains(joined, ".eep") {
		t.Fatalf("unexpected EEPROM write: %s", joined)
	}
	if !strings.Contains(joined, "-cusbasp") ||
		!strings.Contains(joined, "-Uflash:w:"+hex+":i") {
		t.Fatalf("unexpected command: %s", joined)
	}
}

func TestBuildUSBaspForcedBitClock(t *testing.T) {
	command, err := Build(Options{
		Method: MethodUSBasp, Operation: OperationProbe,
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf", USBaspBitClockUS: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(command.Args, " "); !strings.Contains(joined, "-cusbasp -B32") {
		t.Fatalf("USBasp command did not force slow SCK: %s", joined)
	}
}

func TestUSBaspAutoSlowRetriesAndRetainsSlowMode(t *testing.T) {
	var commands []Command
	runner := CommandRunnerFunc(func(_ context.Context, command Command, _ io.Writer) error {
		commands = append(commands, command)
		if len(commands) == 1 {
			return errors.New("initial SCK failed")
		}
		return nil
	})
	var output bytes.Buffer
	wrapped := &usbaspSlowFallbackRunner{inner: runner, output: &output}
	probe := Command{Name: "avrdude", Args: []string{"-patmega328p", "-cusbasp"}}
	if err := wrapped.Run(context.Background(), probe, &output); err != nil {
		t.Fatal(err)
	}
	if err := wrapped.Run(context.Background(), probe, &output); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 {
		t.Fatalf("commands=%d want 3", len(commands))
	}
	for _, index := range []int{1, 2} {
		if !strings.Contains(strings.Join(commands[index].Args, " "), "-B32") {
			t.Fatalf("slow command %d missing -B32: %#v", index, commands[index])
		}
	}
	if !strings.Contains(output.String(), "retaining slow mode") {
		t.Fatalf("fallback not reported: %s", output.String())
	}
}

func TestArduinoUploadUsesSketchNotInputFile(t *testing.T) {
	command, err := Build(Options{
		Method: MethodArduino, Port: "COM18", SketchPath: ".",
		ArduinoCLI: "arduino-cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	if strings.Contains(joined, "--input-file") {
		t.Fatalf("unsafe input-file workflow: %s", joined)
	}
}

func TestArduinoCompileUsesFirmwareRelaxFlags(t *testing.T) {
	root := firmwareCompileFixture(t)
	t.Setenv("PCCONTROLLER_BUILD_TIMESTAMP", "0x35019D5D")
	t.Setenv("PCCONTROLLER_ARDUINO_CACHE", filepath.Join(t.TempDir(), "compile-cache"))
	command, err := Build(Options{
		Method: MethodCompile, SketchPath: root,
		ArduinoCLI: "arduino-cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	for _, expected := range []string{
		"build.extra_flags=-DPCCONTROLLER_BUILD_HASH=0x",
		"-DPCCONTROLLER_BUILD_TIMESTAMP=0x35019D5DUL -DPCCONTROLLER_IDENTITY_ADDRESS=0x7E74UL -mcall-prologues",
		"-fno-tree-scev-cprop -fipa-pta -fstack-usage",
		"compiler.c.elf.extra_flags=-w -flto -fipa-pta -g -Wl,--relax",
		"-Wl,--section-start=.firmware_identity=0x7E74",
		"--warnings all", "--jobs 1", "--build-path", "--output-dir",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing aligned firmware compile argument %q: %s", expected, joined)
		}
	}
	for _, argument := range command.Args {
		if strings.HasPrefix(argument, "compiler.c.elf.extra_flags=") &&
			strings.Contains(argument, "-fstack-usage") {
			t.Fatalf("linker-side -fstack-usage triggers AVR-GCC 7.3 LTO ICE: %s", argument)
		}
	}
}

func TestCompilePlanMatchesManifestAndStagesOnlyCuratedRoots(t *testing.T) {
	root := firmwareCompileFixture(t)
	cache := filepath.Join(t.TempDir(), "cache")
	t.Setenv("PCCONTROLLER_ARDUINO_CACHE", cache)
	t.Setenv("PCCONTROLLER_BUILD_TIMESTAMP", "35019D5D")
	options, identity, err := PlanCompile(Options{
		Method: MethodCompile, SketchPath: root, ArduinoCLI: "arduino-cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(
		"LocalLib/value.cpp:%X\nMenuLogic.cpp:%X\nPCController.ino:%X\nProject/Runtime/Domain.inc.h:%X\nProjectConfig.h:%X\n",
		sha256.Sum256([]byte("int value = 1;\n")),
		sha256.Sum256([]byte("void menuLogic() {}\n")),
		sha256.Sum256([]byte("void setup() {}\nvoid loop() {}\n")),
		sha256.Sum256([]byte("void firmwareDomain() {}\n")),
		sha256.Sum256([]byte("#pragma once\n")),
	)
	digest := sha256.Sum256([]byte(manifest))
	wantHash := binary.BigEndian.Uint32(digest[:4])
	if identity.SourceHash != wantHash || identity.PackedTimestamp != 0x35019D5D {
		t.Fatalf("identity=%#v want hash=%08X stamp=35019D5D", identity, wantHash)
	}
	if !strings.HasPrefix(identity.SketchPath, cache) ||
		strings.HasPrefix(identity.SketchPath, root) ||
		!strings.HasSuffix(identity.OutputDir, filepath.Join(".build", "firmware")) {
		t.Fatalf("compile paths are not isolated/defaulted correctly: %#v", identity)
	}
	if _, err := os.Stat(identity.SketchPath); !os.IsNotExist(err) {
		t.Fatalf("read-only planning created staging path: %v", err)
	}

	options, staged, err := StageCompile(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"PCController.ino", "ProjectConfig.h", "MenuLogic.cpp",
		filepath.Join("LocalLib", "value.cpp"),
		filepath.Join("Project", "Runtime", "Domain.inc.h"),
	} {
		if _, err := os.Stat(filepath.Join(staged.SketchPath, relative)); err != nil {
			t.Fatalf("missing staged source %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(staged.SketchPath, ".cache")); !os.IsNotExist(err) {
		t.Fatalf("repository .cache leaked into staged sketch: %v", err)
	}
	command, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	for _, expected := range []string{
		fmt.Sprintf("PCCONTROLLER_BUILD_HASH=0x%08XUL", wantHash),
		"PCCONTROLLER_BUILD_TIMESTAMP=0x35019D5DUL",
		"--jobs 1", "--build-path " + staged.BuildPath,
		"--output-dir " + staged.OutputDir, staged.SketchPath,
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("compile command missing %q: %s", expected, joined)
		}
	}
}

func firmwareCompileFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"PCController.ino":                     "void setup() {}\nvoid loop() {}\n",
		"ProjectConfig.h":                      "#pragma once\n",
		"MenuLogic.cpp":                        "void menuLogic() {}\n",
		filepath.Join("LocalLib", "value.cpp"): "int value = 1;\n",
		filepath.Join("Project", "Runtime", "Domain.inc.h"): "void firmwareDomain() {}\n",
		filepath.Join(".cache", "poison.cpp"):                "#error must not be staged\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestArduinoCoreInfoAndBurnBootloader(t *testing.T) {
	info, err := Build(Options{
		Method: MethodArduino, Operation: OperationCoreInfo,
		FQBN: "MiniCore:avr:328", ArduinoCLI: "arduino-cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(info.Args, " "); !strings.Contains(
		joined,
		"board details --fqbn MiniCore:avr:328 --full --list-programmers",
	) {
		t.Fatalf("unexpected board details command: %s", joined)
	}

	properties, err := Build(Options{
		Method: MethodArduino, Operation: OperationCoreProperties,
		FQBN: "MiniCore:avr:328", SketchPath: "fixture", ArduinoCLI: "arduino-cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(properties.Args, " "); !strings.Contains(
		joined,
		"compile --fqbn MiniCore:avr:328 --show-properties=expanded fixture",
	) {
		t.Fatalf("unexpected core-properties command: %s", joined)
	}

	burn, err := Build(Options{
		Method: MethodArduino, Operation: OperationBurnBoot,
		FQBN: "MiniCore:avr:328", Programmer: "usbasp",
		ArduinoCLI: "arduino-cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(burn.Args, " "); !strings.Contains(
		joined,
		"burn-bootloader --fqbn MiniCore:avr:328 --programmer usbasp",
	) {
		t.Fatalf("unexpected burn command: %s", joined)
	}
}

func TestBuildReadVerifyMetadataAndEEPROMConfirmation(t *testing.T) {
	base := Options{
		Method: MethodUrclock, Port: "COM18",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	}
	read := base
	read.Operation = OperationReadFlash
	read.OutputPath = "backup.hex"
	command, err := Build(read)
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(command.Args, " "); !strings.Contains(joined, "-Uflash:r:backup.hex:i") ||
		!strings.Contains(joined, "-xbootsize=384") || !strings.Contains(joined, "-xeepromrw") {
		t.Fatalf("unexpected read command: %s", joined)
	}

	metadata := base
	metadata.Operation = OperationMetadata
	command, err = Build(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(command.Args, " "); !strings.Contains(joined, "-xshowall") {
		t.Fatalf("unexpected metadata command: %s", joined)
	}

	eeprom := base
	eeprom.Operation = OperationWriteEEPROM
	eeprom.HexPath = "settings.hex"
	if _, err := Build(eeprom); err == nil ||
		!strings.Contains(err.Error(), "confirm-eeprom-write") {
		t.Fatalf("expected EEPROM confirmation error, got %v", err)
	}
	eeprom.ConfirmEEPROMWrite = true
	command, err = Build(eeprom)
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(command.Args, " "); !strings.Contains(joined, "-Ueeprom:w:settings.hex:i") {
		t.Fatalf("unexpected EEPROM command: %s", joined)
	}
}

func TestLooseVersionOrdering(t *testing.T) {
	if compareLooseVersion("10.0", "8.1") <= 0 ||
		compareLooseVersion("8.0-arduino.2", "8.0-arduino.1") <= 0 {
		t.Fatal("loose version comparison did not select newest tool")
	}
}

func TestBackupArtifactsAreUniqueHashedAndAtomic(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 7, 31, 7, 45, 12, 0, time.UTC)
	first, err := createBackupDirectory(root, at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := createBackupDirectory(root, at)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasSuffix(second, "-2") {
		t.Fatalf("backup directories are not unique: %q %q", first, second)
	}

	flashPath := filepath.Join(first, "flash.hex")
	if err := os.WriteFile(flashPath, []byte("firmware"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := backupFile(flashPath, "flash")
	if err != nil {
		t.Fatal(err)
	}
	if file.Bytes != 8 ||
		file.SHA256 != "c3bf47ea1f4a4a605470313cacb3a44f4a461f68c6faeab07e737610cb5ac835" {
		t.Fatalf("unexpected backup file metadata: %#v", file)
	}

	manifest := BackupManifest{
		Schema: 1, Status: "complete", CreatedAt: at,
		CompletedAt: at.Add(time.Second),
		Method:      MethodUrclock, Port: "COM18", MCU: "atmega328p",
		Programmer: "urclock", ApplicationHash: "1234ABCD",
		Files: []BackupFile{file},
	}
	manifestPath := filepath.Join(first, "manifest.json")
	if err := writeBackupManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded BackupManifest
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != "complete" ||
		decoded.ApplicationHash != "1234ABCD" ||
		len(decoded.Files) != 1 {
		t.Fatalf("unexpected decoded manifest: %#v", decoded)
	}
}

func TestBackupValidationHappensBeforeCreatingArtifacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "must-not-exist")
	err := ValidateBackup(Options{
		Method: MethodCompile, Operation: OperationBackup, OutputPath: root,
	})
	if err == nil || !strings.Contains(err.Error(), "requires urclock") {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
		t.Fatalf("invalid backup created artifacts: %v", statErr)
	}
}
