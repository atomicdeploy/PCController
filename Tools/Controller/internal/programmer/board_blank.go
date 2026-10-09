package programmer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type BoardBlankOptions struct {
	MCU              string
	Programmer       string
	ArduinoCLI       string
	ArduinoConfig    string
	Avrdude          string
	AvrdudeConf      string
	BackupRoot       string
	USBaspBitClockUS float64
	USBaspAutoSlow   bool
}

type BoardBlankReport struct {
	MCU             string `json:"mcu"`
	Programmer      string `json:"programmer"`
	BackupDirectory string `json:"backup_directory"`
	FlashBytes      uint32 `json:"flash_bytes_verified_blank"`
	EEPROMBytes     uint32 `json:"eeprom_bytes_verified_blank"`
	SlowUSBaspUsed  bool   `json:"slow_usbasp_used"`
	FusesPreserved  bool   `json:"fuses_preserved"`
}

func BlankBoard(ctx context.Context, options BoardBlankOptions, output io.Writer) (BoardBlankReport, error) {
	return BlankBoardWithRunner(ctx, options, output, CommandRunnerFunc(Run))
}

func BlankBoardWithRunner(
	ctx context.Context,
	options BoardBlankOptions,
	output io.Writer,
	runner CommandRunner,
) (BoardBlankReport, error) {
	if runner == nil {
		return BoardBlankReport{}, errors.New("board blanking requires a command runner")
	}
	if output == nil {
		output = io.Discard
	}
	if options.MCU == "" {
		options.MCU = generatedBoardMCU
	}
	if !strings.EqualFold(options.MCU, generatedBoardMCU) {
		return BoardBlankReport{}, fmt.Errorf("guarded blanking currently supports %s only", generatedBoardMCU)
	}
	if options.Programmer == "" {
		options.Programmer = "usbasp"
	}
	if !strings.EqualFold(options.Programmer, "usbasp") && !strings.EqualFold(options.Programmer, "usbasp_slow") {
		return BoardBlankReport{}, fmt.Errorf("guarded blanking requires usbasp or usbasp_slow, got %q", options.Programmer)
	}
	if strings.TrimSpace(options.BackupRoot) == "" {
		return BoardBlankReport{}, errors.New("guarded blanking requires a backup root")
	}
	report := BoardBlankReport{MCU: options.MCU, Programmer: options.Programmer}

	ispRunner := runner
	var fallback *usbaspSlowFallbackRunner
	forceSlow := strings.EqualFold(options.Programmer, "usbasp_slow") || options.USBaspBitClockUS > 0
	if options.USBaspAutoSlow && !forceSlow {
		fallback = &usbaspSlowFallbackRunner{inner: runner, output: output}
		ispRunner = fallback
	}
	isp := Options{
		Method: MethodUSBasp, MCU: options.MCU,
		ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig,
		Avrdude: options.Avrdude, AvrdudeConf: options.AvrdudeConf,
		USBaspBitClockUS: options.USBaspBitClockUS,
	}
	if strings.EqualFold(options.Programmer, "usbasp_slow") && isp.USBaspBitClockUS <= 0 {
		isp.USBaspBitClockUS = defaultUSBaspSlowBitClockUS
	}

	fmt.Fprintln(output, "[1/6] Probing ISP target before destructive blanking")
	isp.Operation = OperationProbe
	probe, err := Build(isp)
	if err != nil {
		return report, err
	}
	if err := ispRunner.Run(ctx, probe, output); err != nil {
		return report, fmt.Errorf("probe target before blanking: %w", err)
	}

	fmt.Fprintln(output, "[2/6] Capturing mandatory flash, EEPROM, fuse, and lock-bit backup")
	backup := isp
	backup.Operation = OperationBackup
	backup.OutputPath = options.BackupRoot
	report.BackupDirectory, err = BackupWithRunner(ctx, backup, output, ispRunner)
	if err != nil {
		return report, fmt.Errorf("capture mandatory pre-blank backup: %w", err)
	}
	preMetadata, err := os.ReadFile(filepath.Join(report.BackupDirectory, "programmer.txt"))
	if err != nil {
		return report, fmt.Errorf("read pre-blank fuse evidence: %w", err)
	}
	preFuses, err := parseFuseEvidence(preMetadata)
	if err != nil {
		return report, fmt.Errorf("parse pre-blank fuse evidence: %w", err)
	}

	fmt.Fprintln(output, "[3/6] Erasing application flash and bootloader while preserving fuse configuration")
	erase := isp
	erase.Operation = OperationChipErase
	eraseCommand, err := Build(erase)
	if err != nil {
		return report, err
	}
	if err := ispRunner.Run(ctx, eraseCommand, output); err != nil {
		return report, fmt.Errorf("chip erase: %w", err)
	}

	fmt.Fprintln(output, "[4/6] Overwriting the EESAVE-preserved EEPROM with 0xFF")
	eepromPath, err := writeBlankEEPROMTemporary()
	if err != nil {
		return report, err
	}
	defer os.Remove(eepromPath)
	eeprom := isp
	eeprom.Operation = OperationWriteEEPROM
	eeprom.HexPath = eepromPath
	eeprom.ConfirmEEPROMWrite = true
	// The outer runner retains a successful slow fallback across all six
	// stages; do not wrap it in a second independent retry state.
	eeprom.USBaspAutoSlow = false
	if err := ExecuteWithRunner(ctx, eeprom, output, ispRunner); err != nil {
		return report, fmt.Errorf("blank and verify EEPROM: %w", err)
	}
	report.EEPROMBytes = atmega328PEEPROMCapacity

	fmt.Fprintln(output, "[5/6] Reading all flash bytes back and requiring 0xFF")
	flashReadback, err := reserveBlankReadback("flash")
	if err != nil {
		return report, err
	}
	defer os.Remove(flashReadback)
	readFlash := isp
	readFlash.Operation = OperationReadFlash
	readFlash.OutputPath = flashReadback
	readFlashCommand, err := Build(readFlash)
	if err != nil {
		return report, err
	}
	if err := ispRunner.Run(ctx, readFlashCommand, output); err != nil {
		return report, fmt.Errorf("read blank flash: %w", err)
	}
	if err := requireBlankIntelHex(flashReadback, ATmega328PFlashSize); err != nil {
		return report, fmt.Errorf("verify blank flash: %w", err)
	}
	report.FlashBytes = ATmega328PFlashSize

	fmt.Fprintln(output, "[6/6] Re-reading signature, fuses, and lock bits")
	verify := isp
	verify.Operation = OperationProbe
	verifyCommand, err := Build(verify)
	if err != nil {
		return report, err
	}
	verifyCommand.Args = append(verifyCommand.Args,
		"-Ulfuse:r:-:h", "-Uhfuse:r:-:h", "-Uefuse:r:-:h", "-Ulock:r:-:h",
	)
	var postMetadata bytes.Buffer
	if err := ispRunner.Run(ctx, verifyCommand, io.MultiWriter(output, &postMetadata)); err != nil {
		return report, fmt.Errorf("verify target metadata after blanking: %w", err)
	}
	postFuses, err := parseFuseEvidence(postMetadata.Bytes())
	if err != nil {
		return report, fmt.Errorf("parse post-blank fuse evidence: %w", err)
	}
	if preFuses != postFuses {
		return report, fmt.Errorf("fuses changed during blanking: before=%02X/%02X/%02X after=%02X/%02X/%02X",
			preFuses[0], preFuses[1], preFuses[2], postFuses[0], postFuses[1], postFuses[2])
	}
	report.FusesPreserved = true
	if fallback != nil && fallback.slow {
		report.SlowUSBaspUsed = true
	}
	fmt.Fprintln(output, "BLANK: all flash and EEPROM bytes read back as 0xFF; fuse configuration remains installed.")
	return report, nil
}

