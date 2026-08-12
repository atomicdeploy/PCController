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
	"time"
)

type BoardBlankOptions struct {
	// FQBN and SketchPath identify the selected crystal-capable core policy.
	// When a factory-clock part first needs slow ISP, the lifecycle writes this
	// policy just long enough to recover normal-speed USBasp before erasing.
	FQBN             string
	SketchPath       string
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
	MCU                 string                `json:"mcu"`
	Programmer          string                `json:"programmer"`
	BackupDirectory     string                `json:"backup_directory"`
	FlashBytes          uint32                `json:"flash_bytes_verified_blank"`
	EEPROMBytes         uint32                `json:"eeprom_bytes_verified_blank"`
	SlowUSBaspUsed      bool                  `json:"slow_usbasp_used"`
	FactoryFusesApplied bool                  `json:"factory_fuses_applied"`
	FastISPRecovered    bool                  `json:"fast_isp_recovered"`
	Phases              []BoardLifecyclePhase `json:"phases"`
}

// BoardLifecyclePhase is an operator-visible duration for one destructive
// lifecycle stage. The controller emits it through CLI, generic API command,
// and TUI output so timing evidence never relies on shell timestamps.
type BoardLifecyclePhase struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
}

const (
	factoryLowFuse      byte = 0x62
	factoryHighFuse     byte = 0xD9
	factoryExtendedFuse byte = 0xFF
	factoryLockBits     byte = 0xFF
	// 187.5 kHz is safely below F_CPU/4 for the ATmega328P factory 1 MHz
	// oscillator, while avoiding a needlessly slow complete-memory readback.
	factoryVerifyBitClockUS = 4.0
)

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
	phase := func(name string, run func() error) error {
		started := time.Now()
		err := run()
		report.Phases = append(report.Phases, BoardLifecyclePhase{
			Name: name, DurationMS: time.Since(started).Milliseconds(),
		})
		return err
	}

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

	fmt.Fprintln(output, "[1/9] Probing ISP target before destructive blanking")
	isp.Operation = OperationProbe
	probe, err := Build(isp)
	if err != nil {
		return report, err
	}
	if err := phase("probe", func() error { return ispRunner.Run(ctx, probe, output) }); err != nil {
		return report, fmt.Errorf("probe target before blanking: %w", err)
	}

	fmt.Fprintln(output, "[2/9] Capturing mandatory flash, EEPROM, fuse, and lock-bit backup")
	backup := isp
	backup.Operation = OperationBackup
	backup.OutputPath = options.BackupRoot
	err = phase("backup", func() error {
		report.BackupDirectory, err = BackupWithRunner(ctx, backup, output, ispRunner)
		return err
	})
	if err != nil {
		return report, fmt.Errorf("capture mandatory pre-blank backup: %w", err)
	}
	// A slow first probe means the part is probably still on its factory 1 MHz
	// oscillator. On the known crystal-capable profile, install the selected
	// core fuse policy now, prove normal-speed ISP, and perform the long erase
	// and readback at full speed. The final stage restores factory fuses.
	if fallback != nil && fallback.slow && shouldRecoverFastUSBasp(options) {
		fmt.Fprintln(output, "[3/9] Recovering selected crystal fuse policy at slow SCK, then proving fast USBasp")
		if err := phase("recover_fast_isp", func() error {
			policy, policyErr := resolveBoardCoreFusePolicy(ctx, BoardCoreInitializeOptions{
				FQBN: options.FQBN, SketchPath: options.SketchPath,
				ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig,
			}, runner)
			if policyErr != nil {
				return policyErr
			}
			fuseCommand, buildErr := buildSlowFuseCorrectionCommand(isp, policy)
			if buildErr != nil {
				return buildErr
			}
			if writeErr := runner.Run(ctx, fuseCommand, output); writeErr != nil {
				return writeErr
			}
			fast := isp
			fast.Operation, fast.USBaspBitClockUS = OperationProbe, 0
			fastCommand, buildErr := Build(fast)
			if buildErr != nil {
				return buildErr
			}
			if fastErr := runner.Run(ctx, fastCommand, output); fastErr != nil {
				return fastErr
			}
			isp, ispRunner = fast, runner
			report.FastISPRecovered = true
			return nil
		}); err != nil {
			return report, fmt.Errorf("recover fast USBasp before blanking: %w", err)
		}
	}

	fmt.Fprintln(output, "[4/9] Erasing application flash and bootloader")
	erase := isp
	erase.Operation = OperationChipErase
	eraseCommand, err := Build(erase)
	if err != nil {
		return report, err
	}
	if err := phase("chip_erase", func() error { return ispRunner.Run(ctx, eraseCommand, output) }); err != nil {
		return report, fmt.Errorf("chip erase: %w", err)
	}

	fmt.Fprintln(output, "[5/9] Overwriting the EESAVE-preserved EEPROM with 0xFF")
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
	if err := phase("erase_eeprom", func() error { return ExecuteWithRunner(ctx, eeprom, output, ispRunner) }); err != nil {
		return report, fmt.Errorf("blank and verify EEPROM: %w", err)
	}
	report.EEPROMBytes = atmega328PEEPROMCapacity

	fmt.Fprintln(output, "[6/9] Reading all flash bytes back and requiring 0xFF")
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
	if err := phase("verify_blank_flash", func() error { return ispRunner.Run(ctx, readFlashCommand, output) }); err != nil {
		return report, fmt.Errorf("read blank flash: %w", err)
	}
	if err := requireBlankIntelHex(flashReadback, ATmega328PFlashSize); err != nil {
		return report, fmt.Errorf("verify blank flash: %w", err)
	}
	report.FlashBytes = ATmega328PFlashSize

	fmt.Fprintln(output, "[7/9] Restoring ATmega328P factory fuse and lock-bit defaults")
	if err := phase("restore_factory_fuses", func() error {
		factoryCommand, buildErr := buildFactoryFuseCommand(isp)
		if buildErr != nil {
			return buildErr
		}
		return ispRunner.Run(ctx, factoryCommand, output)
	}); err != nil {
		return report, fmt.Errorf("restore factory fuse defaults: %w", err)
	}
	report.FactoryFusesApplied = true

	fmt.Fprintln(output, "[8/9] Final slow-safe full flash and EEPROM readback after factory fuse restore")
	factoryISP := isp
	factoryISP.USBaspBitClockUS = factoryVerifyBitClockUS
	flashFinal, err := reserveBlankReadback("factory-flash")
	if err != nil {
		return report, err
	}
	defer os.Remove(flashFinal)
	eepromFinal, err := reserveBlankReadback("factory-eeprom")
	if err != nil {
		return report, err
	}
	defer os.Remove(eepromFinal)
	if err := phase("final_factory_readback", func() error {
		flash := factoryISP
		flash.Operation, flash.OutputPath = OperationReadFlash, flashFinal
		flashCommand, buildErr := Build(flash)
		if buildErr != nil {
			return buildErr
		}
		if readErr := runner.Run(ctx, flashCommand, output); readErr != nil {
			return readErr
		}
		eepromRead := factoryISP
		eepromRead.Operation, eepromRead.OutputPath = OperationReadEEPROM, eepromFinal
		eepromCommand, buildErr := Build(eepromRead)
		if buildErr != nil {
			return buildErr
		}
		return runner.Run(ctx, eepromCommand, output)
	}); err != nil {
		return report, fmt.Errorf("read final factory-blank memory: %w", err)
	}
	if err := requireBlankIntelHex(flashFinal, ATmega328PFlashSize); err != nil {
		return report, fmt.Errorf("verify final factory flash: %w", err)
	}
	if err := requireBlankIntelHex(eepromFinal, atmega328PEEPROMCapacity); err != nil {
		return report, fmt.Errorf("verify final factory EEPROM: %w", err)
	}

	fmt.Fprintln(output, "[9/9] Re-reading factory fuse, lock, and signature evidence")
	if err := phase("verify_factory_fuses", func() error {
		verify := factoryISP
		verify.Operation = OperationProbe
		verifyCommand, buildErr := Build(verify)
		if buildErr != nil {
			return buildErr
		}
		verifyCommand.Args = append(verifyCommand.Args,
			"-Ulfuse:r:-:h", "-Uhfuse:r:-:h", "-Uefuse:r:-:h", "-Ulock:r:-:h",
		)
		var metadata bytes.Buffer
		if verifyErr := runner.Run(ctx, verifyCommand, io.MultiWriter(output, &metadata)); verifyErr != nil {
			return verifyErr
		}
		fuses, parseErr := parseFuseEvidence(metadata.Bytes())
		if parseErr != nil {
			return parseErr
		}
		if fuses != [3]byte{factoryLowFuse, factoryHighFuse, factoryExtendedFuse} {
			return fmt.Errorf("factory fuse readback=%02X/%02X/%02X; wanted=%02X/%02X/%02X", fuses[0], fuses[1], fuses[2], factoryLowFuse, factoryHighFuse, factoryExtendedFuse)
		}
		return nil
	}); err != nil {
		return report, fmt.Errorf("verify factory fuse defaults: %w", err)
	}
	if fallback != nil && fallback.slow {
		report.SlowUSBaspUsed = true
	}
	fmt.Fprintln(output, "FACTORY BLANK: all flash and EEPROM bytes read back as 0xFF; factory fuse/lock defaults are restored.")
	return report, nil
}

func shouldRecoverFastUSBasp(options BoardBlankOptions) bool {
	return strings.TrimSpace(options.FQBN) != "" && strings.TrimSpace(options.SketchPath) != ""
}

func buildFactoryFuseCommand(isp Options) (Command, error) {
	isp.Operation = OperationProbe
	command, err := Build(isp)
	if err != nil {
		return Command{}, err
	}
	command.Args = append(command.Args,
		"-Ulfuse:w:0x62:m", "-Uhfuse:w:0xD9:m", "-Uefuse:w:0xFF:m", "-Ulock:w:0xFF:m",
	)
	return command, nil
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
