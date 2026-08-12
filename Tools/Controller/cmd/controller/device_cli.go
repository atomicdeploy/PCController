package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostbridge"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/script"
	"pccontroller.local/controller/internal/webui"
)

func runPorts(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("ports", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	if err := flags.Parse(args); err != nil {
		return err
	}
	list, err := ports.List()
	if err != nil {
		return err
	}
	list = ports.Candidates(list, connection.filter())
	if len(list) == 0 {
		fmt.Fprintln(stdout, "No matching serial ports.")
		return nil
	}
	for _, port := range list {
		fmt.Fprintln(stdout, port.Label())
	}
	return nil
}

func runShell(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("shell", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	noConnect := flags.Bool("no-connect", false, "start without auto-opening a device")
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)
	claim, havePrimary, err := preparePrimaryMode("shell")
	if err != nil {
		return err
	}
	if havePrimary {
		return runSecondaryConsole(os.Stdin, stdout, stderr, store.Current().UI.AppTitle)
	}
	defer func() {
		if claim != nil {
			_ = claim.Close()
		}
	}()
	if err := selectInteractiveDevice(
		connection,
		os.Stdin,
		stderr,
	); err != nil {
		return err
	}
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	watchContext, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	go watchConfiguration(watchContext, store, runtime, connection)
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	primary, err := startPrimaryIPCClaimed(watchContext, runtime, engine, store, claim)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		return runSecondaryConsole(os.Stdin, stdout, stderr, store.Current().UI.AppTitle)
	}
	if err != nil {
		return err
	}
	claim = nil
	defer primary.Close()
	go control.RunAutomations(watchContext, runtime, engine, store.Current)
	if *noConnect {
		_ = runtime.Close()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := runtime.EnsureConnected(ctx)
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "auto-connect:", err)
		}
	}
	fmt.Fprintln(stdout, productidentity.ServiceName(store.Current().UI.AppTitle, "shell.")+" Type help; Ctrl+Z then Enter exits on Windows.")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 64*1024)
	for {
		fmt.Fprint(stdout, "pc> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if strings.EqualFold(line, "exit") || strings.EqualFold(line, "quit") {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		output, err := engine.Execute(ctx, line)
		cancel()
		if output != "" {
			fmt.Fprintln(stdout, output)
		}
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
		}
	}
	return scanner.Err()
}

func runExec(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)
	if flags.NArg() == 0 {
		return errors.New("exec requires a controller shell command")
	}
	commandText := joinControllerCommand(flags.Args())
	return runExecCommand(connection, commandText, stdout, store)
}

// runExecCommand owns the one local/primary routing path used by typed CLI
// commands as well as `exec`.  Keeping this below the argument parsers means
// connection selection, capability discovery, and command safety cannot drift
// between surfaces.
func runExecCommand(connection *connectionFlags, commandText string, stdout io.Writer, store *appconfig.Store) error {
	claim, havePrimary, err := preparePrimaryMode("exec")
	if err != nil {
		return err
	}
	if havePrimary {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Minute,
		)
		defer cancel()
		output, err := executeThroughPrimary(ctx, commandText)
		if output != "" {
			fmt.Fprintln(stdout, output)
		}
		return err
	}
	defer func() {
		if claim != nil {
			_ = claim.Close()
		}
	}()
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	ownerContext, stopOwner := context.WithCancel(context.Background())
	defer stopOwner()
	primary, err := startPrimaryIPCClaimed(ownerContext, runtime, engine, store, claim)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Minute,
		)
		defer cancel()
		output, routeErr := executeThroughPrimary(ctx, commandText)
		if output != "" {
			fmt.Fprintln(stdout, output)
		}
		return routeErr
	}
	if err != nil {
		return err
	}
	claim = nil
	defer primary.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if !commandAllowsDisconnected(commandText) {
		if err := runtime.EnsureConnected(ctx); err != nil {
			cancel()
			return err
		}
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	output, err := engine.Execute(ctx, commandText)
	if output != "" {
		fmt.Fprintln(stdout, output)
	}
	return err
}

func commandAllowsDisconnected(command string) bool {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(command)))
	return len(words) >= 2 && words[0] == "board" && words[1] == "initialize"
}

