package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/wsrelay"
)

func runProgram(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	if len(args) == 0 {
		return errors.New("usage: controller program flash HEX [PORT] | recover HEX [PORT] | --operation DIAGNOSTIC [program flags]")
	}
	if len(args) != 0 && strings.EqualFold(args[0], "recover") {
		command := append([]string{"program"}, args...)
		return runExec(command, stdout, stderr, store)
	}
	return runProgramWithConfig(args, stdout, stderr, store.Current())
}

// runProgramWithConfig executes an already-selected programming command using
// the supplied defaults. Runtime callers pass their validated Store snapshot;
// the compile-only build path passes immutable application defaults instead.
func runProgramWithConfig(
	args []string,
	stdout, stderr io.Writer,
	config appconfig.Config,
) error {
	var normalizeErr error
	args, normalizeErr = normalizeProgramCLIArgs(args)
	if normalizeErr != nil {
		return normalizeErr
	}
	flags := flag.NewFlagSet("program", flag.ContinueOnError)
	flags.SetOutput(stderr)
	defaultMethod := config.Programming.Method
	if defaultMethod == "" {
		defaultMethod = "urclock"
	}
	method := flags.String("method", defaultMethod, "compile|toolchain|urclock|usbasp|avrdude")
	operation := flags.String(
		"operation",
		string(programmer.OperationWriteFlash),
		"write-flash|read-flash|verify-flash|read-eeprom|write-eeprom|metadata|probe|start|core-info|install-bootloader|backup",
	)
	device := flags.String(
		"device",
		envOr("PCCONTROLLER_DEVICE", ""),
		"COM ID, friendly name, VID:PID, serial:VALUE, or instance:VALUE",
	)
	port := flags.String("port", envOr("PCCONTROLLER_PORT", config.Connection.Port), "serial port")
	appDevice := flags.String(
		"app-device",
		"",
		"application UART selector used only before/after advanced USBasp programming",
	)
	hexPath := flags.String("hex", config.Paths.FirmwareHex, "Intel HEX file for avrdude workflows")
	sketch := flags.String("sketch", configuredProject(config, findProjectRoot()), "Arduino sketch directory")
	outputDir := flags.String("output-dir", "", "firmware dependency compile output directory")
	outputPath := flags.String("output", "", "output file for flash/EEPROM reads")
	fqbn := flags.String("fqbn", configuredFQBN(config), "Arduino FQBN")
	defaultProgrammer := config.Programming.Programmer
	if defaultProgrammer == "" {
		defaultProgrammer = "usbasp"
	}
	programmerName := flags.String("programmer", defaultProgrammer, "programmer ID (for example usbasp)")
	mcu := flags.String("mcu", "atmega328p", "avrdude MCU")
	baud := flags.Int("baud", 115200, "urclock baud rate")
	toolchainCLI := flags.String("toolchain-cli", config.Programming.ToolchainCLI, "firmware dependency CLI executable")
	toolchainConfig := flags.String("toolchain-config", config.Programming.ToolchainConfig, "firmware dependency CLI configuration file")
	avrdude := flags.String("avrdude", config.Programming.Avrdude, "avrdude executable")
	avrdudeConf := flags.String("avrdude-conf", config.Programming.AvrdudeConf, "avrdude.conf path")
	usbaspBitClock := flags.Float64("usbasp-bitclock-us", 0, "force USBasp AVRDUDE -B bit-clock period in microseconds")
	usbaspAutoSlow := flags.Bool("usbasp-auto-slow", true, "retry the first failed USBasp exchange at the conservative -B32 period")
	allowIncompleteBackup := flags.Bool(
		"allow-incomplete-backup",
		false,
		"advanced override: flash even if the automatic full backup fails",
	)
	reinitializeEEPROM := flags.Bool(
		"reinitialize-eeprom",
		false,
		"development only: retain raw EEPROM backup but discard incompatible semantic settings",
	)
	confirmEEPROM := flags.Bool(
		"confirm-eeprom-write",
		false,
		"explicitly authorize destructive EEPROM write",
	)
	dryRun := flags.Bool("dry-run", false, "print the resolved command without running it")
	appReconnect := flags.Bool(
		"app-reconnect",
		true,
		"authenticate application HELLO after the programmer releases the port",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *reinitializeEEPROM && *allowIncompleteBackup {
		return errors.New("--reinitialize-eeprom requires a complete verified raw flash, EEPROM, and metadata backup; it cannot be combined with --allow-incomplete-backup")
	}
	explicitDevice := false
	explicitProgrammerPort := false
	flags.Visit(func(value *flag.Flag) {
		switch value.Name {
		case "device", "port":
			explicitDevice = true
			explicitProgrammerPort = true
		case "app-device":
			explicitDevice = true
		}
	})
	if strings.TrimSpace(*device) != "" {
		*port = *device
		explicitDevice = true
	}
	options := programmer.Options{
		Method:    programmer.Method(strings.ToLower(*method)),
		Operation: programmer.Operation(strings.ToLower(*operation)),
		Port:      *port, HexPath: *hexPath, SketchPath: *sketch,
		OutputDir: *outputDir, OutputPath: *outputPath,
		FQBN: *fqbn, Programmer: *programmerName,
		MCU: *mcu, BaudRate: *baud, ArduinoCLI: *toolchainCLI, ArduinoConfig: *toolchainConfig,
		Avrdude: *avrdude, AvrdudeConf: *avrdudeConf,
		ConfirmEEPROMWrite: *confirmEEPROM,
		USBaspBitClockUS:   *usbaspBitClock, USBaspAutoSlow: *usbaspAutoSlow,
	}
	if options.Operation == programmer.OperationChipErase {
		return errors.New("raw chip erase is disabled; use 'controller board blank' for mandatory backup, EEPROM clearing, and full readback")
	}
	if options.USBaspBitClockUS < 0 {
		return errors.New("--usbasp-bitclock-us must be zero or positive")
	}
	// A dry-run describes the dependency command without probing the machine;
	// real execution still resolves and validates the configured executable.
	if *dryRun && options.Method == programmer.MethodCompile &&
		strings.TrimSpace(options.ArduinoCLI) == "" {
		options.ArduinoCLI = "arduino-cli"
	}
	safeFlash := options.Operation == programmer.OperationWriteFlash &&
		options.Method != programmer.MethodCompile
	if safeFlash {
		switch options.Method {
		case programmer.MethodUrclock:
			if strings.TrimSpace(*appDevice) != "" {
				return errors.New("--app-device is only valid with --method usbasp")
			}
		case programmer.MethodUSBasp:
			if explicitProgrammerPort {
				return errors.New("USBasp does not accept --port/--device; use --app-device only for the separate application UART lifecycle")
			}
			options.Port = ""
		case programmer.MethodArduino:
			return errors.New("direct dependency upload is disabled; compile to Intel HEX, then use program flash HEX [PORT]")
		default:
			return fmt.Errorf("guarded flash supports Urclock or USBasp, got %q", options.Method)
		}
	}
	deviceOperation := options.Method != programmer.MethodCompile &&
		options.Operation != programmer.OperationCoreInfo
	if deviceOperation && !*dryRun {
		probeContext, probeCancel := context.WithTimeout(
			context.Background(),
			400*time.Millisecond,
		)
		havePrimary := primaryAvailable(probeContext)
		probeCancel()
		if havePrimary {
			ctx, cancel := signalContext()
			defer cancel()
			if explicitDevice {
				selector := *port
				if options.Method == programmer.MethodUSBasp {
					selector = *appDevice
				}
				openContext, openCancel := context.WithTimeout(ctx, 15*time.Second)
				_, err := executeThroughPrimary(
					openContext,
					joinControllerCommand([]string{"open", selector}),
				)
				openCancel()
				if err != nil {
					return fmt.Errorf("select primary device: %w", err)
				}
			}
			if safeFlash {
				return delegatePrimaryFirmwareUpdate(
					ctx, options.HexPath, string(options.Method), "",
					*allowIncompleteBackup, *reinitializeEEPROM,
					stdout, callPrimary,
				)
			}
			remoteOptions := options
			remoteOptions.Port = ""
			words := programShellWords(remoteOptions)
			if safeFlash && *allowIncompleteBackup {
				words = append(words, "--allow-incomplete-backup")
			}
			output, err := executeThroughPrimary(
				ctx,
				joinControllerCommand(words),
			)
			if output != "" {
				fmt.Fprintln(stdout, output)
			}
			return err
		}
	}
	if operationNeedsSerialPort(options) {
		resolvedPort, err := resolveProgrammingPort(
			*port,
			config.Connection,
			os.Stdin,
			stderr,
		)
		if err != nil {
			return err
		}
		options.Port = resolvedPort
	}
	applicationPort := ""
	if safeFlash {
		switch options.Method {
		case programmer.MethodUrclock:
			applicationPort = options.Port
		case programmer.MethodUSBasp:
			selector := strings.TrimSpace(*appDevice)
			if selector == "" {
				if !*allowIncompleteBackup {
					return errors.New("standalone USBasp flash requires --app-device SELECTOR so MCU settings/display/audio can be preserved; --allow-incomplete-backup is the explicit recovery override")
				}
				fmt.Fprintln(stderr, "WARNING: standalone USBasp application lifecycle skipped by explicit recovery override")
			} else if !*dryRun {
				resolved, resolveErr := resolveProgrammingPort(
					selector,
					config.Connection,
					os.Stdin,
					stderr,
				)
				if resolveErr != nil {
					if !*allowIncompleteBackup {
						return fmt.Errorf("resolve USBasp application lifecycle device: %w", resolveErr)
					}
					fmt.Fprintln(stderr, "WARNING: USBasp application lifecycle selector could not be resolved; explicit recovery override continues:", resolveErr)
				} else {
					applicationPort = resolved
				}
			} else {
				applicationPort = selector
			}
		}
	}
	if options.Operation == programmer.OperationBackup {
		if err := programmer.ValidateBackup(options); err != nil {
			return err
		}
	}
	identityPort := options.Port
	if safeFlash && options.Method == programmer.MethodUSBasp {
		identityPort = applicationPort
	}
	if (options.Operation == programmer.OperationBackup || safeFlash) &&
		!*dryRun &&
		identityPort != "" {
		hello, identityErr := readApplicationIdentityBeforeProgramming(
			identityPort,
			config.Connection,
		)
		if identityErr != nil {
			fmt.Fprintln(
				stderr,
				"backup: application identity unavailable; continuing with programmer metadata:",
				identityErr,
			)
		} else {
			options.ApplicationHash = hello.BuildHash
			options.ApplicationIdentitySchema = hello.IdentitySchema
			options.ApplicationPackedTimestamp = hello.BuildTimestamp
		}
	}
	if options.Method == programmer.MethodCompile {
		var err error
		options, _, err = programmer.PlanCompile(options)
		if err != nil {
			return err
		}
	}
	if safeFlash {
		if *dryRun {
			fmt.Fprintf(
				stdout,
				"dry-run: guarded %s flash %s; require verified flash + EEPROM + metadata backup before write\n",
				options.Method,
				options.HexPath,
			)
			if *allowIncompleteBackup {
				fmt.Fprintln(stdout, "dry-run WARNING: explicit incomplete-backup override enabled")
			}
			if *reinitializeEEPROM {
				fmt.Fprintln(stdout, "dry-run DATA LOSS: current semantic MCU settings will not be restored; the mandatory raw EEPROM backup remains available")
			}
			if applicationPort != "" {
				fmt.Fprintf(stdout, "dry-run: application lifecycle selector=%s (never passed to ISP)\n", applicationPort)
			}
			return nil
		}
		ctx, cancel := signalContext()
		defer cancel()
		return executeGuardedCLIFlash(
			ctx, options, applicationPort, config.Connection,
			*allowIncompleteBackup, *reinitializeEEPROM, *appReconnect,
			stdout,
		)
	}
	var command programmer.Command
	var err error
	if options.Operation == programmer.OperationBackup {
		fmt.Fprintf(
			stdout,
			"backup flash + EEPROM + metadata under %s\n",
			options.OutputPath,
		)
	} else {
		command, err = programmer.Build(options)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, command.String())
	}
	if *dryRun {
		if options.Method == programmer.MethodUSBasp &&
			options.Operation == programmer.OperationWriteFlash {
			fmt.Fprintln(stdout, "dry-run: execution will first read hfuse and require EESAVE bit 3 = 0")
		}
		return nil
	}
	ctx, cancel := signalContext()
	defer cancel()
	programErr := programmer.Execute(ctx, options, stdout)
	if !*appReconnect || !deviceOperation || options.Port == "" {
		return programErr
	}
	reconnectErr := reconnectApplicationAfterProgramming(
		context.WithoutCancel(ctx),
		options.Port,
		config.Connection,
		stdout,
	)
	return errors.Join(programErr, reconnectErr)
}

