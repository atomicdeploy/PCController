package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
)

const boardInitializeUsage = "usage: controller board initialize [--skip-toolchain] [--portable-cli] [--fqbn FQBN] | controller board provision [--name NAME] [--uart auto|PORT|none] [--firmware HEX] [--force-initialize] [--skip-toolchain] [--portable-cli] | controller board blank --confirm NAME [--uart auto|PORT|none] | controller board name [get|set NAME|clear]"

func runBoard(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	if len(args) == 0 {
		return errors.New(boardInitializeUsage)
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	if action != "initialize" && action != "provision" && action != "blank" {
		return runExec(append([]string{"board"}, args...), stdout, stderr, store)
	}
	claim, havePrimary, err := preparePrimaryMode("board " + action)
	if err != nil {
		return err
	}
	if havePrimary {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		output, callErr := executeThroughPrimary(ctx, joinControllerCommand(append([]string{"board", action}, args[1:]...)))
		if output != "" {
			fmt.Fprintln(stdout, output)
		}
		return callErr
	}
	if claim != nil {
		defer claim.Close()
	}
	runtime := newRuntime(&connectionFlags{}, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	ctx, cancel := signalContext()
	defer cancel()
	if action == "blank" {
		return blankBoard(ctx, runtime, args[1:], store, stdout)
	}
	if action == "initialize" {
		return initializeBoard(ctx, runtime, args[1:], store, findProjectRoot(), stdout)
	}
	return provisionBoard(ctx, runtime, args[1:], store, findProjectRoot(), stdout)
}

func blankBoard(
	ctx context.Context,
	runtime *control.Runtime,
	args []string,
	store *appconfig.Store,
	output io.Writer,
) error {
	if runtime == nil || store == nil {
		return errors.New("board blanking requires the primary runtime and configuration store")
	}
	flags := flag.NewFlagSet("board blank", flag.ContinueOnError)
	flags.SetOutput(output)
	confirm := flags.String("confirm", "", "exact current board name, or ERASE-BOARD when UART identity is unavailable")
	uart := flags.String("uart", "auto", "application UART port, auto, or none")
	programmerName := flags.String("programmer", configuredProgrammer(store.Current()), "ISP programmer (usbasp or usbasp_slow)")
	bitClock := flags.Float64("usbasp-bitclock-us", 0, "force the USBasp AVRDUDE -B period")
	jsonReport := flags.Bool("json", false, "append the machine-readable blanking report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*confirm) == "" || *bitClock < 0 {
		return errors.New("usage: controller board blank --confirm NAME [--uart auto|PORT|none] [--usbasp-bitclock-us N] [--json]")
	}

	uartPort, warning, err := resolveInitializationUART(*uart, store.Current().Connection)
	if err != nil {
		return err
	}
	confirmedName := ""
	programmingLatchArmed := false
	var programmingLatchDuration time.Duration
	if uartPort != "" {
		connectContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = runtime.Open(connectContext, uartPort)
		cancel()
		if err != nil {
			return fmt.Errorf("authenticate board identity before blanking on %s: %w", uartPort, err)
		}
		nameContext, nameCancel := context.WithTimeout(ctx, 5*time.Second)
		name, nameErr := runtime.BoardName(nameContext)
		nameCancel()
		if nameErr != nil {
			return fmt.Errorf("read board name before blanking: %w", nameErr)
		}
		confirmedName = name.Name
		if err := validateBoardBlankConfirmation(*confirm, uartPort, confirmedName); err != nil {
			return err
		}
		fmt.Fprintf(output, "Authenticated board %q on %s before destructive blanking.\n", confirmedName, uartPort)
		lease, _, leaseErr := runtime.AcquireProgramState("board-lifecycle", "destructive ISP blank")
		if leaseErr != nil {
			return fmt.Errorf("enter programming-safe application state: %w", leaseErr)
		}
		defer lease.Release()
		latchStarted := time.Now()
		latchContext, latchCancel := context.WithTimeout(ctx, 3*time.Second)
		_, latchErr := control.ArmProgrammingSafetyLatch(latchContext, runtime)
		latchCancel()
		programmingLatchDuration = time.Since(latchStarted)
		if latchErr != nil {
			return fmt.Errorf("persist programming safety latch before destructive blanking: %w", latchErr)
		}
		programmingLatchArmed = true
		fmt.Fprintf(output, "Programming safety latch persisted in %d ms; application outputs are interlocked before ISP erase.\n", programmingLatchDuration.Milliseconds())
	} else {
		if err := validateBoardBlankConfirmation(*confirm, "", ""); err != nil {
			return err
		}
		if warning != "" {
			fmt.Fprintln(output, "WARNING:", warning)
		}
		fmt.Fprintln(output, "WARNING: proceeding without an application identity; USBasp signature and complete backup remain mandatory.")
	}
	_ = runtime.Close()

	paths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		return err
	}
	if err := programmer.EnsureHostDataPaths(paths); err != nil {
		return err
	}
	fmt.Fprintln(output, "A new complete recovery backup will be committed before erase; the controller will finish at verified ATmega328P factory fuses and all-FF flash/EEPROM.")
	report, err := programmer.BlankBoard(ctx, programmer.BoardBlankOptions{
		FQBN: configuredFQBN(store.Current()), SketchPath: configuredProject(store.Current(), findProjectRoot()),
		MCU: programmer.DefaultBoardTarget().MCU, Programmer: *programmerName,
		ArduinoCLI: store.Current().Programming.ToolchainCLI, ArduinoConfig: store.Current().Programming.ToolchainConfig,
		Avrdude: store.Current().Programming.Avrdude, AvrdudeConf: store.Current().Programming.AvrdudeConf,
		BackupRoot: paths.BackupsDir, USBaspBitClockUS: *bitClock, USBaspAutoSlow: true,
	}, output)
	if err != nil {
		return err
	}
	result := map[string]any{
		"board_name_before_erase":       confirmedName,
		"uart_port":                     uartPort,
		"programming_latch_armed":       programmingLatchArmed,
		"programming_latch_duration_ms": programmingLatchDuration.Milliseconds(),
		"blank":                         report,
	}
	if *jsonReport {
		encoded, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Fprintln(output, string(encoded))
	}
	return nil
}

func validateBoardBlankConfirmation(supplied, uartPort, boardName string) error {
	required := "ERASE-BOARD"
	if strings.TrimSpace(uartPort) != "" && boardName != "" {
		required = boardName
	}
	if supplied != required {
		if strings.TrimSpace(uartPort) == "" {
			return errors.New("UART identity is unavailable; pass the literal --confirm ERASE-BOARD to authorize ISP-only blanking")
		}
		return fmt.Errorf("blank confirmation %q does not exactly match %q", supplied, required)
	}
	return nil
}

func initializeBoard(
	ctx context.Context,
	runtime *control.Runtime,
	args []string,
	store *appconfig.Store,
	fallbackProject string,
	output io.Writer,
) error {
	if runtime == nil || store == nil {
		return errors.New("board initialization requires the primary runtime and configuration store")
	}
	flags := flag.NewFlagSet("board initialize", flag.ContinueOnError)
	flags.SetOutput(output)
	skipToolchain := flags.Bool("skip-toolchain", false, "use the already configured toolchain without an install/repair pass")
	portableCLI := flags.Bool("portable-cli", false, "download/use a fresh managed portable arduino-cli even when another CLI is configured")
	toolchainCLI := flags.String("cli", "", "explicit arduino-cli executable")
	fqbn := flags.String("fqbn", configuredFQBN(store.Current()), "board FQBN and core bootloader policy")
	programmerName := flags.String("programmer", configuredProgrammer(store.Current()), "ISP programmer (usbasp or usbasp_slow)")
	bitClock := flags.Float64("usbasp-bitclock-us", 0, "force the USBasp AVRDUDE -B period")
	jsonReport := flags.Bool("json", false, "append the machine-readable initialization report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *bitClock < 0 {
		return errors.New(boardInitializeUsage)
	}
	project := configuredProject(store.Current(), fallbackProject)
	if strings.TrimSpace(project) == "" || project == "." {
		return errors.New("board initialization cannot locate the firmware project; configure paths.project")
	}
	dataPaths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		return err
	}
	if err := programmer.EnsureHostDataPaths(dataPaths); err != nil {
		return err
	}

	fmt.Fprintln(output, "=== PCController board initialization (fuse policy + bootloader only) ===")
	fmt.Fprintln(output, "FQBN:", *fqbn)
	fmt.Fprintln(output, "Host data:", dataPaths.DataDir)
	fmt.Fprintln(output, "Project:", project)

	cli := strings.TrimSpace(*toolchainCLI)
	cliConfig := strings.TrimSpace(store.Current().Programming.ToolchainConfig)
	if cli == "" && !*portableCLI {
		cli = strings.TrimSpace(store.Current().Programming.ToolchainCLI)
	}
	if *portableCLI {
		cliConfig = ""
	}
	if !*skipToolchain {
		fmt.Fprintln(output, "\n[toolchain] Installing/repairing the exact core and libraries (proxy values remain hidden)")
		report, bootstrapErr := programmer.BootstrapToolchain(ctx, programmer.ToolchainBootstrapOptions{
			Profile: programmer.DefaultToolchainProfile(), CLI: cli, DirectRetry: true,
		}, output)
		if bootstrapErr != nil {
			return fmt.Errorf("prepare firmware toolchain: %w", bootstrapErr)
		}
		cli = report.CLIPath
		cliConfig = report.ConfigPath
		if _, err := store.Update(func(config *appconfig.Config) error {
			config.Programming.ToolchainCLI = report.CLIPath
			config.Programming.ToolchainConfig = report.ConfigPath
			config.Programming.FQBN = *fqbn
			config.Programming.Programmer = *programmerName
			config.Paths.Project = project
			return nil
		}); err != nil {
			return fmt.Errorf("save initialized toolchain selection: %w", err)
		}
	} else if cli == "" {
		return errors.New("--skip-toolchain requires a configured or explicit --cli executable")
	}

	// Initialization intentionally owns only ISP fuse/bootloader setup.  It
	// never compiles or uploads application firmware; `board provision` does
	// that only when a usable existing application/bootloader is unavailable or
	// when an explicit image was supplied.
	_ = runtime.Close()
	fmt.Fprintln(output, "\n[isp] USBasp signature, complete backup, core bootloader/fuses, and post-write verification")
	coreReport, err := programmer.InitializeBoardCore(ctx, programmer.BoardCoreInitializeOptions{
		FQBN: *fqbn, Programmer: *programmerName, ArduinoCLI: cli, ArduinoConfig: cliConfig,
		SketchPath:       project,
		Avrdude:          store.Current().Programming.Avrdude,
		AvrdudeConf:      store.Current().Programming.AvrdudeConf,
		BackupRoot:       dataPaths.BackupsDir,
		USBaspBitClockUS: *bitClock, USBaspAutoSlow: true,
	}, output)
	if err != nil {
		return err
	}

	result := map[string]any{"core": coreReport, "initialized": true}
	fmt.Fprintln(output, "\nREADY: selected fuse policy and bootloader verified. Run board provision to deploy or inspect an application.")
	return appendBoardInitializationReport(output, result, *jsonReport)
}

