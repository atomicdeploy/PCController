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

const boardInitializeUsage = "usage: controller board provision [--name NAME] [--uart auto|PORT|none] [--firmware HEX] [--bootloader-only] [--skip-toolchain] [--portable-cli] | controller board blank --confirm NAME [--uart auto|PORT|none] | controller board name [get|set NAME|clear]"

func runBoard(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	if len(args) == 0 {
		return errors.New(boardInitializeUsage)
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	if action == "initialize" {
		action = "provision"
	}
	if action != "provision" && action != "blank" {
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
	return initializeBoard(ctx, runtime, args[1:], store, findProjectRoot(), stdout)
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
	flags := flag.NewFlagSet("board provision", flag.ContinueOnError)
	flags.SetOutput(output)
	uart := flags.String("uart", "auto", "application UART port, auto, or none")
	boardName := flags.String("name", "", "persist an operator board name of at most eight printable ASCII characters")
	firmware := flags.String("firmware", store.Current().Paths.FirmwareHex, "precompiled application Intel HEX (compile project when empty)")
	bootloaderOnly := flags.Bool("bootloader-only", false, "install and verify only the core bootloader/fuses")
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
	if err := native.ValidateBoardName(*boardName); err != nil {
		return err
	}
	if *boardName != "" && (*bootloaderOnly || strings.EqualFold(strings.TrimSpace(*uart), "none")) {
		return errors.New("--name requires the UART application phase; it cannot be combined with --bootloader-only or --uart none")
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

	fmt.Fprintln(output, "=== PCController blank-board provision ===")
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

	applicationHex := strings.TrimSpace(*firmware)
	if !*bootloaderOnly && applicationHex == "" {
		fmt.Fprintln(output, "\n[firmware] Compiling the current project before touching the MCU")
		compileOptions, identity, planErr := programmer.PlanCompile(programmer.Options{
			Method: programmer.MethodCompile, SketchPath: project,
			ArduinoCLI: cli, ArduinoConfig: cliConfig, FQBN: *fqbn,
		})
		if planErr != nil {
			return planErr
		}
		if err := programmer.Execute(ctx, compileOptions, output); err != nil {
			return fmt.Errorf("compile initialization firmware: %w", err)
		}
		applicationHex, err = findCompiledApplicationHex(identity.OutputDir)
		if err != nil {
			return err
		}
	}
	if applicationHex != "" {
		if _, err := programmer.LoadIntelHex(applicationHex); err != nil {
			return fmt.Errorf("inspect initialization firmware: %w", err)
		}
		fmt.Fprintln(output, "Application image:", applicationHex)
	}

	// No authenticated application is expected yet, but close any stale UART
	// session before ISP takes ownership of RESET and the target clock.
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

	result := map[string]any{
		"core": coreReport, "application_hex": applicationHex,
		"uart_requested": *uart, "uart_programmed": false,
		"board_name_requested": *boardName,
	}
	applicationPhases := make([]programmer.BoardLifecyclePhase, 0, 3)
	applicationPhase := func(name string, run func() error) error {
		started := time.Now()
		err := run()
		applicationPhases = append(applicationPhases, programmer.BoardLifecyclePhase{
			Name: name, DurationMS: time.Since(started).Milliseconds(),
		})
		return err
	}
	if *bootloaderOnly {
		fmt.Fprintln(output, "\nREADY (bootloader-only): UART application programming and first-boot checks were explicitly skipped.")
		return appendBoardInitializationReport(output, result, *jsonReport)
	}
	uartPort, warning, err := resolveInitializationUART(*uart, store.Current().Connection)
	if err != nil {
		return err
	}
	if warning != "" {
		fmt.Fprintln(output, "\nWARNING:", warning)
	}
	if uartPort == "" {
		fmt.Fprintln(output, "READY (bootloader-only): connect UART later and rerun board provision --skip-toolchain, or program from the TUI Programming page.")
		result["warning"] = warning
		if *boardName != "" {
			return fmt.Errorf("bootloader installed, but board name %q could not be stored because UART is unavailable", *boardName)
		}
		return appendBoardInitializationReport(output, result, *jsonReport)
	}

	fmt.Fprintf(output, "\n[uart] Programming application through %s with mandatory Urclock readback\n", uartPort)
	if err := applicationPhase("uart_application_write", func() error {
		return programmer.Execute(ctx, programmer.Options{
			Method: programmer.MethodUrclock, Operation: programmer.OperationWriteFlash,
			Port: uartPort, HexPath: applicationHex, FQBN: *fqbn,
			ArduinoCLI: cli, ArduinoConfig: cliConfig, Avrdude: store.Current().Programming.Avrdude,
			AvrdudeConf: store.Current().Programming.AvrdudeConf,
		}, output)
	}); err != nil {
		return fmt.Errorf("program application through UART bootloader: %w", err)
	}
	result["uart_port"] = uartPort
	result["uart_programmed"] = true

	fmt.Fprintln(output, "\n[first boot] Authenticating application HELLO and provisioning missing host-owned defaults")
	connectContext, connectCancel := context.WithTimeout(ctx, 20*time.Second)
	err = applicationPhase("first_hello", func() error { return runtime.Open(connectContext, uartPort) })
	connectCancel()
	if err != nil {
		return fmt.Errorf("authenticate first application boot on %s: %w", uartPort, err)
	}
	if *boardName != "" {
		nameContext, nameCancel := context.WithTimeout(ctx, 5*time.Second)
		confirmed, nameErr := runtime.SetBoardName(nameContext, *boardName)
		nameCancel()
		if nameErr != nil {
			return fmt.Errorf("persist board name: %w", nameErr)
		}
		result["board_name"] = confirmed
		fmt.Fprintf(output, "BOARD NAME: %q persisted and read back\n", confirmed.Name)
	}
	var health map[string]any
	err = applicationPhase("first_boot_health", func() error {
		var healthErr error
		health, healthErr = firstBootHealthChecks(ctx, runtime, output)
		return healthErr
	})
	if err != nil {
		return err
	}
	result["health"] = health
	result["application_phases"] = applicationPhases
	fmt.Fprintln(output, "\nREADY: bootloader, application, UART authentication, defaults, and available peripherals verified.")
	return appendBoardInitializationReport(output, result, *jsonReport)
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