type primaryCallFunc func(context.Context, string, any, any) error

// delegatePrimaryFirmwareUpdate transfers an immutable candidate to the
// already-running primary, starts one idempotent update operation there, and
// follows its durable typed status instead of opening the serial port locally.
func delegatePrimaryFirmwareUpdate(
	ctx context.Context,
	firmwarePath, method, port string,
	allowIncompleteBackup, reinitializeEEPROM bool,
	output io.Writer,
	call primaryCallFunc,
) error {
	document, err := programmer.LoadIntelHex(firmwarePath)
	if err != nil {
		return fmt.Errorf("validate delegated firmware: %w", err)
	}
	content, err := os.ReadFile(firmwarePath)
	if err != nil {
		return fmt.Errorf("read delegated firmware: %w", err)
	}
	uploadRequest := artifacts.UploadRequest{
		Kind: artifacts.KindFirmware, Name: filepath.Base(firmwarePath),
		Data: content, SHA256: document.SourceSHA256, Bytes: int64(len(content)),
	}
	var upload artifacts.OperationResult
	if err := call(ctx, "controller.artifact.upload", uploadRequest, &upload); err != nil {
		return fmt.Errorf("upload firmware to primary: %w", err)
	}
	if upload.Artifact == nil ||
		!strings.EqualFold(upload.Artifact.SHA256, document.SourceSHA256) {
		return errors.New("primary did not acknowledge the exact uploaded firmware hash")
	}

	var primarySnapshot controllerapi.Snapshot
	_ = call(ctx, "controller.snapshot", map[string]any{}, &primarySnapshot)
	idempotencyDocument, _ := json.Marshal(struct {
		Firmware           string `json:"firmware"`
		Method             string `json:"method"`
		Port               string `json:"port"`
		Serial             string `json:"serial"`
		Instance           string `json:"instance"`
		ReinitializeEEPROM bool   `json:"reinitialize_eeprom"`
	}{
		Firmware: upload.Artifact.SHA256, Method: method, Port: port,
		Serial: primarySnapshot.Port.SerialNumber, Instance: primarySnapshot.Port.InstanceID,
		ReinitializeEEPROM: reinitializeEEPROM,
	})
	idempotencyDigest := sha256.Sum256(idempotencyDocument)
	updateRequest := artifacts.UpdateRequest{
		ArtifactSHA256: upload.Artifact.SHA256,
		Authorized:     true, Method: method, Port: port,
		AllowIncompleteBackup: allowIncompleteBackup,
		ReinitializeEEPROM:    reinitializeEEPROM,
		IdempotencyKey:        "firmware:" + hex.EncodeToString(idempotencyDigest[:]),
	}
	var operation artifacts.OperationResult
	if err := call(ctx, "controller.update.firmware", updateRequest, &operation); err != nil {
		return fmt.Errorf("start primary firmware update: %w", err)
	}
	if operation.Operation.ID == "" {
		return errors.New("primary returned a firmware update without an operation ID")
	}
	if output != nil {
		fmt.Fprintf(
			output,
			"delegated firmware SHA-256 %s to primary operation %s (reused=%t)\n",
			upload.Artifact.SHA256, operation.Operation.ID, operation.Reused,
		)
	}
	return monitorPrimaryFirmwareUpdate(ctx, operation.Operation.ID, output, call)
}

