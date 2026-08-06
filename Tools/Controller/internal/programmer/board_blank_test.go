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
}

func (runner *blankBoardFixtureRunner) Run(_ context.Context, command Command, output io.Writer) error {
	joined := strings.Join(command.Args, " ")
	if strings.Contains(joined, "-Ulfuse:r:-:h") {
		_, err := io.WriteString(output, "0xf7\n0xd7\n0xfd\n0xff\n")
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
	if containsArgument(command.Args, "-e") {
		runner.chipEraseCalls++
		runner.erased = true
		return nil
	}
	if strings.Contains(joined, "-cusbasp") && !strings.Contains(joined, "-U") {
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
		report.EEPROMBytes != atmega328PEEPROMCapacity || !report.FusesPreserved ||
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

func TestChipEraseRejectsUrclock(t *testing.T) {
	_, err := Build(Options{
		Method: MethodUrclock, Operation: OperationChipErase, Port: "COM18",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	})
	if err == nil || !strings.Contains(err.Error(), "requires ISP") {
		t.Fatalf("Urclock chip erase was accepted: %v", err)
	}
}