// provisionBoard is the application-level lifecycle. A healthy authenticated
// PCController firmware takes precedence: it is inspected and retained unless
// the caller supplies --firmware or requests --force-initialize.  This avoids
// resetting a known-good board merely because somebody ran the full lifecycle.
func provisionBoard(
	ctx context.Context,
	runtime *control.Runtime,
	args []string,
	store *appconfig.Store,
	fallbackProject string,
	output io.Writer,
) error {
	if runtime == nil || store == nil {
		return errors.New("board provisioning requires the primary runtime and configuration store")
	}
	flags := flag.NewFlagSet("board provision", flag.ContinueOnError)
	flags.SetOutput(output)
	uart := flags.String("uart", "auto", "application UART port, auto, or none")
	boardName := flags.String("name", "", "persist an operator board name of at most eight printable ASCII characters")
	firmware := flags.String("firmware", "", "precompiled application Intel HEX to upload through the verified bootloader")
	forceInitialize := flags.Bool("force-initialize", false, "run ISP initialize even if an application authenticates")
	skipToolchain := flags.Bool("skip-toolchain", false, "use the configured toolchain without an install/repair pass")
	portableCLI := flags.Bool("portable-cli", false, "use a fresh managed portable arduino-cli")
	toolchainCLI := flags.String("cli", "", "explicit arduino-cli executable")
	fqbn := flags.String("fqbn", configuredFQBN(store.Current()), "board FQBN and core bootloader policy")
	jsonReport := flags.Bool("json", false, "append the machine-readable provisioning report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New(boardInitializeUsage)
	}
	if err := native.ValidateBoardName(*boardName); err != nil {
		return err
	}

	result := map[string]any{"initialized": false, "uart_programmed": false, "firmware": strings.TrimSpace(*firmware)}
	uartPort, warning, err := resolveInitializationUART(*uart, store.Current().Connection)
	if err != nil {
		return err
	}
	if warning != "" {
		fmt.Fprintln(output, "WARNING:", warning)
	}

	needsInitialize := *forceInitialize
	if uartPort != "" && !needsInitialize {
		fmt.Fprintf(output, "[detect] Authenticating existing application on %s before considering ISP initialization\n", uartPort)
		openContext, cancel := context.WithTimeout(ctx, 12*time.Second)
		err := runtime.Open(openContext, uartPort)
		cancel()
		if err == nil {
			nameContext, nameCancel := context.WithTimeout(ctx, 4*time.Second)
			identity, nameErr := runtime.BoardName(nameContext)
			nameCancel()
			if nameErr != nil {
				return fmt.Errorf("read authenticated board identity: %w", nameErr)
			}
			result["existing_application"] = true
			result["board_name"] = identity
			fmt.Fprintf(output, "Existing PCController firmware authenticated: board=%q persisted=%t.\n", identity.Name, identity.Persisted)
			if strings.TrimSpace(*firmware) == "" {
				if *boardName != "" && *boardName != identity.Name {
					nameContext, nameCancel := context.WithTimeout(ctx, 5*time.Second)
					updated, nameErr := runtime.SetBoardName(nameContext, *boardName)
					nameCancel()
					if nameErr != nil {
						return fmt.Errorf("persist board name: %w", nameErr)
					}
					result["board_name"] = updated
				}
				fmt.Fprintln(output, "READY: healthy application retained; ISP initialization and firmware upload skipped.")
				return appendBoardInitializationReport(output, result, *jsonReport)
			}
		} else {
			fmt.Fprintln(output, "Existing application did not authenticate; checking the UART bootloader before considering ISP initialization.")
			_ = runtime.Close()
			// A valid Urboot answers even when the application is absent or broken.
			// In that case an explicit firmware image can repair the application
			// without needlessly touching ISP fuses, EEPROM, or the bootloader.
			if strings.TrimSpace(*firmware) != "" {
				project := configuredProject(store.Current(), fallbackProject)
				cli, cliConfig, toolchainErr := resolveProvisionToolchain(ctx, store, *skipToolchain, *portableCLI, *toolchainCLI, *fqbn, project, output)
				if toolchainErr == nil {
					probeErr := programmer.Execute(ctx, programmer.Options{
						Method: programmer.MethodUrclock, Operation: programmer.OperationProbe,
						Port: uartPort, FQBN: *fqbn, ArduinoCLI: cli, ArduinoConfig: cliConfig,
						Avrdude: store.Current().Programming.Avrdude, AvrdudeConf: store.Current().Programming.AvrdudeConf,
					}, output)
					if probeErr == nil {
						result["bootloader_authenticated"] = true
						fmt.Fprintln(output, "Verified UART bootloader is usable; retaining ISP state and uploading the requested firmware.")
					} else {
						needsInitialize = true
						fmt.Fprintln(output, "UART bootloader did not respond; provisioning will initialize the core before upload.")
					}
				} else {
					needsInitialize = true
					fmt.Fprintln(output, "Toolchain could not prepare a UART bootloader probe; provisioning will initialize the core before upload.")
				}
			} else {
				needsInitialize = true
			}
		}
	} else if !needsInitialize {
		fmt.Fprintln(output, "No application UART is available; provisioning will initialize the core before any requested upload.")
		needsInitialize = true
	}

	if needsInitialize {
		initializeArgs := make([]string, 0, 8)
		if *skipToolchain {
			initializeArgs = append(initializeArgs, "--skip-toolchain")
		}
		if *portableCLI {
			initializeArgs = append(initializeArgs, "--portable-cli")
		}
		if *toolchainCLI != "" {
			initializeArgs = append(initializeArgs, "--cli", *toolchainCLI)
		}
		if *fqbn != "" {
			initializeArgs = append(initializeArgs, "--fqbn", *fqbn)
		}
		fmt.Fprintln(output, "[initialize] Running explicit/required ISP fuse and bootloader initialization.")
		if err := initializeBoard(ctx, runtime, initializeArgs, store, fallbackProject, output); err != nil {
			return err
		}
		result["initialized"] = true
	}

	if strings.TrimSpace(*firmware) == "" {
		if needsInitialize {
			fmt.Fprintln(output, "READY: initialization complete; no application image was requested.")
			return appendBoardInitializationReport(output, result, *jsonReport)
		}
		return errors.New("no authenticated application, no firmware supplied, and initialization was not requested")
	}
	if _, err := programmer.LoadIntelHex(*firmware); err != nil {
		return fmt.Errorf("inspect requested firmware: %w", err)
	}
	if uartPort == "" {
		return errors.New("firmware was supplied but no UART bootloader port is available; initialize completed without upload")
	}
	_ = runtime.Close()
	project := configuredProject(store.Current(), fallbackProject)
	cli, cliConfig, err := resolveProvisionToolchain(ctx, store, *skipToolchain, *portableCLI, *toolchainCLI, *fqbn, project, output)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "[upload] Writing requested firmware through %s with mandatory readback\n", uartPort)
	if err := programmer.Execute(ctx, programmer.Options{
		Method: programmer.MethodUrclock, Operation: programmer.OperationWriteFlash,
		Port: uartPort, HexPath: *firmware, FQBN: *fqbn, ArduinoCLI: cli, ArduinoConfig: cliConfig,
		Avrdude: store.Current().Programming.Avrdude, AvrdudeConf: store.Current().Programming.AvrdudeConf,
	}, output); err != nil {
		return fmt.Errorf("upload requested firmware: %w", err)
	}
	result["uart_programmed"] = true
	_ = runtime.Close()
	openContext, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = runtime.Open(openContext, uartPort)
	cancel()
	if err != nil {
		return fmt.Errorf("authenticate uploaded application: %w", err)
	}
	if *boardName != "" {
		nameContext, nameCancel := context.WithTimeout(ctx, 5*time.Second)
		confirmed, nameErr := runtime.SetBoardName(nameContext, *boardName)
		nameCancel()
		if nameErr != nil {
			return fmt.Errorf("persist board name: %w", nameErr)
		}
		result["board_name"] = confirmed
	}
	fmt.Fprintln(output, "READY: requested firmware uploaded, read back, and application HELLO authenticated.")
	return appendBoardInitializationReport(output, result, *jsonReport)
}