func monitorPrimaryFirmwareUpdate(
	ctx context.Context,
	operationID string,
	output io.Writer,
	call primaryCallFunc,
) error {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	last := ""
	for {
		var status artifacts.UpdateStatus
		if err := call(
			ctx, "controller.update.status",
			map[string]string{"id": operationID}, &status,
		); err != nil {
			return fmt.Errorf("follow primary firmware update %s: %w", operationID, err)
		}
		line := fmt.Sprintf("%s %d%% %s", status.State, status.ProgressPercent, status.Detail)
		if line != last && output != nil {
			fmt.Fprintf(output, "primary firmware update: %s\n", line)
			last = line
		}
		switch status.State {
		case "completed":
			if output != nil {
				fmt.Fprintf(
					output,
					"primary firmware update complete: method=%s bootloader=%s sha256=%s\n",
					status.ProgrammingMethod, status.BootloaderOutcome, status.ArtifactSHA256,
				)
			}
			return nil
		case "failed", "cancelled":
			suggestion := ""
			if status.ISPFallbackSuggested {
				suggestion = "; ISP fallback suggested"
			}
			return fmt.Errorf(
				"primary firmware update %s: %s (code=%s method=%s bootloader=%s%s)",
				status.State, status.Detail, status.ErrorCode,
				status.ProgrammingMethod, status.BootloaderOutcome, suggestion,
			)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runEEPROM(args []string, stdout, stderr io.Writer) error {
	const usage = "usage: controller eeprom factory-defaults --output EEPROM.hex | inspect (--input IMAGE.hex | --backup-manifest MANIFEST.json) | export --backup-manifest MANIFEST.json --output SETTINGS.hex | import --backup-manifest MANIFEST.json --settings SETTINGS.hex --output EEPROM.hex | restore --backup-manifest MANIFEST.json --output EEPROM.hex"
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch strings.ToLower(args[0]) {
	case "factory-defaults":
		flags := flag.NewFlagSet("eeprom factory-defaults", flag.ContinueOnError)
		flags.SetOutput(stderr)
		output := flags.String("output", "", "new no-overwrite host-owned factory EEPROM Intel HEX image")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*output) == "" {
			return errors.New(usage)
		}
		if err := programmer.WriteDefaultEEPROMIntelHex(*output); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "factory EEPROM image:", *output)
		fmt.Fprintln(stdout, "Includes current settings, empty RF storage, reset journal space, and host-owned status LED profiles; no board was written.")
		return nil

	case "inspect":
		flags := flag.NewFlagSet("eeprom inspect", flag.ContinueOnError)
		flags.SetOutput(stderr)
		input := flags.String("input", "", "current EEPROM Intel HEX image")
		manifest := flags.String("backup-manifest", "", "validated complete backup manifest")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || (strings.TrimSpace(*input) == "") == (strings.TrimSpace(*manifest) == "") {
			return errors.New(usage)
		}
		var decoded programmer.OfflineEEPROMDecode
		var err error
		if strings.TrimSpace(*manifest) != "" {
			decoded, err = programmer.DecodeBackupEEPROM(*manifest)
		} else {
			decoded, err = programmer.DecodeOfflineEEPROMHex(*input)
		}
		if err != nil {
			return err
		}
		encoded, _ := json.MarshalIndent(decoded, "", "  ")
		fmt.Fprintln(stdout, string(encoded))
		if !decoded.Settings.Supported {
			return fmt.Errorf("unsupported current EEPROM settings layout: %s", decoded.Settings.Issue)
		}
		if !decoded.Settings.Valid {
			return fmt.Errorf("current EEPROM settings failed semantic validation: %s", decoded.Settings.Issue)
		}
		return nil

	case "export", "restore":
		flags := flag.NewFlagSet("eeprom "+strings.ToLower(args[0]), flag.ContinueOnError)
		flags.SetOutput(stderr)
		manifest := flags.String("backup-manifest", "", "validated complete backup manifest")
		output := flags.String("output", "", "new no-overwrite Intel HEX artifact")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifest) == "" || strings.TrimSpace(*output) == "" {
			return errors.New(usage)
		}
		var result programmer.EEPROMTransferResult
		var err error
		if strings.EqualFold(args[0], "export") {
			result, err = programmer.ExportCurrentEEPROMSettings(*manifest, *output)
		} else {
			result, err = programmer.PrepareCurrentEEPROMRestore(*manifest, *output)
		}
		return writeEEPROMTransferResult(stdout, result, err)

	case "import":
		flags := flag.NewFlagSet("eeprom import", flag.ContinueOnError)
		flags.SetOutput(stderr)
		manifest := flags.String("backup-manifest", "", "validated complete backup manifest")
		settings := flags.String("settings", "", "sparse current settings Intel HEX artifact")
		output := flags.String("output", "", "new full EEPROM restore image")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifest) == "" ||
			strings.TrimSpace(*settings) == "" || strings.TrimSpace(*output) == "" {
			return errors.New(usage)
		}
		result, err := programmer.ImportCurrentEEPROMSettings(
			*manifest, *settings, *output,
		)
		return writeEEPROMTransferResult(stdout, result, err)
	default:
		return errors.New(usage)
	}
}

func writeEEPROMTransferResult(
	output io.Writer,
	result programmer.EEPROMTransferResult,
	err error,
) error {
	if err != nil {
		return err
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(output, string(encoded))
	fmt.Fprintln(output, "Validated backup remained unchanged; no serial port was opened and no board EEPROM was written.")
	return nil
}

func normalizeProgramCLIArgs(args []string) ([]string, error) {
	const usage = "usage: controller program flash HEX [PORT] [--method urclock|usbasp] [--app-device SELECTOR] [--allow-incomplete-backup] [--reinitialize-eeprom]"
	shortcut := 0
	for shortcut < len(args) {
		argument := args[shortcut]
		if guardedFlashBooleanFlag(argument) || guardedFlashInlineValueFlag(argument) {
			shortcut++
			continue
		}
		if guardedFlashValueFlag(argument) && shortcut+1 < len(args) {
			shortcut += 2
			continue
		}
		break
	}
	if shortcut >= len(args) || !strings.EqualFold(args[shortcut], "flash") {
		return args, nil
	}
	if shortcut+1 >= len(args) {
		return nil, errors.New(usage)
	}
	arguments := append([]string(nil), args[:shortcut]...)
	arguments = append(arguments, args[shortcut+1:]...)
	positionals := make([]string, 0, 2)
	flags := make([]string, 0, len(arguments))
	selectedMethod := programmer.MethodUrclock
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if guardedFlashBooleanFlag(argument) {
			flags = append(flags, argument)
			continue
		}
		if guardedFlashInlineValueFlag(argument) {
			lower := strings.ToLower(argument)
			if strings.HasPrefix(lower, "--method=") {
				selectedMethod = programmer.Method(strings.TrimPrefix(lower, "--method="))
			} else {
				flags = append(flags, argument)
			}
			continue
		}
		if guardedFlashValueFlag(argument) {
			if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "-") {
				return nil, fmt.Errorf("guarded flash flag %s requires a value", argument)
			}
			if strings.EqualFold(argument, "--method") {
				selectedMethod = programmer.Method(strings.ToLower(arguments[index+1]))
			} else {
				flags = append(flags, argument, arguments[index+1])
			}
			index++
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return nil, fmt.Errorf("unknown guarded flash flag %q", argument)
		}
		positionals = append(positionals, argument)
	}
	if len(positionals) < 1 || len(positionals) > 2 {
		return nil, errors.New(usage)
	}
	result := []string{
		"--operation", string(programmer.OperationWriteFlash),
		"--method", string(selectedMethod),
		"--hex", positionals[0],
	}
	result = append(result, flags...)
	if len(positionals) == 2 {
		selectorFlag := "--port"
		if selectedMethod == programmer.MethodUSBasp {
			selectorFlag = "--app-device"
		}
		result = append(result, selectorFlag, positionals[1])
	}
	return result, nil
}