func runBatch(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("batch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	fileName := flags.String("file", "-", "script file, or - for standard input")
	continueOnError := flags.Bool("continue", false, "continue after command errors")
	jsonOutput := flags.Bool("json", false, "emit one JSON result object per command")
	noConnect := flags.Bool("no-connect", false, "do not connect before running")
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)

	var input io.Reader = os.Stdin
	var file *os.File
	var err error
	if *fileName != "-" {
		file, err = os.Open(*fileName)
		if err != nil {
			return err
		}
		defer file.Close()
		input = file
	}
	scriptOptions := script.Options{
		ContinueOnError: *continueOnError,
		OnResult: func(result script.Result) {
			if *jsonOutput {
				_ = json.NewEncoder(stdout).Encode(result)
				return
			}
			fmt.Fprintf(stdout, "[line %d] > %s\n", result.Line, result.Command)
			if result.Output != "" {
				fmt.Fprintln(stdout, result.Output)
			}
			if result.Error != "" {
				fmt.Fprintln(stderr, "error:", result.Error)
			}
		},
	}
	ctx, cancel := signalContext()
	defer cancel()
	claim, havePrimary, err := preparePrimaryMode("batch")
	if err != nil {
		return err
	}
	if havePrimary {
		return script.Run(ctx, input, primaryExecutor{}, scriptOptions)
	}
	defer func() {
		if claim != nil {
			_ = claim.Close()
		}
	}()

	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	watchContext, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	go watchConfiguration(watchContext, store, runtime, connection)
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	primary, err := startPrimaryIPCClaimed(watchContext, runtime, engine, store, claim)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		return script.Run(ctx, input, primaryExecutor{}, scriptOptions)
	}
	if err != nil {
		return err
	}
	claim = nil
	defer primary.Close()
	go control.RunAutomations(watchContext, runtime, engine, store.Current)
	if !*noConnect {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = runtime.EnsureConnected(ctx)
		cancel()
		if err != nil {
			return err
		}
	} else {
		_ = runtime.Close()
	}
	return script.Run(ctx, input, engine, scriptOptions)
}