func resolveProvisionToolchain(ctx context.Context, store *appconfig.Store, skip, portable bool, requestedCLI, fqbn, project string, output io.Writer) (string, string, error) {
	cli, config := strings.TrimSpace(requestedCLI), strings.TrimSpace(store.Current().Programming.ToolchainConfig)
	if cli == "" && !portable {
		cli = strings.TrimSpace(store.Current().Programming.ToolchainCLI)
	}
	if portable {
		config = ""
	}
	if skip {
		if cli == "" {
			return "", "", errors.New("--skip-toolchain requires a configured or explicit --cli executable")
		}
		return cli, config, nil
	}
	report, err := programmer.BootstrapToolchain(ctx, programmer.ToolchainBootstrapOptions{Profile: programmer.DefaultToolchainProfile(), CLI: cli, DirectRetry: true}, output)
	if err != nil {
		return "", "", fmt.Errorf("prepare firmware toolchain: %w", err)
	}
	if _, err := store.Update(func(value *appconfig.Config) error {
		value.Programming.ToolchainCLI, value.Programming.ToolchainConfig = report.CLIPath, report.ConfigPath
		value.Programming.FQBN, value.Paths.Project = fqbn, project
		return nil
	}); err != nil {
		return "", "", err
	}
	return report.CLIPath, report.ConfigPath, nil
}