// configIndependentProgramCompile recognizes only an explicitly selected
// compile method. Every operation that could use a device, IPC, or persisted
// host preference continues through appconfig.Open and its full validation.
func configIndependentProgramCompile(args []string) bool {
	if len(args) < 2 || !strings.EqualFold(args[0], "program") {
		return false
	}
	normalized, err := normalizeProgramCLIArgs(args[1:])
	if err != nil {
		return false
	}
	method := ""
	explicit := false
	for index := 0; index < len(normalized); index++ {
		argument := normalized[index]
		lower := strings.ToLower(argument)
		switch {
		case lower == "--":
			return explicit && strings.EqualFold(method, string(programmer.MethodCompile))
		case lower == "--method":
			if index+1 >= len(normalized) {
				return false
			}
			index++
			method = normalized[index]
			explicit = true
		case strings.HasPrefix(lower, "--method="):
			method = strings.TrimSpace(argument[len("--method="):])
			explicit = true
		}
	}
	return explicit && strings.EqualFold(method, string(programmer.MethodCompile))
}

func configIndependentToolchainCompile(args []string) bool {
	return len(args) >= 2 &&
		strings.EqualFold(args[0], "toolchain") &&
		strings.EqualFold(args[1], "compile")
}

func guardedFlashBooleanFlag(argument string) bool {
	lower := strings.ToLower(argument)
	if lower == "--allow-incomplete-backup" || lower == "--reinitialize-eeprom" || lower == "--dry-run" {
		return true
	}
	return lower == "--app-reconnect" || strings.HasPrefix(lower, "--app-reconnect=")
}

func guardedFlashValueFlag(argument string) bool {
	return strings.EqualFold(argument, "--app-device") ||
		strings.EqualFold(argument, "--method")
}

func guardedFlashInlineValueFlag(argument string) bool {
	lower := strings.ToLower(argument)
	return strings.HasPrefix(lower, "--app-device=") ||
		strings.HasPrefix(lower, "--method=")
}

func executeGuardedCLIFlash(
	ctx context.Context,
	options programmer.Options,
	applicationPort string,
	connection appconfig.Connection,
	allowIncompleteBackup, reinitializeEEPROM, appReconnect bool,
	output io.Writer,
) error {
	paths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		return err
	}
	if err := programmer.EnsureHostDataPaths(paths); err != nil {
		return err
	}
	var application *control.Runtime
	var programmingSession *control.ProgrammingSession
	lifecycleOptions := control.ProgrammingLifecycleOptions{
		DataPaths: paths, ReinitializeEEPROM: reinitializeEEPROM,
	}
	if applicationPort != "" {
		candidate := control.New(control.Options{
			Filter:         ports.Filter{Port: applicationPort},
			BaudRate:       connection.BaudRate,
			StartupWait:    time.Duration(connection.StartupWaitMS) * time.Millisecond,
			RequestTimeout: time.Duration(connection.RequestTimeoutMS) * time.Millisecond,
			HelloAttempts:  connection.HelloAttempts,
		})
		connectContext, connectCancel := context.WithTimeout(ctx, 8*time.Second)
		connectErr := candidate.EnsureConnected(connectContext)
		connectCancel()
		if connectErr != nil {
			_ = candidate.Close()
			if !allowIncompleteBackup {
				return fmt.Errorf("prepare guarded flash application connection: %w", connectErr)
			}
			fmt.Fprintln(
				output,
				"WARNING: application lifecycle connection failed; explicit recovery override continues:",
				connectErr,
			)
		} else {
			application = candidate
			lifecycleOptions.Outputs = control.NewOutputScheduler(application)
			defer application.Close()
			var prepareErr error
			programmingSession, prepareErr = control.PrepareProgrammingSession(
				ctx,
				application,
				options.HexPath,
				lifecycleOptions,
				output,
			)
			if prepareErr != nil {
				if !allowIncompleteBackup {
					return fmt.Errorf("prepare application programming state: %w", prepareErr)
				}
				fmt.Fprintln(
					output,
					"WARNING: application programming preparation was incomplete; explicit recovery override continues:",
					prepareErr,
				)
			}
			if err := application.Close(); err != nil {
				return fmt.Errorf(
					"release application UART (settings recovery marker retained): %w", err,
				)
			}
		}
	}
	backup := options
	backup.Operation = programmer.OperationBackup
	backup.HexPath = ""
	backup.OutputPath = ""
	write := options
	write.Operation = programmer.OperationWriteFlash
	var afterBackup programmer.PostBackupOperation
	if application != nil && programmingSession != nil && reinitializeEEPROM {
		afterBackup = func(
			backupContext context.Context,
			_ programmer.AutomaticPreflashResult,
			writer io.Writer,
		) error {
			application.ResumeAuto()
			reconnectContext, reconnectCancel := context.WithTimeout(
				context.WithoutCancel(backupContext), 12*time.Second,
			)
			reconnectErr := application.EnsureConnected(reconnectContext)
			reconnectCancel()
			if reconnectErr != nil {
				return fmt.Errorf("reconnect application after untouched raw backup: %w", reconnectErr)
			}
			armContext, armCancel := context.WithTimeout(
				context.WithoutCancel(backupContext), 8*time.Second,
			)
			armErr := control.ArmProgrammingSessionAfterBackup(
				armContext, application, programmingSession, lifecycleOptions, writer,
			)
			armCancel()
			closeErr := application.Close()
			if armErr != nil {
				return errors.Join(armErr, closeErr)
			}
			if closeErr != nil {
				return fmt.Errorf("release application UART after arming programming latch: %w", closeErr)
			}
			return nil
		}
	}
	result, flashErr := programmer.AutomaticBackupThenFlash(
		ctx,
		programmer.AutomaticPreflashOptions{
			FirmwarePath: options.HexPath,
			Backup:       backup, DataPaths: paths,
			AllowFlashWithoutFullBackup: allowIncompleteBackup,
			AfterBackup:                 afterBackup,
		},
		programmer.CommandRunnerFunc(programmer.Run),
		func(flashContext context.Context, path string, writer io.Writer) error {
			write.HexPath = path
			return programmer.Execute(flashContext, write, writer)
		},
		output,
	)
	fmt.Fprintf(output, "firmware SHA-256: %s\n", result.FirmwareSHA256)
	if result.BackupReference != "" {
		fmt.Fprintf(output, "verified backup: %s\n", result.BackupReference)
	}
	if result.BackupManifest != "" {
		fmt.Fprintf(output, "backup manifest: %s\n", result.BackupManifest)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(output, "WARNING:", warning)
	}
	if result.Flashed {
		fmt.Fprintln(output, "guarded firmware flash completed")
	}
	verifiedProgram := flashErr == nil && result.Flashed
	abortedBeforeWrite := programmingSession != nil && !result.BackupComplete &&
		!allowIncompleteBackup
	if verifiedProgram && reinitializeEEPROM {
		if factoryErr := programFactoryEEPROM(
			context.WithoutCancel(ctx), paths, options, output,
		); factoryErr != nil {
			flashErr = errors.Join(flashErr, factoryErr)
			verifiedProgram = false
		} else {
			fmt.Fprintln(output, "host-owned Silent/Prog factory EEPROM programmed and independently read back")
		}
	}
	if programmingSession != nil {
		var markerErr error
		if abortedBeforeWrite {
			markerErr = control.AbortProgrammingSessionBeforeWrite(programmingSession)
			if markerErr == nil {
				fmt.Fprintln(output,
					"mandatory backup stopped before any flash write; restoring the pre-flash application state")
			}
		} else {
			markerErr = control.MarkProgrammingSessionComplete(
				programmingSession, verifiedProgram,
			)
		}
		if markerErr != nil {
			flashErr = errors.Join(flashErr, fmt.Errorf(
				"persist host programming completion (safe-state marker retained): %w", markerErr,
			))
			verifiedProgram = false
			abortedBeforeWrite = false
		}
	}
	var reconnectErr error
	var restoreErr error
	if application != nil {
		application.ResumeAuto()
		reconnectContext, reconnectCancel := context.WithTimeout(
			context.WithoutCancel(ctx), 12*time.Second,
		)
		reconnectErr = application.EnsureConnected(reconnectContext)
		reconnectCancel()
		if reconnectErr != nil {
			reconnectErr = fmt.Errorf(
				"application HELLO reconnect failed; settings recovery marker retained: %w",
				reconnectErr,
			)
		} else if verifiedProgram || abortedBeforeWrite {
			restoreContext, restoreCancel := context.WithTimeout(
				context.WithoutCancel(ctx), 8*time.Second,
			)
			restoreErr = control.RestoreProgrammingSession(
				restoreContext,
				application,
				programmingSession,
				lifecycleOptions,
				output,
			)
			restoreCancel()
			if restoreErr == nil {
				connected := application.Snapshot()
				fmt.Fprintf(
					output,
					"application mode restored and authenticated on %s: %s\n",
					connected.Port.Name,
					fmt.Sprintf(
						"%s build=%08X timestamp=%s capabilities=0x%08X",
						connected.Hello.Name,
						connected.Hello.BuildHash,
						connected.Hello.BuildStamp,
						connected.Hello.Capabilities,
					),
				)
			}
		} else if programmingSession != nil {
			fmt.Fprintln(output,
				"programmer result was not verified successful; programming latch and recovery marker retained")
		}
	} else if appReconnect && applicationPort != "" {
		reconnectErr = reconnectApplicationAfterProgramming(
			context.WithoutCancel(ctx), applicationPort, connection, output,
		)
	}
	return errors.Join(flashErr, reconnectErr, restoreErr)
}