func runMonitor(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("monitor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	period := flags.Duration(
		"interval",
		time.Duration(store.Current().UI.StatusIntervalMS)*time.Millisecond,
		"status refresh interval",
	)
	count := flags.Int("count", 0, "number of samples; zero runs until interrupted")
	jsonOutput := flags.Bool("json", false, "emit newline-delimited JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)
	if *period < 100*time.Millisecond {
		return errors.New("monitor interval must be at least 100ms")
	}
	if *count < 0 {
		return errors.New("monitor count cannot be negative")
	}
	ctx, cancel := signalContext()
	defer cancel()
	claim, havePrimary, err := preparePrimaryMode("monitor")
	if err != nil {
		return err
	}
	if havePrimary {
		return runRemoteMonitor(
			ctx,
			*period,
			*count,
			*jsonOutput,
			stdout,
			stderr,
		)
	}
	defer func() {
		if claim != nil {
			_ = claim.Close()
		}
	}()
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	go watchConfiguration(ctx, store, runtime, connection)
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	primary, err := startPrimaryIPCClaimed(ctx, runtime, engine, store, claim)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		return runRemoteMonitor(
			ctx,
			*period,
			*count,
			*jsonOutput,
			stdout,
			stderr,
		)
	}
	if err != nil {
		return err
	}
	claim = nil
	defer primary.Close()
	go control.RunAutomations(ctx, runtime, engine, store.Current)
	if err := connectWithTimeout(ctx, runtime); err != nil {
		return err
	}
	ticker := time.NewTicker(*period)
	defer ticker.Stop()
	samples := 0
	refresh := func() error {
		requestContext, requestCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		status, err := runtime.RefreshStatus(requestContext)
		requestCancel()
		if err != nil {
			return err
		}
		if *jsonOutput {
			record := struct {
				Type   string        `json:"type"`
				Time   time.Time     `json:"time"`
				Port   string        `json:"port"`
				Status native.Status `json:"status"`
			}{
				Type: "status", Time: time.Now(),
				Port: runtime.Snapshot().Port.Name, Status: status,
			}
			return json.NewEncoder(stdout).Encode(record)
		}
		fmt.Fprintf(
			stdout,
			"%s supply=%7.3fV bus=%7.3fV current=%6dmA power=%6dmW "+
				"tLED=%6.2fC tBT=%6.2fC keys=%X relays=%02X door=%t BT=%d "+
				"PWM=%d:%d reset=%d/0x%02X\n",
			time.Now().Format("15:04:05.000"),
			float64(status.SupplyMV)/1000,
			float64(status.BusMV)/1000,
			status.CurrentMA,
			status.PowerMW,
			float64(status.TLEDCenti)/100,
			float64(status.TBTCenti)/100,
			status.ActiveKeys,
			status.ActiveRelays,
			status.DoorOpen,
			status.BluetoothState,
			status.PWMChannel,
			status.PWMValue,
			status.ResetCount,
			status.ResetCause,
		)
		return nil
	}
	emitEvent := func(event control.Event) error {
		if event.Kind == "telemetry" {
			return nil
		}
		if *jsonOutput {
			var device *native.DeviceEvent
			gesture := event.Gesture
			source := event.Source
			if event.Frame.Opcode == native.OpEvent {
				if parsed, err := native.ParseDeviceEvent(event.Frame.Payload); err == nil {
					device = &parsed
					if parsed.Type == native.EventKey {
						gesture = control.NormalizeGesture(parsed.Gesture)
						source = map[byte]string{
							native.InputSourcePhysical: "physical",
							native.InputSourceRF:       "rf",
							native.InputSourceHost:     "host",
						}[parsed.Source]
					}
				}
			}
			record := struct {
				Type       string              `json:"type"`
				ID         uint64              `json:"id"`
				Time       time.Time           `json:"time"`
				Kind       string              `json:"kind"`
				Text       string              `json:"text"`
				Opcode     byte                `json:"opcode,omitempty"`
				Payload    []byte              `json:"payload,omitempty"`
				Lifecycle  string              `json:"lifecycle,omitempty"`
				Port       ports.Info          `json:"port,omitempty"`
				Reason     string              `json:"reason,omitempty"`
				State      string              `json:"state,omitempty"`
				Device     *native.DeviceEvent `json:"device,omitempty"`
				Gesture    string              `json:"gesture,omitempty"`
				Source     string              `json:"source,omitempty"`
				RFCode     uint32              `json:"rf_code,omitempty"`
				RFBits     byte                `json:"rf_bits,omitempty"`
				RFProtocol byte                `json:"rf_protocol,omitempty"`
				RFPulseUS  uint16              `json:"rf_pulse_us,omitempty"`
				ResetCause byte                `json:"reset_cause,omitempty"`
				ResetCount uint32              `json:"reset_count,omitempty"`
			}{
				Type: "event", ID: event.ID, Time: event.Time,
				Kind: event.Kind, Text: event.Text,
				Opcode: event.Frame.Opcode, Payload: event.Frame.Payload,
				Lifecycle: event.Lifecycle, Port: event.Port,
				Reason: event.Reason, State: event.State,
				Device: device, Gesture: gesture, Source: source,
				RFCode: event.RFCode, RFBits: event.RFBits,
				RFProtocol: event.RFProtocol, RFPulseUS: event.RFPulseUS,
				ResetCause: event.ResetCause, ResetCount: event.ResetCount,
			}
			return json.NewEncoder(stdout).Encode(record)
		}
		fmt.Fprintf(
			stdout,
			"%s EVENT %-10s %s\n",
			event.Time.Format("15:04:05.000"),
			event.Kind,
			event.Text,
		)
		return nil
	}
	if err := refresh(); err != nil {
		fmt.Fprintln(stderr, "monitor:", err)
	} else {
		samples++
	}
	for {
		if *count != 0 && samples >= *count {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case event := <-runtime.Events():
			if err := emitEvent(event); err != nil {
				return err
			}
		case <-ticker.C:
			if err := refresh(); err != nil {
				fmt.Fprintln(stderr, "monitor:", err)
				if !runtime.Snapshot().Connected {
					runtime.ResumeAuto()
					if reconnectErr := connectWithTimeout(ctx, runtime); reconnectErr != nil {
						continue
					}
				}
			} else {
				samples++
			}
		}
	}
}

func connectWithTimeout(ctx context.Context, runtime *control.Runtime) error {
	connectContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return runtime.EnsureConnected(connectContext)
}

