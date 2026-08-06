package programmer

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

type boardInitializeFixtureRunner struct {
	calls          []string
	fusesCorrected bool
	fastAfterFuses bool
	bootProgrammer string
}

func (runner *boardInitializeFixtureRunner) Run(
	_ context.Context,
	command Command,
	output io.Writer,
) error {
	joined := strings.Join(command.Args, " ")
	runner.calls = append(runner.calls, joined)
	if strings.Contains(joined, "--show-properties=expanded") {
		_, err := io.WriteString(output,
			"bootloader.low_fuses=0b11110111\n"+
				"bootloader.high_fuses=0xd7\n"+
				"bootloader.extended_fuses=0b11111101\n",
		)
		return err
	}
	if path := commandOutputPath(command, "-Uflash:r:"); path != "" {
		return writeBoardInitializeFixtureHex(path, 0x12)
	}
	if path := commandOutputPath(command, "-Ueeprom:r:"); path != "" {
		return writeBoardInitializeFixtureHex(path, 0x34)
	}
	if strings.Contains(joined, "-Ulfuse:w:0xF7:m") {
		if !strings.Contains(joined, "-B32") ||
			!strings.Contains(joined, "-Uhfuse:w:0xD7:m") ||
			!strings.Contains(joined, "-Uefuse:w:0xFD:m") {
			return errors.New("fuse recovery did not use the complete slow core policy")
		}
		runner.fusesCorrected = true
		return nil
	}
	if strings.Contains(joined, "burn-bootloader") {
		if strings.Contains(joined, "--programmer usbasp_slow") {
			runner.bootProgrammer = "usbasp_slow"
		} else if strings.Contains(joined, "--programmer usbasp") {
			runner.bootProgrammer = "usbasp"
		}
		return nil
	}
	if strings.Contains(joined, "-Ulfuse:r:-:h") {
		_, err := io.WriteString(output, "0xf7\n0xd7\n0xfd\n0xff\n")
		return err
	}
	if strings.Contains(joined, "-cusbasp") && !strings.Contains(joined, "-U") {
		if strings.Contains(joined, "-B32") || (runner.fusesCorrected && runner.fastAfterFuses) {
			_, err := io.WriteString(output, "Device signature = 1E 95 0F\n")
			return err
		}
		return errors.New("target clock is too slow for normal USBasp SCK")
	}
	return errors.New("unexpected command: " + joined)
}

func writeBoardInitializeFixtureHex(path string, value byte) error {
	content, err := (&IntelHexImage{data: map[uint32]byte{0: value}}).Canonical()
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func TestBoardInitializeRepairsFusesSlowThenRetriesBootloaderFast(t *testing.T) {
	runner := &boardInitializeFixtureRunner{fastAfterFuses: true}
	report, err := InitializeBoardCoreWithRunner(context.Background(), BoardCoreInitializeOptions{
		FQBN: "MiniCore:avr:328:clock=16MHz_external", Programmer: "usbasp",
		ArduinoCLI: "arduino-cli", ArduinoConfig: "arduino-cli.yaml", SketchPath: "fixture",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf", BackupRoot: t.TempDir(),
		USBaspAutoSlow: true,
	}, io.Discard, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !report.SlowUSBaspUsed || !report.FuseRecoveryApplied ||
		!report.NormalSpeedRecovered || report.BootloaderProgrammer != "usbasp" ||
		!report.BootloaderInstalled || runner.bootProgrammer != "usbasp" {
		t.Fatalf("unexpected report=%+v boot programmer=%q", report, runner.bootProgrammer)
	}
	fuseIndex, fastProbeIndex, bootIndex := -1, -1, -1
	for index, call := range runner.calls {
		switch {
		case strings.Contains(call, "-Ulfuse:w:0xF7:m"):
			fuseIndex = index
		case fuseIndex >= 0 && !strings.Contains(call, "-B32") &&
			strings.Contains(call, "-cusbasp") && !strings.Contains(call, "-U"):
			fastProbeIndex = index
		case strings.Contains(call, "burn-bootloader"):
			bootIndex = index
		}
	}
	if fuseIndex < 0 || fastProbeIndex <= fuseIndex || bootIndex <= fastProbeIndex {
		t.Fatalf("expected slow fuse repair, fast probe, then bootloader; calls=%v", runner.calls)
	}
}

func TestBoardInitializeKeepsSlowBootloaderWhenFastRetryStillFails(t *testing.T) {
	runner := &boardInitializeFixtureRunner{fastAfterFuses: false}
	report, err := InitializeBoardCoreWithRunner(context.Background(), BoardCoreInitializeOptions{
		FQBN: "MiniCore:avr:328:clock=16MHz_external", Programmer: "usbasp",
		ArduinoCLI: "arduino-cli", ArduinoConfig: "arduino-cli.yaml", SketchPath: "fixture",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf", BackupRoot: t.TempDir(),
		USBaspAutoSlow: true,
	}, io.Discard, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !report.FuseRecoveryApplied || report.NormalSpeedRecovered ||
		report.BootloaderProgrammer != "usbasp_slow" || runner.bootProgrammer != "usbasp_slow" {
		t.Fatalf("unexpected slow fallback report=%+v boot programmer=%q", report, runner.bootProgrammer)
	}
}

func TestResolveBoardCoreFusePolicyParsesCoreNumberFormats(t *testing.T) {
	runner := CommandRunnerFunc(func(_ context.Context, _ Command, output io.Writer) error {
		_, err := io.WriteString(output,
			"bootloader.low_fuses=0b11110111\n"+
				"bootloader.high_fuses=0xD7\n"+
				"bootloader.extended_fuses=253\n",
		)
		return err
	})
	policy, err := resolveBoardCoreFusePolicy(context.Background(), BoardCoreInitializeOptions{
		FQBN: "vendor:arch:board", SketchPath: "fixture", ArduinoCLI: "arduino-cli",
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Low != 0xF7 || policy.High != 0xD7 || policy.Extended != 0xFD {
		t.Fatalf("unexpected policy: %+v", policy)
	}
}