func programFactoryEEPROM(
	ctx context.Context,
	paths programmer.HostDataPaths,
	base programmer.Options,
	output io.Writer,
) error {
	return programmer.ProgramLatchedFactoryEEPROM(
		ctx, paths, base, programmer.Execute, output,
	)
}

func readApplicationIdentityBeforeProgramming(
	port string,
	connection appconfig.Connection,
) (native.Hello, error) {
	runtime := control.New(control.Options{
		Filter:         ports.Filter{Port: port},
		BaudRate:       connection.BaudRate,
		StartupWait:    time.Duration(connection.StartupWaitMS) * time.Millisecond,
		RequestTimeout: time.Duration(connection.RequestTimeoutMS) * time.Millisecond,
		HelloAttempts:  connection.HelloAttempts,
	})
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := runtime.EnsureConnected(ctx); err != nil {
		return native.Hello{}, err
	}
	return runtime.Snapshot().Hello, nil
}

func operationNeedsSerialPort(options programmer.Options) bool {
	if options.Method == programmer.MethodUrclock {
		return true
	}
	if options.Method == programmer.MethodArduino {
		return options.Operation == programmer.OperationWriteFlash
	}
	return options.Method == programmer.MethodAvrdude &&
		!strings.EqualFold(options.Programmer, "usbasp")
}

func resolveProgrammingPort(
	selector string,
	config appconfig.Connection,
	input io.Reader,
	output io.Writer,
) (string, error) {
	options := &connectionFlags{
		port: config.Port, vid: config.VID, pid: config.PID, name: config.Name,
		baud:      config.BaudRate,
		overrides: make(map[string]bool),
	}
	if config.LastDevice != nil {
		options.preferred = ports.Identity{
			Port: config.LastDevice.Port,
			VID:  config.LastDevice.VID, PID: config.LastDevice.PID,
			SerialNumber: config.LastDevice.SerialNumber,
			Name:         config.LastDevice.Name,
			InstanceID:   config.LastDevice.InstanceID,
		}
	}
	if strings.TrimSpace(selector) != "" {
		options.device = selector
		options.overrides["device"] = true
	}
	if err := selectInteractiveDevice(options, input, output); err != nil {
		return "", err
	}
	filter := options.filter()
	port := filter.Port
	if port == "" {
		list, err := ports.List()
		if err != nil {
			return "", err
		}
		if selected, ok := ports.PreferredCandidate(
			ports.Candidates(list, filter),
			filter.Preferred,
		); ok {
			port = selected.Name
		}
	}
	if port == "" {
		return "", errors.New(
			"no unique serial device matched; use --device COM_ID, friendly name, VID:PID, serial:VALUE, or instance:VALUE",
		)
	}
	return port, nil
}

func programShellWords(options programmer.Options) []string {
	if options.Operation == programmer.OperationWriteFlash &&
		(options.Method == programmer.MethodUrclock || options.Method == programmer.MethodUSBasp) {
		words := []string{"program", "flash", options.HexPath}
		if options.Port != "" && options.Method == programmer.MethodUrclock {
			words = append(words, options.Port)
		}
		if options.Method == programmer.MethodUSBasp {
			words = append(words, "--method", string(programmer.MethodUSBasp))
		}
		return words
	}
	words := []string{"program"}
	if options.Operation != "" &&
		options.Operation != programmer.OperationWriteFlash {
		words = append(words, string(options.Operation))
	}
	words = append(words, string(options.Method))
	switch {
	case options.Operation == programmer.OperationBackup:
		words = append(words, options.OutputPath)
	case options.Method == programmer.MethodCompile ||
		options.Method == programmer.MethodArduino &&
			options.Operation == programmer.OperationWriteFlash:
		words = append(words, options.SketchPath)
	case options.Operation == programmer.OperationReadFlash ||
		options.Operation == programmer.OperationReadEEPROM:
		words = append(words, options.OutputPath)
	case options.Operation != programmer.OperationMetadata &&
		options.Operation != programmer.OperationProbe &&
		options.Operation != programmer.OperationStart &&
		options.Operation != programmer.OperationCoreInfo &&
		options.Operation != programmer.OperationBurnBoot:
		words = append(words, options.HexPath)
	}
	if options.Operation == programmer.OperationWriteEEPROM {
		words = append(words, "CONFIRM")
	}
	if options.Port != "" {
		words = append(words, options.Port)
	}
	return words
}

func runBoot(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	translated, err := bootCLIArguments(args)
	if err != nil {
		return err
	}
	return runProgram(translated, stdout, stderr, store)
}