func findCompiledApplicationHex(directory string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(directory, "*.hex"))
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, match := range matches {
		name := strings.ToLower(filepath.Base(match))
		if strings.Contains(name, "with_bootloader") || strings.HasSuffix(name, ".eep.hex") {
			continue
		}
		candidates = append(candidates, match)
	}
	sort.Strings(candidates)
	if len(candidates) != 1 {
		return "", fmt.Errorf("compiled application output in %s has %d candidate HEX files", directory, len(candidates))
	}
	return candidates[0], nil
}

func resolveInitializationUART(requested string, connection appconfig.Connection) (string, string, error) {
	requested = strings.TrimSpace(requested)
	if strings.EqualFold(requested, "none") || strings.EqualFold(requested, "off") {
		return "", "UART was disabled; bootloader installed and serial checks skipped", nil
	}
	if requested != "" && !strings.EqualFold(requested, "auto") {
		return requested, "", nil
	}
	all, err := ports.List()
	if err != nil {
		return "", "", err
	}
	candidates := ports.Candidates(all, ports.Filter{
		Port: connection.Port, VID: connection.VID, PID: connection.PID, Name: connection.Name,
	})
	if len(candidates) == 0 {
		return "", "no matching UART adapter is connected; serial programming and first-boot checks were skipped", nil
	}
	if len(candidates) > 1 {
		return "", "", &ports.AmbiguousError{Candidates: candidates}
	}
	return candidates[0].Name, "", nil
}