func runIPC(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	if len(args) == 0 {
		return errors.New("usage: ipc serve|call")
	}
	switch strings.ToLower(args[0]) {
	case "serve":
		flags := flag.NewFlagSet("ipc serve", flag.ContinueOnError)
		flags.SetOutput(stderr)
		connection := addConnectionFlags(flags, store.Current().Connection)
		listen := flags.String(
			"listen",
			store.Current().IPC.Listen,
			"loopback TCP listen address for NDJSON and WebSocket",
		)
		websocketPath := flags.String(
			"ws-path",
			store.Current().IPC.WebSocketPath,
			"WebSocket endpoint path on the same IPC port",
		)
		stdio := flags.Bool("stdio", false, "serve newline-delimited JSON-RPC on stdin/stdout")
		noConnect := flags.Bool("no-connect", false, "start without connecting to the board")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		serverConfig, runtimeConfigErr := store.Runtime()
		if runtimeConfigErr != nil {
			return runtimeConfigErr
		}
		serverConfig.IPC.Listen = *listen
		serverConfig.IPC.WebSocketPath = *websocketPath
		if err := serverConfig.Validate(); err != nil {
			return fmt.Errorf("IPC server configuration: %w", err)
		}
		connection.captureOverrides(flags)
		var listener net.Listener
		if *stdio {
			probeContext, probeCancel := context.WithTimeout(
				context.Background(),
				400*time.Millisecond,
			)
			havePrimary := primaryAvailable(probeContext)
			probeCancel()
			if havePrimary {
				return errPrimaryAlreadyRunning
			}
		} else {
			var err error
			listener, err = ipcjson.ListenWithRemote(
				*listen,
				serverConfig.IPC.AllowRemote,
			)
			if err != nil {
				probeContext, probeCancel := context.WithTimeout(
					context.Background(),
					400*time.Millisecond,
				)
				havePrimary := primaryAvailableAt(probeContext, *listen)
				probeCancel()
				if havePrimary {
					return errPrimaryAlreadyRunning
				}
				return err
			}
			defer listener.Close()
		}
		currentConfig := store.Current()
		client := controllerapi.New(apiOptions(currentConfig, connection))
		if err := client.ConfigureHistory(configuredHistoryOptions(store)); err != nil {
			return err
		}
		bindClientDevicePersistence(client, store)
		defer client.Shutdown()
		sessionSnapshot := newHostSessionRecorder(client, store)
		defer sessionSnapshot.logOnExit(stderr)
		ctx, cancel := signalContext()
		defer cancel()
		go store.Watch(
			ctx,
			appconfig.DefaultWatchInterval,
			func(value appconfig.Config) {
				client.ApplyHostOptions(apiOptions(value, connection))
				if historyErr := client.ConfigureHistory(
					configuredHistoryOptions(store),
				); historyErr != nil {
					fmt.Fprintln(stderr, "history configuration rejected:", historyErr)
				}
				fmt.Fprintln(stderr, "configuration reloaded:", store.Path())
			},
			func(err error) {
				fmt.Fprintln(stderr, "configuration reload rejected:", err)
			},
		)
		if !*noConnect {
			connectContext, connectCancel := context.WithTimeout(ctx, 15*time.Second)
			err := client.Connect(connectContext)
			connectCancel()
			if err != nil {
				fmt.Fprintln(stderr, "initial auto-connect:", err)
			}
		}
		integrationProxy, err := newIntegrationProxy(store)
		if err != nil {
			return fmt.Errorf("configure local integration proxy: %w", err)
		}
		localDevice := startLocalDeviceHost(ctx, client, store)
		defer localDevice.Close()
		actions := hostui.NewActionBroker()
		integrations, err := hostbridge.Start(ctx, client, store, actions)
		if err != nil {
			return fmt.Errorf("start headless host integrations: %w", err)
		}
		defer integrations.Close()
		service := &ipcjson.Service{
			Client: client, WebSocketPath: *websocketPath,
			SocketIOPath:        serverConfig.IPC.SocketIOPath,
			WebUI:               webui.Handler(*websocketPath),
			IntegrationProxy:    integrationProxy,
			LocalDevice:         localDevice,
			AuthToken:           serverConfig.IPC.AuthToken,
			AllowedOrigins:      append([]string(nil), serverConfig.IPC.AllowedOrigins...),
			InboundWebhooks:     serverConfig.Integrations.InboundWebhooksEnabled,
			HostVersion:         version,
			HostSourceHash:      sourceHash,
			HostBuildTime:       buildTime,
			AppAction:           actions.Publish,
			Shutdown:            cancel,
			LastSessionSnapshot: sessionSnapshot.read,
			HostConfig:          store.CurrentRuntime,
			UpdateHostConfig: func(change func(*appconfig.Config) error) error {
				_, err := store.Update(change)
				return err
			},
			BridgeList: func() any { return integrations.BridgePeers() },
			BridgeCall: integrations.CallBridge,
			WebhookAdmin: func() ipcjson.WebhookAdminService {
				return integrations
			},
			HotkeyStatus: func() any { return integrations.HotkeyStatus() },
		}
		artifactService, err := newArtifactHostService(client, store, service.Shutdown)
		if err != nil {
			return fmt.Errorf("configure artifact/update service: %w", err)
		}
		defer artifactService.Close()
		service.Artifacts = artifactService
		sessionSnapshot.attachArtifactContext(artifactService)
		releaseDiscovery, err := newReleaseHostService(client, artifactService)
		if err != nil {
			return fmt.Errorf("configure release discovery: %w", err)
		}
		defer releaseDiscovery.Close()
		service.ReleaseDiscovery = releaseDiscovery
		if *stdio {
			return ipcjson.ServeStreams(ctx, os.Stdin, stdout, service)
		}
		fmt.Fprintf(
			stdout,
			"JSON-RPC IPC listening on %s (WebSocket ws://%s%s)\n",
			listener.Addr(),
			listener.Addr(),
			*websocketPath,
		)
		return ipcjson.Serve(ctx, listener, service)

	case "call":
		flags := flag.NewFlagSet("ipc call", flag.ContinueOnError)
		flags.SetOutput(stderr)
		address := flags.String("addr", store.Current().IPC.Listen, "IPC server address")
		token := flags.String("token", store.CurrentRuntime().IPC.AuthToken, "IPC bearer token")
		tokenReference := flags.String("token-ref", "", "resolve the IPC bearer token from an OS-vault or environment reference")
		method := flags.String("method", "", "JSON-RPC method")
		params := flags.String("params", "{}", "JSON object with method parameters")
		timeout := flags.Duration("timeout", 15*time.Second, "call timeout")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *method == "" && flags.NArg() != 0 {
			*method = flags.Arg(0)
		}
		if *method == "" {
			return errors.New("ipc call requires --method")
		}
		if strings.TrimSpace(*tokenReference) != "" {
			tokenWasSet := false
			flags.Visit(func(value *flag.Flag) {
				tokenWasSet = tokenWasSet || value.Name == "token"
			})
			if tokenWasSet {
				return errors.New("--token and --token-ref are mutually exclusive")
			}
			resolved, err := store.ResolveSecret(*tokenReference)
			if err != nil {
				return fmt.Errorf("resolve IPC bearer token: %w", err)
			}
			*token = resolved
		}
		rawParams := json.RawMessage(*params)
		if !json.Valid(rawParams) {
			return errors.New("--params must be valid JSON")
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		response, err := ipcjson.Call(ctx, *address, ipcjson.Request{
			Method: *method,
			Params: rawParams,
			Auth:   *token,
		})
		if err != nil {
			return err
		}
		formatted, err := json.MarshalIndent(response.Result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(formatted))
		return nil
	default:
		return fmt.Errorf("unknown ipc command %q", args[0])
	}
}

func runReset(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("reset", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	duration := flags.Duration("pulse", 120*time.Millisecond, "DTR/RTS low pulse duration")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *duration < time.Millisecond {
		return errors.New("pulse duration must be at least 1ms")
	}
	claim, havePrimary, err := preparePrimaryMode("reset")
	if err != nil {
		return err
	}
	if havePrimary {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := callPrimary(
			ctx,
			"controller.reset.lines",
			map[string]int{"pulse_ms": int(duration.Milliseconds())},
			nil,
		); err != nil {
			return err
		}
		fmt.Fprintln(
			stdout,
			"DTR/RTS reset complete over IPC; application HELLO reauthenticated.",
		)
		return nil
	}
	defer func() {
		if claim != nil {
			_ = claim.Close()
		}
	}()
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	ownerContext, stopOwner := context.WithCancel(context.Background())
	defer stopOwner()
	primary, err := startPrimaryIPCClaimed(ownerContext, runtime, engine, store, claim)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if routeErr := callPrimary(
			ctx,
			"controller.reset.lines",
			map[string]int{"pulse_ms": int(duration.Milliseconds())},
			nil,
		); routeErr != nil {
			return routeErr
		}
		fmt.Fprintln(
			stdout,
			"DTR/RTS reset complete over IPC; application HELLO reauthenticated.",
		)
		return nil
	}
	if err != nil {
		return err
	}
	claim = nil
	defer primary.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := runtime.EnsureConnected(ctx); err != nil {
		return err
	}
	if err := runtime.PulseResetFor(ctx, *duration); err != nil {
		return err
	}
	if err := runtime.Reconnect(ctx, "DTR/RTS reset pulse completed"); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "DTR/RTS reset complete; application HELLO reauthenticated.")
	return nil
}