func bootCLIArguments(args []string) ([]string, error) {
	const usage = "usage: controller boot probe|info|metadata|backup DIR|read FILE|write FILE|verify FILE|start [program flags]"
	if len(args) == 0 {
		return nil, errors.New(usage)
	}
	translated := []string{"--method", string(programmer.MethodUrclock)}
	action := strings.ToLower(args[0])
	switch action {
	case "probe":
		translated = append(translated, "--operation", string(programmer.OperationProbe))
		return append(translated, args[1:]...), nil
	case "info", "metadata":
		translated = append(translated, "--operation", string(programmer.OperationMetadata))
		return append(translated, args[1:]...), nil
	case "start":
		translated = append(translated, "--operation", string(programmer.OperationStart))
		return append(translated, args[1:]...), nil
	case "backup":
		if len(args) < 2 {
			return nil, errors.New(usage)
		}
		translated = append(
			translated,
			"--operation", string(programmer.OperationBackup),
			"--output", args[1],
		)
		return append(translated, args[2:]...), nil
	case "read":
		if len(args) < 2 {
			return nil, errors.New(usage)
		}
		translated = append(
			translated,
			"--operation", string(programmer.OperationReadFlash),
			"--output", args[1],
		)
		return append(translated, args[2:]...), nil
	case "write", "verify":
		if len(args) < 2 {
			return nil, errors.New(usage)
		}
		operation := programmer.OperationWriteFlash
		if action == "verify" {
			operation = programmer.OperationVerifyFlash
		}
		translated = append(
			translated,
			"--operation", string(operation),
			"--hex", args[1],
		)
		return append(translated, args[2:]...), nil
	default:
		return nil, errors.New(usage)
	}
}

func runToolchain(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	if len(args) != 0 && strings.EqualFold(args[0], "sync") {
		return runToolchainSync(args[1:], stdout, stderr, store)
	}
	if len(args) != 0 && strings.EqualFold(args[0], "adopt") {
		return runToolchainAdopt(args[1:], stdout, stderr, store)
	}
	if len(args) != 0 && strings.EqualFold(args[0], "bootstrap") {
		return runToolchainBootstrap(args[1:], stdout, stderr, store)
	}
	if len(args) != 0 && strings.EqualFold(args[0], "profile") {
		return runToolchainProfile(args[1:], stdout, stderr)
	}
	if len(args) != 0 && strings.EqualFold(args[0], "lock") {
		return runToolchainLock(args[1:], stdout, stderr)
	}
	if len(args) != 0 && strings.EqualFold(args[0], "check") {
		return runToolchainCheck(args[1:], stdout, stderr)
	}
	if len(args) != 0 && strings.EqualFold(args[0], "update") {
		return runToolchainUpdate(args[1:], stdout, stderr)
	}
	translated, err := toolchainCLIArguments(args)
	if err != nil {
		return err
	}
	return runProgram(translated, stdout, stderr, store)
}

func runToolchainAdopt(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("toolchain adopt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourceData := flags.String("source-data", "", "verified managed Arduino data directory")
	sourceUser := flags.String("source-user", "", "verified managed Arduino user directory")
	targetData := flags.String("target-data", "", "shared Arduino data directory")
	targetUser := flags.String("target-user", "", "shared Arduino user/sketchbook directory")
	firmwareCLI := flags.String("cli", "", "shared firmware dependency CLI executable")
	targetConfig := flags.String("toolchain-config", "", "shared firmware dependency CLI config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *sourceData == "" || *sourceUser == "" ||
		*targetData == "" || *targetUser == "" || *firmwareCLI == "" || *targetConfig == "" {
		return errors.New("usage: controller toolchain adopt --source-data DIR --source-user DIR --target-data DIR --target-user DIR --cli PATH --toolchain-config FILE")
	}
	report, err := programmer.AdoptToolchain(*sourceData, *sourceUser, *targetData, *targetUser)
	if err != nil {
		return err
	}
	if _, err := store.Update(func(config *appconfig.Config) error {
		config.Programming.ToolchainCLI = *firmwareCLI
		config.Programming.ToolchainConfig = *targetConfig
		return nil
	}); err != nil {
		return fmt.Errorf("save adopted shared toolchain paths: %w", err)
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Fprintln(stdout, string(encoded))
	fmt.Fprintln(stdout, "Adopted verified toolchain into the shared Arduino installation and saved its paths.")
	return nil
}

func runToolchainSync(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	flags := flag.NewFlagSet("toolchain sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	firmwareCLI := flags.String(
		"cli",
		store.Current().Programming.ToolchainCLI,
		"firmware dependency CLI executable",
	)
	directRetry := flags.Bool(
		"direct-retry",
		true,
		"retry a failed proxy attempt once without proxy variables",
	)
	dryRun := flags.Bool("dry-run", false, "print every update/install step without executing it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller toolchain sync [--cli PATH] [--direct-retry=false] [--dry-run]")
	}
	ctx, cancel := signalContext()
	defer cancel()
	report, err := programmer.SyncToolchain(ctx, programmer.ToolchainSyncOptions{
		ToolchainCLI: *firmwareCLI, DirectRetry: *directRetry, DryRun: *dryRun,
	}, stdout)
	if *dryRun {
		fmt.Fprintf(stdout, "\nToolchain sync plan complete: %d steps; no changes made.\n", len(report.Steps))
	} else {
		succeeded := 0
		for _, step := range report.Steps {
			if step.Succeeded {
				succeeded++
			}
		}
		fmt.Fprintf(stdout, "\nToolchain sync result: %d/%d steps succeeded.\n", succeeded, len(report.Steps))
	}
	return err
}

func runToolchainBootstrap(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	flags := flag.NewFlagSet("toolchain bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "latest-compatible toolchain policy JSON")
	lockPath := flags.String("lock", "", "exact resolved toolchain lock JSON")
	locked := flags.Bool("locked", false, "bootstrap the existing lock without checking registries")
	installDir := flags.String("install-dir", "", "managed tool directory (host data directory by default)")
	firmwareCLI := flags.String("cli", "", "use an existing dependency CLI instead of the managed resolved copy")
	directRetry := flags.Bool("direct-retry", true, "retry failed network steps once without proxy variables")
	dryRun := flags.Bool("dry-run", false, "print verified download/install plan without changing the machine")
	saveCLI := flags.Bool("save-cli", true, "save the resolved dependency path in PC-side host config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller toolchain bootstrap [--policy FILE] [--locked --lock FILE] [--install-dir DIR] [--cli PATH] [--direct-retry=false] [--dry-run]")
	}
	ctx, cancel := signalContext()
	defer cancel()
	var profile programmer.ToolchainProfile
	if *locked {
		resolvedLockPath := defaultToolchainMetadataPath(*lockPath, "toolchain-lock.json")
		lock, err := programmer.LoadToolchainLock(resolvedLockPath)
		if err != nil {
			return fmt.Errorf("load exact rollback lock: %w", err)
		}
		profile = lock.Firmware
		fmt.Fprintln(stdout, "Using exact resolved lock:", resolvedLockPath)
	} else {
		policy, err := programmer.LoadToolchainPolicy(defaultToolchainMetadataPath(*policyPath, "toolchain-profile.json"))
		if err != nil {
			return err
		}
		resolution, err := programmer.ResolveToolchainPolicy(ctx, policy, programmer.ToolchainResolveOptions{
			DirectRetry: *directRetry, ModuleDir: defaultToolchainModuleDir(),
		})
		if err != nil {
			return fmt.Errorf("resolve latest compatible toolchain (use --locked for an intentional offline rollback): %w", err)
		}
		profile = resolution.Lock.Firmware
		fmt.Fprintf(stdout, "Resolved latest stable toolchain: CLI %s, %s@%s, Urboot %s, Go %s\n",
			profile.CLI.Version, profile.CoreID, profile.CoreVersion,
			resolution.Lock.Bootloader.Tag, resolution.Lock.Go.Version)
	}
	report, bootstrapErr := programmer.BootstrapToolchain(
		ctx,
		programmer.ToolchainBootstrapOptions{
			Profile: profile, CLI: *firmwareCLI, InstallDir: *installDir,
			DirectRetry: *directRetry, DryRun: *dryRun,
		},
		stdout,
	)
	if bootstrapErr == nil && !*dryRun && *saveCLI {
		_, saveErr := store.Update(func(config *appconfig.Config) error {
			config.Programming.ToolchainCLI = report.CLIPath
			config.Programming.ToolchainConfig = report.ConfigPath
			return nil
		})
		if saveErr != nil {
			bootstrapErr = fmt.Errorf("save managed toolchain path in PC config: %w", saveErr)
		} else {
			fmt.Fprintln(stdout, "Saved managed firmware CLI path in PC-side host configuration.")
		}
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Fprintln(stdout, string(encoded))
	return bootstrapErr
}

func runToolchainProfile(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain profile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("policy", "", "latest-compatible toolchain policy JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller toolchain profile [--policy FILE]")
	}
	profile, err := programmer.LoadToolchainPolicy(defaultToolchainMetadataPath(*manifest, "toolchain-profile.json"))
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(encoded))
	return nil
}

func runToolchainLock(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain lock", flag.ContinueOnError)
	flags.SetOutput(stderr)
	lockPath := flags.String("lock", "", "exact resolved toolchain lock JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller toolchain lock [--lock FILE]")
	}
	lock, err := programmer.LoadToolchainLock(defaultToolchainMetadataPath(*lockPath, "toolchain-lock.json"))
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(encoded))
	return nil
}