func firstBootHealthChecks(ctx context.Context, runtime *control.Runtime, output io.Writer) (map[string]any, error) {
	statusContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	status, err := runtime.RefreshStatus(statusContext)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("read first-boot status: %w", err)
	}
	snapshot := runtime.Snapshot()
	fmt.Fprintf(output, "HELLO: build=%08X schema=%d capabilities=%08X profile=%s(%d) build_features=%02X\n", snapshot.Hello.BuildHash, snapshot.Hello.IdentitySchema, snapshot.Hello.Capabilities, native.FeatureProfileName(snapshot.Hello.FeatureProfile), snapshot.Hello.FeatureProfile, snapshot.Hello.BuildFeatures)
	fmt.Fprintf(output, "STATUS: uptime=%s reset_cause=%d reset_count=%d framing=%d crc=%d\n", status.ReadableUptime(), status.ResetCause, status.ResetCount, status.FramingErrors, status.CRCErrors)

	settingsPersisted := false
	for attempt := 0; attempt < 20; attempt++ {
		requestContext, requestCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		frame, requestErr := runtime.Request(requestContext, native.OpGetSettings, nil, native.OpSettings)
		requestCancel()
		if requestErr == nil {
			settings, parseErr := native.ParseSettings(frame.Payload)
			if parseErr == nil && settings.Persisted {
				settingsPersisted = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !settingsPersisted {
		return nil, errors.New("first-boot host-owned settings were not persisted and verified")
	}
	fmt.Fprintln(output, "SETTINGS: host-owned factory defaults persisted and read back")

	i2cContext, i2cCancel := context.WithTimeout(ctx, 15*time.Second)
	addresses, i2cErr := control.ScanI2C(i2cContext, runtime)
	i2cCancel()
	if i2cErr != nil {
		return nil, fmt.Errorf("scan available I2C peripherals: %w", i2cErr)
	}
	addressText := make([]string, len(addresses))
	for index, address := range addresses {
		addressText[index] = fmt.Sprintf("0x%02X", address)
	}
	fmt.Fprintln(output, "I2C:", strings.Join(addressText, ", "))
	for address, label := range map[byte]string{0x40: "INA219", 0x41: "PCA9685"} {
		if !containsByte(addresses, address) {
			fmt.Fprintf(output, "WARNING: optional %s at 0x%02X is not connected; initialization continues\n", label, address)
		}
	}
	if status.LCDAddress == 0 {
		fmt.Fprintln(output, "WARNING: optional LCD at 0x27/0x3F is not connected; initialization continues")
	}

	temperatureContext, temperatureCancel := context.WithTimeout(ctx, 5*time.Second)
	frame, temperatureErr := runtime.Request(temperatureContext, native.OpTemperatureList, []byte{1}, native.OpTemperatures)
	temperatureCancel()
	if temperatureErr != nil {
		return nil, fmt.Errorf("scan optional DS18B20 bus: %w", temperatureErr)
	}
	temperatures, err := native.ParseTemperatures(frame.Payload)
	if err != nil {
		return nil, err
	}
	if len(temperatures) == 0 {
		fmt.Fprintln(output, "WARNING: optional DS18B20 sensors are not connected; initialization continues")
	} else {
		fmt.Fprintf(output, "DS18B20: %d sensor(s) discovered\n", len(temperatures))
	}
	return map[string]any{
		"hello_build_hash":    fmt.Sprintf("%08X", snapshot.Hello.BuildHash),
		"identity_schema":     snapshot.Hello.IdentitySchema,
		"feature_profile":     native.FeatureProfileName(snapshot.Hello.FeatureProfile),
		"feature_profile_id":  snapshot.Hello.FeatureProfile,
		"build_features":      fmt.Sprintf("%02X", snapshot.Hello.BuildFeatures),
		"settings_persisted":  settingsPersisted,
		"i2c_addresses":       addressText,
		"temperature_sensors": len(temperatures),
		"pwm_available":       status.PWMAvailable,
		"lcd_address":         fmt.Sprintf("0x%02X", status.LCDAddress),
		"framing_errors":      status.FramingErrors, "crc_errors": status.CRCErrors,
	}, nil
}

func containsByte(values []byte, wanted byte) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func appendBoardInitializationReport(output io.Writer, report map[string]any, enabled bool) error {
	if !enabled {
		return nil
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(output, string(encoded))
	return nil
}
