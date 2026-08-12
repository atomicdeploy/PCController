package programmer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// BoardCoreInitializeOptions owns the ISP portion of first-board setup. The
// application/UART phase deliberately remains separate so a board without a
// UART adapter still receives a complete core-provided bootloader and fuse set.
type BoardCoreInitializeOptions struct {
	FQBN             string
	Programmer       string
	ArduinoCLI       string
	ArduinoConfig    string
	SketchPath       string
	Avrdude          string
	AvrdudeConf      string
	MCU              string
	BackupRoot       string
	USBaspBitClockUS float64
	USBaspAutoSlow   bool
}

type BoardCoreInitializeReport struct {
	FQBN                 string                `json:"fqbn"`
	MCU                  string                `json:"mcu"`
	Programmer           string                `json:"programmer"`
	BackupDirectory      string                `json:"backup_directory"`
	BootloaderInstalled  bool                  `json:"bootloader_installed"`
	SlowUSBaspUsed       bool                  `json:"slow_usbasp_used"`
	FuseRecoveryApplied  bool                  `json:"fuse_recovery_applied"`
	NormalSpeedRecovered bool                  `json:"normal_speed_recovered"`
	BootloaderProgrammer string                `json:"bootloader_programmer"`
	InitialFuses         string                `json:"initial_fuses,omitempty"`
	InitialLock          string                `json:"initial_lock,omitempty"`
	Phases               []BoardLifecyclePhase `json:"phases"`
}