func runToolchainCheck(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "latest-compatible toolchain policy JSON")
	lockPath := flags.String("lock", "", "exact resolved toolchain lock JSON")
	directRetry := flags.Bool("direct-retry", true, "retry failed registry reads once without proxy variables")
	includeCanary := flags.Bool("include-canary", false, "report prerelease CLI and Urboot main without selecting them")
	requireCurrent := flags.Bool("require-current", false, "fail when the generated stable lock is stale")
	jsonOutput := flags.Bool("json", false, "emit machine-readable resolution report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller toolchain check [--policy FILE] [--lock FILE] [--include-canary] [--require-current] [--json]")
	}
	policy, err := programmer.LoadToolchainPolicy(defaultToolchainMetadataPath(*policyPath, "toolchain-profile.json"))
	if err != nil {
		return err
	}
	current, err := programmer.LoadToolchainLock(defaultToolchainMetadataPath(*lockPath, "toolchain-lock.json"))
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	resolution, err := programmer.ResolveToolchainPolicy(ctx, policy, programmer.ToolchainResolveOptions{
		DirectRetry: *directRetry, IncludeCanary: *includeCanary,
		ModuleDir: defaultToolchainModuleDir(),
	})
	if err != nil {
		return err
	}
	changes := programmer.CompareToolchainLocks(current, resolution.Lock)
	if *jsonOutput {
		encoded, marshalErr := json.MarshalIndent(struct {
			Current bool                         `json:"current"`
			Changes []programmer.ToolchainChange `json:"changes"`
			Canary  programmer.ToolchainCanary   `json:"canary,omitempty"`
		}{Current: len(changes) == 0, Changes: changes, Canary: resolution.Canary}, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Fprintln(stdout, string(encoded))
	} else {
		printToolchainChanges(stdout, changes)
		if *includeCanary {
			fmt.Fprintf(stdout, "Canary only (never auto-deployed): CLI %s; Urboot %s@%s\n",
				resolution.Canary.CLIRelease, resolution.Canary.BootloaderRef,
				resolution.Canary.BootloaderCommit)
		}
	}
	if *requireCurrent && len(changes) != 0 {
		return fmt.Errorf("resolved toolchain lock is stale (%d changes)", len(changes))
	}
	return nil
}

func runToolchainUpdate(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("toolchain update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "latest-compatible toolchain policy JSON")
	lockPath := flags.String("lock", "", "exact resolved toolchain lock JSON")
	directRetry := flags.Bool("direct-retry", true, "retry failed registry reads once without proxy variables")
	includeCanary := flags.Bool("include-canary", true, "report canaries without writing them to the stable lock")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller toolchain update [--policy FILE] [--lock FILE] [--include-canary=false]")
	}
	resolvedPolicyPath := defaultToolchainMetadataPath(*policyPath, "toolchain-profile.json")
	resolvedLockPath := defaultToolchainMetadataPath(*lockPath, "toolchain-lock.json")
	if resolvedLockPath == "" {
		return errors.New("toolchain lock path cannot be resolved; pass --lock FILE")
	}
	policy, err := programmer.LoadToolchainPolicy(resolvedPolicyPath)
	if err != nil {
		return err
	}
	var current programmer.ToolchainLock
	if loaded, loadErr := programmer.LoadToolchainLock(resolvedLockPath); loadErr == nil {
		current = loaded
	}
	ctx, cancel := signalContext()
	defer cancel()
	resolution, err := programmer.ResolveToolchainPolicy(ctx, policy, programmer.ToolchainResolveOptions{
		DirectRetry: *directRetry, IncludeCanary: *includeCanary,
		ModuleDir: defaultToolchainModuleDir(),
	})
	if err != nil {
		return err
	}
	changes := programmer.CompareToolchainLocks(current, resolution.Lock)
	printToolchainChanges(stdout, changes)
	written, err := programmer.UpdateToolchainLock(resolvedLockPath, current, resolution.Lock)
	if err != nil {
		return err
	}
	if written {
		fmt.Fprintln(stdout, "Wrote exact stable dependency lock:", resolvedLockPath)
	} else {
		fmt.Fprintln(stdout, "Preserved lock timestamp; no substantive dependency changed.")
	}
	if *includeCanary {
		fmt.Fprintf(stdout, "Observed canaries without selecting them: CLI %s; Urboot %s@%s\n",
			resolution.Canary.CLIRelease, resolution.Canary.BootloaderRef,
			resolution.Canary.BootloaderCommit)
	}
	return nil
}

func printToolchainChanges(output io.Writer, changes []programmer.ToolchainChange) {
	if len(changes) == 0 {
		fmt.Fprintln(output, "✅ Resolved dependency lock is current.")
		return
	}
	fmt.Fprintf(output, "⬆ Latest-compatible resolution found %d change(s):\n", len(changes))
	for _, change := range changes {
		fmt.Fprintf(output, "  %-12s %-42s %s -> %s\n", change.Area, change.Name, change.Current, change.Resolved)
	}
}

func defaultToolchainMetadataPath(explicit, name string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	root := findProjectRoot()
	if root != "." {
		candidate := filepath.Join(root, "Tools", "Controller", name)
		if _, err := os.Stat(candidate); err == nil || name == "toolchain-lock.json" {
			return candidate
		}
	}
	if _, err := os.Stat(name); err == nil {
		return name
	}
	return ""
}

func defaultToolchainModuleDir() string {
	root := findProjectRoot()
	if root == "." {
		return "."
	}
	return filepath.Join(root, "Tools", "Controller")
}

func toolchainCLIArguments(args []string) ([]string, error) {
	const usage = "usage: controller toolchain check|update|bootstrap|sync|profile|lock|compile SKETCH|core-info|install-bootloader [flags]"
	if len(args) == 0 {
		return nil, errors.New(usage)
	}
	switch strings.ToLower(args[0]) {
	case "compile":
		if len(args) < 2 {
			return nil, errors.New(usage)
		}
		result := []string{
			"--method", string(programmer.MethodCompile),
			"--sketch", args[1],
		}
		return append(result, args[2:]...), nil
	case "core-info", "info":
		result := []string{
			"--method", string(programmer.MethodArduino),
			"--operation", string(programmer.OperationCoreInfo),
		}
		return append(result, args[1:]...), nil
	case "install-bootloader":
		result := []string{
			"--method", string(programmer.MethodArduino),
			"--operation", string(programmer.OperationBurnBoot),
		}
		return append(result, args[1:]...), nil
	default:
		return nil, errors.New(usage)
	}
}

