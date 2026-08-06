package programmer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// BoardCoreInitializeOptions owns the ISP portion of first-board setup. The
// application/UART phase deliberately remains separate so a board without a
// UART adapter still receives a complete core-provided bootloader and fuse set.
type BoardCoreInitializeOptions struct {
	FQBN             string
	Programmer       string
	ArduinoCLI       string
	ArduinoConfig    string
	Avrdude          string
	AvrdudeConf      string
	MCU              string
	BackupRoot       string
	USBaspBitClockUS float64
	USBaspAutoSlow   bool
}

type BoardCoreInitializeReport struct {
	FQBN                string `json:"fqbn"`
	MCU                 string `json:"mcu"`
	Programmer          string `json:"programmer"`
	BackupDirectory     string `json:"backup_directory"`
	BootloaderInstalled bool   `json:"bootloader_installed"`
	SlowUSBaspUsed      bool   `json:"slow_usbasp_used"`
}

func InitializeBoardCore(
	ctx context.Context,
	options BoardCoreInitializeOptions,
	output io.Writer,
) (BoardCoreInitializeReport, error) {
	return InitializeBoardCoreWithRunner(ctx, options, output, CommandRunnerFunc(Run))
}

func InitializeBoardCoreWithRunner(
	ctx context.Context,
	options BoardCoreInitializeOptions,
	output io.Writer,
	runner CommandRunner,
) (BoardCoreInitializeReport, error) {
	if runner == nil {
		return BoardCoreInitializeReport{}, errors.New("board initialization requires a command runner")
	}
	if output == nil {
		output = io.Discard
	}
	if options.FQBN == "" {
		options.FQBN = DefaultFQBN()
	}
	if options.MCU == "" {
		options.MCU = generatedBoardMCU
	}
	if options.Programmer == "" {
		options.Programmer = "usbasp"
	}
	if !strings.EqualFold(options.Programmer, "usbasp") &&
		!strings.EqualFold(options.Programmer, "usbasp_slow") {
		return BoardCoreInitializeReport{}, fmt.Errorf(
			"first-board initialization currently requires usbasp or usbasp_slow, got %q",
			options.Programmer,
		)
	}
	if strings.TrimSpace(options.BackupRoot) == "" {
		return BoardCoreInitializeReport{}, errors.New("board initialization requires a backup root")
	}
	report := BoardCoreInitializeReport{
		FQBN: options.FQBN, MCU: options.MCU, Programmer: options.Programmer,
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
		ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig, Avrdude: options.Avrdude,
		AvrdudeConf: options.AvrdudeConf, USBaspBitClockUS: options.USBaspBitClockUS,
	}
	if strings.EqualFold(options.Programmer, "usbasp_slow") && isp.USBaspBitClockUS <= 0 {
		isp.USBaspBitClockUS = defaultUSBaspSlowBitClockUS
	}

	fmt.Fprintln(output, "[1/4] Probing ISP target and validating the ATmega328P signature")
	isp.Operation = OperationProbe
	probe, err := Build(isp)
	if err != nil {
		return report, err
	}
	if err := ispRunner.Run(ctx, probe, output); err != nil {
		return report, fmt.Errorf("probe ISP target before any write: %w", err)
	}

	fmt.Fprintln(output, "[2/4] Capturing flash, EEPROM, signature, fuse, and lock-bit backup")
	backup := isp
	backup.Operation = OperationBackup
	backup.OutputPath = options.BackupRoot
	report.BackupDirectory, err = BackupWithRunner(ctx, backup, output, ispRunner)
	if err != nil {
		return report, fmt.Errorf("capture mandatory pre-initialization backup: %w", err)
	}
	if fallback != nil && fallback.slow {
		report.SlowUSBaspUsed = true
	}

	fmt.Fprintln(output, "[3/4] Installing bootloader and fuse/lock policy from the selected board core")
	coreProgrammer := options.Programmer
	if report.SlowUSBaspUsed && strings.EqualFold(coreProgrammer, "usbasp") {
		coreProgrammer = "usbasp_slow"
	}
	burnOptions := Options{
		Method: MethodArduino, Operation: OperationBurnBoot,
		FQBN: options.FQBN, Programmer: coreProgrammer,
		ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig, USBaspAutoSlow: options.USBaspAutoSlow,
	}
	burn, err := Build(burnOptions)
	if err != nil {
		return report, err
	}
	burnErr := runner.Run(ctx, burn, output)
	if burnErr != nil && options.USBaspAutoSlow && strings.EqualFold(coreProgrammer, "usbasp") {
		burnOptions.Programmer = "usbasp_slow"
		slowBurn, buildErr := Build(burnOptions)
		if buildErr != nil {
			return report, errors.Join(burnErr, buildErr)
		}
		fmt.Fprintln(output, "Core bootloader attempt failed; retrying with MiniCore's usbasp_slow programmer (-B32).")
		if slowErr := runner.Run(ctx, slowBurn, output); slowErr != nil {
			return report, errors.Join(
				fmt.Errorf("default-speed core bootloader install: %w", burnErr),
				fmt.Errorf("slow core bootloader install: %w", slowErr),
			)
		}
		report.SlowUSBaspUsed = true
	} else if burnErr != nil {
		return report, fmt.Errorf("install core bootloader: %w", burnErr)
	}
	report.BootloaderInstalled = true

	fmt.Fprintln(output, "[4/4] Re-reading target signature, fuses, and lock bits after bootloader install")
	verify := isp
	verify.Operation = OperationProbe
	if report.SlowUSBaspUsed && verify.USBaspBitClockUS <= 0 {
		verify.USBaspBitClockUS = defaultUSBaspSlowBitClockUS
	}
	verifyCommand, err := Build(verify)
	if err != nil {
		return report, err
	}
	verifyCommand.Args = append(
		verifyCommand.Args,
		"-Ulfuse:r:-:h", "-Uhfuse:r:-:h", "-Uefuse:r:-:h", "-Ulock:r:-:h",
	)
	if err := runner.Run(ctx, verifyCommand, output); err != nil {
		return report, fmt.Errorf("verify target after core bootloader install: %w", err)
	}
	return report, nil
}