type boardCoreFusePolicy struct {
	Low      byte
	High     byte
	Extended byte
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
	report.SlowUSBaspUsed = forceSlow
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
	if err := phase("probe", func() error { return ispRunner.Run(ctx, probe, output) }); err != nil {
		return report, fmt.Errorf("probe ISP target before any write: %w", err)
	}

	if fallback != nil && fallback.slow {
		report.SlowUSBaspUsed = true
	}

	fmt.Fprintln(output, "[2/5] Recovering normal-speed USBasp when the selected core expects an external crystal")
	coreProgrammer := options.Programmer
	if fallback != nil && fallback.slow && strings.EqualFold(coreProgrammer, "usbasp") {
		fmt.Fprintln(output, "Slow SCK was needed for discovery; retaining fuse/lock evidence before repairing only the selected core's fuse policy at -B32.")
		if phaseErr := phase("recover_fast_isp", func() error {
			initial, evidenceErr := captureFuseLockEvidence(ctx, isp, ispRunner, output)
			if evidenceErr != nil {
				return fmt.Errorf("capture pre-fuse evidence: %w", evidenceErr)
			}
			report.InitialFuses = fmt.Sprintf("%02X/%02X/%02X", initial.Low, initial.High, initial.Extended)
			report.InitialLock = fmt.Sprintf("%02X", initial.Lock)
			policy, policyErr := resolveBoardCoreFusePolicy(ctx, options, runner)
			if policyErr != nil {
				return fmt.Errorf("resolve selected core fuse policy: %w", policyErr)
			}
			fuseCommand, buildErr := buildSlowFuseCorrectionCommand(isp, policy)
			if buildErr != nil {
				return fmt.Errorf("build slow fuse correction: %w", buildErr)
			}
			if fuseErr := runner.Run(ctx, fuseCommand, output); fuseErr != nil {
				return fmt.Errorf("apply selected core fuse policy at slow SCK: %w", fuseErr)
			}
			report.FuseRecoveryApplied = true

			fastProbeOptions := isp
			fastProbeOptions.Operation = OperationProbe
			fastProbeOptions.USBaspBitClockUS = 0
			fastProbe, buildErr := Build(fastProbeOptions)
			if buildErr != nil {
				return fmt.Errorf("build normal-speed probe after fuse correction: %w", buildErr)
			}
			fmt.Fprintln(output, "Fuse policy corrected; retrying USBasp at normal speed before loading the bootloader.")
			if fastErr := runner.Run(ctx, fastProbe, output); fastErr == nil {
				report.NormalSpeedRecovered = true
				// The evidence/fuse transaction used the retained slow wrapper.
				// From this point the selected external clock is proven, so all
				// long backup and core operations must use normal USBasp speed.
				isp.USBaspBitClockUS = 0
				ispRunner = runner
				fmt.Fprintln(output, "Normal-speed USBasp recovered; bootloader installation will use the fast programmer.")
			} else {
				fmt.Fprintln(output, "Normal-speed USBasp is still unavailable after fuse correction; retaining -B32 for bootloader installation.")
				coreProgrammer = "usbasp_slow"
			}
			return nil
		}); phaseErr != nil {
			return report, phaseErr
		}
	} else if report.SlowUSBaspUsed && strings.EqualFold(coreProgrammer, "usbasp") {
		coreProgrammer = "usbasp_slow"
	}

	fmt.Fprintln(output, "[3/5] Capturing mandatory flash, EEPROM, signature, fuse, and lock-bit backup")
	backup := isp
	backup.Operation = OperationBackup
	backup.OutputPath = options.BackupRoot
	err = phase("backup", func() error {
		report.BackupDirectory, err = BackupWithRunner(ctx, backup, output, ispRunner)
		return err
	})
	if err != nil {
		return report, fmt.Errorf("capture mandatory pre-initialization backup: %w", err)
	}

	fmt.Fprintln(output, "[4/5] Installing bootloader and fuse/lock policy from the selected board core")
	burnOptions := Options{
		Method: MethodArduino, Operation: OperationBurnBoot,
		FQBN: options.FQBN, Programmer: coreProgrammer,
		ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig, USBaspAutoSlow: options.USBaspAutoSlow,
	}
	burn, err := Build(burnOptions)
	if err != nil {
		return report, err
	}
	burnErr := phase("install_bootloader", func() error { return runner.Run(ctx, burn, output) })
	if burnErr != nil && options.USBaspAutoSlow && strings.EqualFold(coreProgrammer, "usbasp") {
		burnOptions.Programmer = "usbasp_slow"
		slowBurn, buildErr := Build(burnOptions)
		if buildErr != nil {
			return report, errors.Join(burnErr, buildErr)
		}
		fmt.Fprintln(output, "Core bootloader attempt failed; retrying with MiniCore's usbasp_slow programmer (-B32).")
		if slowErr := phase("install_bootloader_slow_retry", func() error { return runner.Run(ctx, slowBurn, output) }); slowErr != nil {
			return report, errors.Join(
				fmt.Errorf("default-speed core bootloader install: %w", burnErr),
				fmt.Errorf("slow core bootloader install: %w", slowErr),
			)
		}
		coreProgrammer = "usbasp_slow"
		report.SlowUSBaspUsed = true
	} else if burnErr != nil {
		return report, fmt.Errorf("install core bootloader: %w", burnErr)
	}
	report.BootloaderProgrammer = coreProgrammer
	report.BootloaderInstalled = true

	fmt.Fprintln(output, "[5/5] Re-reading target signature, fuses, and lock bits after bootloader install")
	verify := isp
	verify.Operation = OperationProbe
	if strings.EqualFold(coreProgrammer, "usbasp_slow") && verify.USBaspBitClockUS <= 0 {
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
	if err := phase("verify", func() error { return runner.Run(ctx, verifyCommand, output) }); err != nil {
		return report, fmt.Errorf("verify target after core bootloader install: %w", err)
	}
	return report, nil
}

func resolveBoardCoreFusePolicy(
	ctx context.Context,
	options BoardCoreInitializeOptions,
	runner CommandRunner,
) (boardCoreFusePolicy, error) {
	command, err := Build(Options{
		Method: MethodArduino, Operation: OperationCoreProperties,
		FQBN: options.FQBN, SketchPath: options.SketchPath,
		ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig,
	})
	if err != nil {
		return boardCoreFusePolicy{}, err
	}
	var properties bytes.Buffer
	if err := runner.Run(ctx, command, &properties); err != nil {
		return boardCoreFusePolicy{}, err
	}
	values := make(map[string]string)
	for _, line := range strings.Split(properties.String(), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	parse := func(key string) (byte, error) {
		value, ok := values[key]
		if !ok || value == "" {
			return 0, fmt.Errorf("Arduino core properties omitted %s", key)
		}
		parsed, parseErr := strconv.ParseUint(value, 0, 8)
		if parseErr != nil {
			return 0, fmt.Errorf("parse %s=%q: %w", key, value, parseErr)
		}
		return byte(parsed), nil
	}
	low, err := parse("bootloader.low_fuses")
	if err != nil {
		return boardCoreFusePolicy{}, err
	}
	high, err := parse("bootloader.high_fuses")
	if err != nil {
		return boardCoreFusePolicy{}, err
	}
	extended, err := parse("bootloader.extended_fuses")
	if err != nil {
		return boardCoreFusePolicy{}, err
	}
	return boardCoreFusePolicy{Low: low, High: high, Extended: extended}, nil
}

func buildSlowFuseCorrectionCommand(isp Options, policy boardCoreFusePolicy) (Command, error) {
	isp.Operation = OperationProbe
	command, err := Build(isp)
	if err != nil {
		return Command{}, err
	}
	command = withUSBaspBitClock(command, defaultUSBaspSlowBitClockUS)
	command.Args = append(command.Args,
		"-D",
		fmt.Sprintf("-Ulfuse:w:0x%02X:m", policy.Low),
		fmt.Sprintf("-Uhfuse:w:0x%02X:m", policy.High),
		fmt.Sprintf("-Uefuse:w:0x%02X:m", policy.Extended),
	)
	return command, nil
}