func reconnectApplicationAfterProgramming(
	ctx context.Context,
	port string,
	connection appconfig.Connection,
	output io.Writer,
) error {
	runtime := control.New(control.Options{
		Filter:         ports.Filter{Port: port},
		BaudRate:       connection.BaudRate,
		StartupWait:    time.Duration(connection.StartupWaitMS) * time.Millisecond,
		RequestTimeout: time.Duration(connection.RequestTimeoutMS) * time.Millisecond,
		HelloAttempts:  connection.HelloAttempts,
	})
	defer runtime.Close()
	reconnectContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := runtime.EnsureConnected(reconnectContext); err != nil {
		return fmt.Errorf(
			"programmer completed, but application HELLO reconnect failed: %w",
			err,
		)
	}
	snapshot := runtime.Snapshot()
	fmt.Fprintf(
		output,
		"Application mode restored and authenticated on %s: %s\n",
		snapshot.Port.Name,
		snapshot.Hello.Name,
	)
	return nil
}

func validatedWSFlashMethod(value string) (programmer.Method, error) {
	method := programmer.Method(strings.ToLower(strings.TrimSpace(value)))
	if method != programmer.MethodUrclock && method != programmer.MethodUSBasp {
		return "", fmt.Errorf("ws client method %q is unsupported; use urclock or usbasp", value)
	}
	return method, nil
}

func runWS(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	config := store.Current()
	if len(args) == 0 {
		return errors.New("usage: ws serve|client")
	}
	switch strings.ToLower(args[0]) {
	case "serve":
		flags := flag.NewFlagSet("ws serve", flag.ContinueOnError)
		flags.SetOutput(stderr)
		file := flags.String("file", config.Paths.FirmwareHex, "watched Intel HEX path")
		listen := flags.String("listen", "127.0.0.1:3000", "HTTP listen address")
		path := flags.String("path", "/firmware", "WebSocket endpoint path")
		poll := flags.Duration("poll", 500*time.Millisecond, "file polling interval")
		maxSize := flags.Int64("max-size", wsrelay.DefaultMaxSize, "maximum firmware bytes")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *file == "" && flags.NArg() != 0 {
			*file = flags.Arg(0)
		}
		logger := log.New(stdout, "ws-server: ", log.LstdFlags)
		ctx, cancel := signalContext()
		defer cancel()
		return wsrelay.Serve(ctx, wsrelay.ServerOptions{
			Listen: *listen, Path: *path, FirmwarePath: *file,
			PollInterval: *poll, MaxSize: *maxSize, Logger: logger,
		})

	case "client":
		flags := flag.NewFlagSet("ws client", flag.ContinueOnError)
		flags.SetOutput(stderr)
		url := flags.String("url", envOr("PCCONTROLLER_WS_URL", "ws://127.0.0.1:3000/firmware"), "relay WebSocket URL")
		method := flags.String("method", "urclock", "urclock|usbasp")
		port := flags.String("port", envOr("PCCONTROLLER_PORT", config.Connection.Port), "serial port")
		appDevice := flags.String(
			"app-device",
			"",
			"application UART selector used only before/after advanced USBasp programming",
		)
		programmerName := flags.String("programmer", "", "custom avrdude programmer")
		mcu := flags.String("mcu", "atmega328p", "avrdude MCU")
		baud := flags.Int("baud", 115200, "urclock baud rate")
		avrdude := flags.String("avrdude", config.Programming.Avrdude, "avrdude executable")
		avrdudeConf := flags.String("avrdude-conf", config.Programming.AvrdudeConf, "avrdude.conf path")
		allowIncomplete := flags.Bool("allow-incomplete-backup", false, "explicitly allow flashing without a complete verified backup")
		reinitializeEEPROM := flags.Bool("reinitialize-eeprom", false, "development only: retain raw EEPROM backup but discard incompatible semantic settings")
		reconnect := flags.Duration("reconnect", 2*time.Second, "reconnect delay")
		maxSize := flags.Int64("max-size", wsrelay.DefaultMaxSize, "maximum firmware bytes")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *reinitializeEEPROM && *allowIncomplete {
			return errors.New("--reinitialize-eeprom requires a complete verified raw flash, EEPROM, and metadata backup; it cannot be combined with --allow-incomplete-backup")
		}
		flashMethod, methodErr := validatedWSFlashMethod(*method)
		if methodErr != nil {
			return methodErr
		}
		logger := log.New(stdout, "ws-client: ", log.LstdFlags)
		ctx, cancel := signalContext()
		defer cancel()
		return wsrelay.RunClient(ctx, wsrelay.ClientOptions{
			URL: *url, ReconnectDelay: *reconnect, MaxSize: *maxSize,
			Logger: logger,
			OnFirmware: func(ctx context.Context, message wsrelay.FirmwareMessage) error {
				tempPath, cleanup, err := wsrelay.SaveTemp(message)
				if err != nil {
					return err
				}
				defer cleanup()
				probeContext, probeCancel := context.WithTimeout(
					ctx,
					400*time.Millisecond,
				)
				havePrimary := primaryAvailable(probeContext)
				probeCancel()
				if havePrimary {
					if flashMethod == programmer.MethodUSBasp && strings.TrimSpace(*appDevice) != "" {
						openContext, openCancel := context.WithTimeout(ctx, 15*time.Second)
						_, openErr := executeThroughPrimary(
							openContext,
							joinControllerCommand([]string{"open", strings.TrimSpace(*appDevice)}),
						)
						openCancel()
						if openErr != nil {
							return fmt.Errorf("select relay programming application device: %w", openErr)
						}
					}
					words := []string{"program", "flash", tempPath, "--method", string(flashMethod)}
					if flashMethod == programmer.MethodUrclock && strings.TrimSpace(*port) != "" {
						words = append(words, *port)
					}
					if *allowIncomplete {
						words = append(words, "--allow-incomplete-backup")
					}
					if *reinitializeEEPROM {
						words = append(words, "--reinitialize-eeprom")
					}
					output, routeErr := executeThroughPrimary(
						ctx,
						joinControllerCommand(words),
					)
					if output != "" {
						logger.Print(output)
					}
					return routeErr
				}
				applicationSelector := strings.TrimSpace(*port)
				if flashMethod == programmer.MethodUSBasp {
					applicationSelector = strings.TrimSpace(*appDevice)
					if applicationSelector == "" && !*allowIncomplete {
						return errors.New("standalone USBasp relay programming requires --app-device SELECTOR or the explicit --allow-incomplete-backup recovery override")
					}
				}
				applicationPort := ""
				if applicationSelector != "" {
					applicationPort, err = resolveProgrammingPort(
						applicationSelector, config.Connection, os.Stdin, stderr,
					)
					if err != nil {
						if !*allowIncomplete {
							return fmt.Errorf("resolve relay programming application device: %w", err)
						}
						logger.Print("WARNING: application selector unresolved under explicit recovery override: ", err)
						applicationPort = ""
					}
				}
				programmerPort := applicationPort
				if flashMethod == programmer.MethodUSBasp {
					programmerPort = ""
				}
				flashOptions := programmer.Options{
					Operation: programmer.OperationWriteFlash,
					Method:    flashMethod,
					Port:      programmerPort, HexPath: tempPath, Programmer: *programmerName,
					MCU: *mcu, BaudRate: *baud, Avrdude: *avrdude,
					AvrdudeConf: *avrdudeConf,
				}
				command, err := programmer.Build(flashOptions)
				if err != nil {
					return err
				}
				logger.Print("guarded preflight: ", command.String())
				return executeGuardedCLIFlash(
					ctx, flashOptions, applicationPort, config.Connection,
					*allowIncomplete, *reinitializeEEPROM, true, stdout,
				)
			},
		})
	default:
		return fmt.Errorf("unknown ws command %q", args[0])
	}
}