func writeBlankEEPROMTemporary() (string, error) {
	image := &IntelHexImage{data: make(map[uint32]byte, atmega328PEEPROMCapacity)}
	for address := uint32(0); address < atmega328PEEPROMCapacity; address++ {
		image.data[address] = 0xFF
	}
	content, err := image.Canonical()
	if err != nil {
		return "", fmt.Errorf("encode blank EEPROM image: %w", err)
	}
	file, err := os.CreateTemp("", "pccontroller-blank-eeprom-*.hex")
	if err != nil {
		return "", fmt.Errorf("reserve blank EEPROM image: %w", err)
	}
	path := file.Name()
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return "", fmt.Errorf("write blank EEPROM image: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("flush blank EEPROM image: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close blank EEPROM image: %w", err)
	}
	keep = true
	return path, nil
}

func reserveBlankReadback(memory string) (string, error) {
	file, err := os.CreateTemp("", "pccontroller-blank-"+memory+"-*.hex")
	if err != nil {
		return "", fmt.Errorf("reserve blank %s readback: %w", memory, err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("prepare blank %s readback: %w", memory, err)
	}
	return filepath.Clean(path), nil
}

func requireBlankIntelHex(path string, capacity uint32) error {
	document, err := LoadIntelHex(path)
	if err != nil {
		return err
	}
	for address := uint32(0); address < capacity; address++ {
		value, present := document.Image.Byte(address)
		if !present {
			return fmt.Errorf("readback omits address 0x%X", address)
		}
		if value != 0xFF {
			return fmt.Errorf("address 0x%X contains 0x%02X", address, value)
		}
	}
	return nil
}

var fuseEvidencePattern = regexp.MustCompile(`(?mi)^\s*0x([0-9a-f]{2})\s*$`)

func parseFuseEvidence(content []byte) ([3]byte, error) {
	matches := fuseEvidencePattern.FindAllSubmatch(content, -1)
	if len(matches) < 3 {
		return [3]byte{}, fmt.Errorf("expected lfuse, hfuse, and efuse values; found %d", len(matches))
	}
	var result [3]byte
	for index := range result {
		value, err := strconv.ParseUint(string(matches[index][1]), 16, 8)
		if err != nil {
			return [3]byte{}, err
		}
		result[index] = byte(value)
	}
	return result, nil
}
