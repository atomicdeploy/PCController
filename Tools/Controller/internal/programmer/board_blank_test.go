package programmer

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

type blankBoardFixtureRunner struct {
	erased          bool
	eepromBlank     bool
	corruptFlash    bool
	chipEraseCalls  int
	eepromWriteCall int
	factoryFuses    bool
	slowUntilCore   bool
	coreFusesSet    bool
	calls           []string
}

func (runner *blankBoardFixtureRunner) Run(_ context.Context, command Command, output io.Writer) error {
	joined := strings.Join(command.Args, " ")
	runner.calls = append(runner.calls, joined)
	if strings.Contains(joined, "--show-properties=expanded") {
		_, err := io.WriteString(output,
			"bootloader.low_fuses=0xf7\nbootloader.high_fuses=0xd7\nbootloader.extended_fuses=0xfd\n",
		)
		return err
	}
	if strings.Contains(joined, "-Ulfuse:r:-:h") {
		values := "0xf7\n0xd7\n0xfd\n0xff\n"
		if runner.factoryFuses {
			values = "0x62\n0xd9\n0xff\n0xff\n"
		}
		_, err := io.WriteString(output, values)
		return err
	}
	if path := commandOutputPath(command, "-Uflash:r:"); path != "" {
		data := map[uint32]byte{0: 0x12, 1024: 0x34}
		if runner.erased {
			data = make(map[uint32]byte, ATmega328PFlashSize)
			for address := uint32(0); address < ATmega328PFlashSize; address++ {
				data[address] = 0xFF
			}
			if runner.corruptFlash {
				data[77] = 0x00
			}
		}
		content, err := (&IntelHexImage{data: data}).Canonical()
		if err != nil {
			return err
		}
		return os.WriteFile(path, content, 0o600)
	}
	if path := commandOutputPath(command, "-Ueeprom:r:"); path != "" {
		data := map[uint32]byte{0: 0x44, atmega328PEEPROMCapacity - 1: 0x55}
		if runner.eepromBlank {
			data = make(map[uint32]byte, atmega328PEEPROMCapacity)
			for address := uint32(0); address < atmega328PEEPROMCapacity; address++ {
				data[address] = 0xFF
			}
		}
		content, err := (&IntelHexImage{data: data}).Canonical()
		if err != nil {
			return err
		}
		return os.WriteFile(path, content, 0o600)
	}
	if strings.Contains(joined, "-Ueeprom:w:") {
		runner.eepromWriteCall++
		runner.eepromBlank = true
		return nil
	}
	if strings.Contains(joined, "-Ulfuse:w:0x62:m") &&
		strings.Contains(joined, "-Uhfuse:w:0xD9:m") &&
		strings.Contains(joined, "-Uefuse:w:0xFF:m") &&
		strings.Contains(joined, "-Ulock:w:0xFF:m") {
		runner.factoryFuses = true
		return nil
	}
	if strings.Contains(joined, "-Ulfuse:w:0xF7:m") &&
		strings.Contains(joined, "-Uhfuse:w:0xD7:m") &&
		strings.Contains(joined, "-Uefuse:w:0xFD:m") {
		if !strings.Contains(joined, "-B32") {
			return errors.New("core fuse recovery was not slow-safe")
		}
		runner.coreFusesSet = true
		return nil
	}
	if containsArgument(command.Args, "-e") {
		runner.chipEraseCalls++
		runner.erased = true
		return nil
	}
	if strings.Contains(joined, "-cusbasp") && !strings.Contains(joined, "-U") {
		if runner.slowUntilCore && !runner.coreFusesSet && !strings.Contains(joined, "-B32") {
			return errors.New("factory-clock target needs slow USBasp")
		}
		_, err := io.WriteString(output, "Device signature = 1E 95 0F\n")
		return err
	}
	return errors.New("unexpected command: " + joined)
}

func TestBlankBoardBacksUpErasesAndVerifiesEveryByte(t *testing.T) {
	runner := &blankBoardFixtureRunner{}
	report, err := BlankBoardWithRunner(context.Background(), BoardBlankOptions{
		MCU: "atmega328p", Programmer: "usbasp", BackupRoot: t.TempDir(),
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	}, io.Discard, runner)
	if err != nil {
		t.Fatal(err)
	}
	if report.BackupDirectory == "" || report.FlashBytes != ATmega328PFlashSize ||
		report.EEPROMBytes != atmega328PEEPROMCapacity || !report.FactoryFusesApplied ||
		runner.chipEraseCalls != 1 || runner.eepromWriteCall != 1 {
		t.Fatalf("blank report=%+v runner=%+v", report, runner)
	}
	if _, err := ValidateBackupManifest(report.BackupDirectory + string(os.PathSeparator) + "manifest.json"); err != nil {
		t.Fatalf("pre-blank backup is incomplete: %v", err)
	}
}

func TestBlankBoardRejectsSingleNonBlankFlashByte(t *testing.T) {
	runner := &blankBoardFixtureRunner{corruptFlash: true}
	_, err := BlankBoardWithRunner(context.Background(), BoardBlankOptions{
		MCU: "atmega328p", Programmer: "usbasp", BackupRoot: t.TempDir(),
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	}, io.Discard, runner)
	if err == nil || !strings.Contains(err.Error(), "address 0x4D contains 0x00") {
		t.Fatalf("nonblank readback was accepted: %v", err)
	}
}

func TestBlankBoardRecoversFastUSBaspBeforeEraseThenRestoresFactoryClock(t *testing.T) {
	runner := &blankBoardFixtureRunner{slowUntilCore: true}
	report, err := BlankBoardWithRunner(context.Background(), BoardBlankOptions{
		FQBN: "MiniCore:avr:328:clock=16MHz_external", SketchPath: "fixture",
		MCU: "atmega328p", Programmer: "usbasp", BackupRoot: t.TempDir(),
		ArduinoCLI: "arduino-cli", ArduinoConfig: "arduino-cli.yaml",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf", USBaspAutoSlow: true,
	}, io.Discard, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !report.SlowUSBaspUsed || !report.FastISPRecovered || !report.FactoryFusesApplied || !runner.coreFusesSet || !runner.factoryFuses {
		t.Fatalf("unexpected report=%+v runner=%+v", report, runner)
	}
	coreIndex, backupIndex, eraseIndex := -1, -1, -1
	for index, call := range runner.calls {
		if strings.Contains(call, "-Ulfuse:w:0xF7:m") {
			coreIndex = index
		}
		if backupIndex < 0 && strings.Contains(call, "-Uflash:r:") && !strings.Contains(call, "-B32") {
			backupIndex = index
		}
		if containsArgument(strings.Fields(call), "-e") {
			eraseIndex = index
		}
	}
	if coreIndex < 0 || backupIndex <= coreIndex || eraseIndex <= backupIndex {
		t.Fatalf("core recovery must precede fast backup and erase: %v", runner.calls)
	}
}

func TestChipEraseRejectsUrclock(t *testing.T) {
	_, err := Build(Options{
		Method: MethodUrclock, Operation: OperationChipErase, Port: "COM18",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	})
	if err == nil || !strings.Contains(err.Error(), "requires ISP") {
		t.Fatalf("Urclock chip erase was accepted: %v", err)
	}
}
