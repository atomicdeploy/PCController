package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
)

type connectionFlags struct {
	device           string
	port             string
	vid              string
	pid              string
	name             string
	baud             int
	startupWait      time.Duration
	requestTimeout   time.Duration
	helloAttempts    int
	resetOnReconnect bool
	overrides        map[string]bool
	preferred        ports.Identity
}

func addConnectionFlags(
	flags *flag.FlagSet,
	config appconfig.Connection,
) *connectionFlags {
	options := &connectionFlags{overrides: make(map[string]bool)}
	if config.LastDevice != nil {
		options.preferred = ports.Identity{
			Port: config.LastDevice.Port,
			VID:  config.LastDevice.VID, PID: config.LastDevice.PID,
			SerialNumber: config.LastDevice.SerialNumber,
			Name:         config.LastDevice.Name,
			InstanceID:   config.LastDevice.InstanceID,
		}
	}
	flags.StringVar(
		&options.device,
		"device",
		envOr("PCCONTROLLER_DEVICE", ""),
		"COM ID, friendly name, VID:PID, serial:VALUE, or instance:VALUE",
	)
	flags.StringVar(&options.port, "port", envOr("PCCONTROLLER_PORT", config.Port), "explicit serial port or tcp://host:port")
	flags.StringVar(&options.vid, "vid", envOr("PCCONTROLLER_VID", config.VID), "USB VID filter")
	flags.StringVar(&options.pid, "pid", envOr("PCCONTROLLER_PID", config.PID), "USB PID filter")
	flags.StringVar(&options.name, "name", envOr("PCCONTROLLER_NAME", config.Name), "port/product/manufacturer substring")
	flags.IntVar(&options.baud, "baud", envInt("PCCONTROLLER_BAUD", config.BaudRate), "UART baud rate")
	flags.DurationVar(
		&options.startupWait,
		"startup-wait",
		time.Duration(config.StartupWaitMS)*time.Millisecond,
		"wait after opening before HELLO",
	)
	flags.DurationVar(
		&options.requestTimeout,
		"request-timeout",
		time.Duration(config.RequestTimeoutMS)*time.Millisecond,
		"timeout for each HELLO attempt",
	)
	flags.IntVar(
		&options.helloAttempts,
		"hello-attempts",
		config.HelloAttempts,
		"HELLO attempts while Urboot starts",
	)
	flags.BoolVar(
		&options.resetOnReconnect,
		"reset-on-reconnect",
		config.ResetOnReconnect,
		"pulse DTR once when a disconnected USB board reappears",
	)
	return options
}

func (options *connectionFlags) captureOverrides(flags *flag.FlagSet) {
	flags.Visit(func(value *flag.Flag) {
		options.overrides[value.Name] = true
	})
}

func (options connectionFlags) filter() ports.Filter {
	filter := ports.Filter{
		Port: options.port, VID: options.vid, PID: options.pid, Name: options.name,
		Preferred: options.preferred,
	}
	if options.device != "" {
		selector, _ := ports.ParseSelector(options.device)
		selector.Preferred = options.preferred
		filter = selector
	}
	if options.overrides["device"] || options.overrides["port"] ||
		options.overrides["vid"] || options.overrides["pid"] ||
		options.overrides["name"] {
		filter.Preferred = ports.Identity{}
	}
	return filter
}

func runtimeOptions(options *connectionFlags) control.Options {
	return control.Options{
		Filter:           options.filter(),
		BaudRate:         options.baud,
		StartupWait:      options.startupWait,
		RequestTimeout:   options.requestTimeout,
		HelloAttempts:    options.helloAttempts,
		ResetOnReconnect: options.resetOnReconnect,
	}
}

func newRuntime(
	connection *connectionFlags,
	store *appconfig.Store,
) *control.Runtime {
	runtime := control.New(runtimeOptions(connection))
	if err := configureRuntimeHistory(runtime, store); err != nil {
		runtime.PublishHostEvent("error", "history configuration: "+err.Error())
	}
	if err := configureRuntimeLCD(runtime, store.Current().UI); err != nil {
		runtime.PublishHostEvent("error", "LCD presentation configuration: "+err.Error())
	}
	return runtime
}

func configureRuntimeLCD(runtime *control.Runtime, ui appconfig.UI) error {
	return runtime.LCDPresenter().Configure(control.LCDPresentationOptions{
		Enabled:      ui.LCDServiceEnabled,
		Debounce:     time.Duration(ui.LCDPromptDebounceMS) * time.Millisecond,
		PriorityHold: time.Duration(ui.LCDPriorityHoldMS) * time.Millisecond,
	})
}

func configureRuntimeHistory(
	runtime *control.Runtime,
	store *appconfig.Store,
) error {
	return runtime.ConfigureHistory(configuredHistoryOptions(store))
}

func configuredHistoryOptions(store *appconfig.Store) control.HistoryOptions {
	config := store.Current()
	dataDirectory := filepath.Dir(store.Path())
	path := strings.TrimSpace(config.Paths.HistoryFile)
	if path == "" {
		path = filepath.Join(dataDirectory, "timeline.jsonl")
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(dataDirectory, path)
	}
	return control.HistoryOptions{
		Retention:      time.Duration(config.UI.HistoryHours) * time.Hour,
		SampleInterval: time.Duration(config.UI.HistorySampleMS) * time.Millisecond,
		TimelineLimit:  config.UI.EventLogLimit,
		TimelinePath:   path,
		StatusPath:     filepath.Join(dataDirectory, "measurements.jsonl"),
	}
}

func (options *connectionFlags) fromConfig(
	config appconfig.Connection,
) control.Options {
	port, vid, pid, name := config.Port, config.VID, config.PID, config.Name
	baud := config.BaudRate
	startupWait := time.Duration(config.StartupWaitMS) * time.Millisecond
	requestTimeout := time.Duration(config.RequestTimeoutMS) * time.Millisecond
	helloAttempts := config.HelloAttempts
	resetOnReconnect := config.ResetOnReconnect
	if options.overrides["port"] {
		port = options.port
	}
	if options.overrides["vid"] {
		vid = options.vid
	}
	if options.overrides["pid"] {
		pid = options.pid
	}
	if options.overrides["name"] {
		name = options.name
	}
	filter := ports.Filter{Port: port, VID: vid, PID: pid, Name: name}
	if config.LastDevice != nil {
		filter.Preferred = ports.Identity{
			Port: config.LastDevice.Port,
			VID:  config.LastDevice.VID, PID: config.LastDevice.PID,
			SerialNumber: config.LastDevice.SerialNumber,
			Name:         config.LastDevice.Name,
			InstanceID:   config.LastDevice.InstanceID,
		}
	}
	if options.overrides["device"] && options.device != "" {
		selector, _ := ports.ParseSelector(options.device)
		filter = selector
	}
	if options.overrides["device"] || options.overrides["port"] ||
		options.overrides["vid"] || options.overrides["pid"] ||
		options.overrides["name"] {
		filter.Preferred = ports.Identity{}
	}
	if options.overrides["baud"] {
		baud = options.baud
	}
	if options.overrides["startup-wait"] {
		startupWait = options.startupWait
	}
	if options.overrides["request-timeout"] {
		requestTimeout = options.requestTimeout
	}
	if options.overrides["hello-attempts"] {
		helloAttempts = options.helloAttempts
	}
	if options.overrides["reset-on-reconnect"] {
		resetOnReconnect = options.resetOnReconnect
	}
	return control.Options{
		Filter:   filter,
		BaudRate: baud, StartupWait: startupWait,
		RequestTimeout: requestTimeout, HelloAttempts: helloAttempts,
		ResetOnReconnect: resetOnReconnect,
	}
}

func commandOptions(store *appconfig.Store, fallbackProject string) control.CommandOptions {
	config := store.Current()
	options := control.CommandOptions{
		ProjectPath:   configuredProject(config, fallbackProject),
		FQBN:          configuredFQBN(config),
		ArduinoCLI:    config.Programming.ToolchainCLI,
		ArduinoConfig: config.Programming.ToolchainConfig,
		Avrdude:       config.Programming.Avrdude,
		AvrdudeConf:   config.Programming.AvrdudeConf,
		Programmer:    configuredProgrammer(config),
		HostConfig:    store.Current,
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			_, err := store.Update(change)
			return err
		},
		Macros: func() []appconfig.Macro {
			return store.Current().Macros
		},
	}
	options.InitializeBoard = func(
		ctx context.Context,
		runtime *control.Runtime,
		args []string,
		output io.Writer,
	) error {
		return initializeBoard(ctx, runtime, args, store, fallbackProject, output)
	}
	options.BlankBoard = func(
		ctx context.Context,
		runtime *control.Runtime,
		args []string,
		output io.Writer,
	) error {
		return blankBoard(ctx, runtime, args, store, output)
	}
	options.USBaspDriver = func(_ context.Context, args []string, output io.Writer) error {
		return runDriver(args, output, output)
	}
	options.Resolve = func() control.CommandOptions {
		current := store.Current()
		return control.CommandOptions{
			ProjectPath:      configuredProject(current, fallbackProject),
			FQBN:             configuredFQBN(current),
			ArduinoCLI:       current.Programming.ToolchainCLI,
			ArduinoConfig:    current.Programming.ToolchainConfig,
			Avrdude:          current.Programming.Avrdude,
			AvrdudeConf:      current.Programming.AvrdudeConf,
			Programmer:       configuredProgrammer(current),
			HostConfig:       store.Current,
			UpdateHostConfig: options.UpdateHostConfig,
			Macros:           options.Macros,
			InitializeBoard:  options.InitializeBoard,
			BlankBoard:       options.BlankBoard,
			USBaspDriver:     options.USBaspDriver,
		}
	}
	return options
}

func configuredProgrammer(config appconfig.Config) string {
	if config.Programming.Programmer != "" {
		return config.Programming.Programmer
	}
	return "usbasp"
}

func configuredProject(config appconfig.Config, fallback string) string {
	if config.Paths.Project != "" {
		return config.Paths.Project
	}
	return fallback
}

func configuredFQBN(config appconfig.Config) string {
	if config.Programming.FQBN != "" {
		return config.Programming.FQBN
	}
	return programmer.DefaultFQBN()
}

func apiMacros(source []appconfig.Macro) []controllerapi.Macro {
	result := make([]controllerapi.Macro, len(source))
	for index, macro := range source {
		result[index] = controllerapi.Macro{
			ID: macro.ID, Name: macro.Name, Category: macro.Category,
			Color: macro.Color, Label: macro.Label, LCDMessage: macro.LCDMessage,
			TimingToleranceUS:   macro.TimingToleranceUS,
			KeepOutputsOnCancel: macro.KeepOutputsOnCancel,
			Steps:               make([]controllerapi.MacroStep, len(macro.Steps)),
		}
		for stepIndex, step := range macro.Steps {
			result[index].Steps[stepIndex] = controllerapi.MacroStep{
				AtUS: step.AtUS, Kind: step.Kind,
				Target: step.Target, Value: step.Value,
				DurationMS: step.DurationMS, FrequencyHz: step.FrequencyHz,
				Text: step.Text, Destination: step.Destination,
				Code: step.Code, Bits: step.Bits, Protocol: step.Protocol,
				PulseUS: step.PulseUS, Red: step.Red, Green: step.Green,
				Blue: step.Blue, Brightness: step.Brightness,
				Opcode: step.Opcode, PayloadHex: step.PayloadHex,
			}
		}
	}
	return result
}

func apiAutomations(source []appconfig.Automation) []controllerapi.Automation {
	result := make([]controllerapi.Automation, len(source))
	for index, automation := range source {
		result[index] = controllerapi.Automation{
			Name: automation.Name, Enabled: automation.Enabled,
			CooldownMS: automation.CooldownMS,
			Match: controllerapi.AutomationMatch{
				Kind: automation.Match.Kind, Lifecycle: automation.Match.Lifecycle,
				State: automation.Match.State, Contains: automation.Match.Contains,
				Key: automation.Match.Key, Gesture: automation.Match.Gesture,
				Source: automation.Match.Source, RFID: automation.Match.RFID,
				RFCode:     automation.Match.RFCode,
				RFProtocol: automation.Match.RFProtocol,
			},
			Actions: make([]controllerapi.AutomationAction, len(automation.Actions)),
		}
		for actionIndex, action := range automation.Actions {
			result[index].Actions[actionIndex] = controllerapi.AutomationAction{
				Type: action.Type, Command: action.Command, Macro: action.Macro,
				Executable: action.Executable,
				Args:       append([]string(nil), action.Args...),
				Script:     action.Script,
				Event:      action.Event,
				VirtualKey: action.VirtualKey,
				HoldMS:     action.HoldMS,
				Power:      action.Power,
				Confirm:    action.Confirm,
			}
			if action.RF != nil {
				result[index].Actions[actionIndex].RF = &controllerapi.RFTransmit{
					Code: action.RF.Code, Bits: action.RF.Bits,
					Protocol: action.RF.Protocol, PulseUS: action.RF.PulseUS,
					Repeats: action.RF.Repeats,
				}
			}
		}
	}
	return result
}

func apiOptions(
	config appconfig.Config,
	connection *connectionFlags,
) controllerapi.Options {
	resolved := connection.fromConfig(config.Connection)
	return controllerapi.Options{
		Port: resolved.Filter.Port, VID: resolved.Filter.VID,
		PID: resolved.Filter.PID, Name: resolved.Filter.Name,
		BaudRate: resolved.BaudRate, StartupWait: resolved.StartupWait,
		RequestTimeout:   resolved.RequestTimeout,
		HelloAttempts:    resolved.HelloAttempts,
		ResetOnReconnect: resolved.ResetOnReconnect,
		PreferredDevice:  publicPreferredDevice(config.Connection.LastDevice),
		ProjectPath:      configuredProject(config, findProjectRoot()),
		FQBN:             configuredFQBN(config), Macros: apiMacros(config.Macros),
		Melodies:         config.Melodies,
		StatusEffects:    config.StatusEffects,
		ToolchainCLI:     config.Programming.ToolchainCLI,
		Avrdude:          config.Programming.Avrdude,
		AvrdudeConf:      config.Programming.AvrdudeConf,
		Programmer:       configuredProgrammer(config),
		MotionDoorPolicy: config.Safety.MotionDoorPolicy,
		RF:               config.RF,
		OSActions:        config.OSActions,
		LCDPresentation: controllerapi.LCDPresentationOptions{
			Enabled:      config.UI.LCDServiceEnabled,
			Debounce:     time.Duration(config.UI.LCDPromptDebounceMS) * time.Millisecond,
			PriorityHold: time.Duration(config.UI.LCDPriorityHoldMS) * time.Millisecond,
		},
		Scripts:     config.Scripts,
		Automations: apiAutomations(config.Automations),
	}
}

func publicPreferredDevice(
	identity *appconfig.DeviceIdentity,
) *controllerapi.PortInfo {
	if identity == nil {
		return nil
	}
	return &controllerapi.PortInfo{
		Name: identity.Port, VID: identity.VID, PID: identity.PID,
		SerialNumber: identity.SerialNumber,
		FriendlyName: identity.Name,
		InstanceID:   identity.InstanceID,
	}
}

func watchConfiguration(
	ctx context.Context,
	store *appconfig.Store,
	runtime *control.Runtime,
	connection *connectionFlags,
) {
	store.Watch(
		ctx,
		appconfig.DefaultWatchInterval,
		func(value appconfig.Config) {
			runtime.ApplyOptions(connection.fromConfig(value.Connection))
			if err := configureRuntimeLCD(runtime, value.UI); err != nil {
				runtime.PublishHostEvent(
					"error",
					"LCD presentation configuration rejected: "+err.Error(),
				)
			}
			if err := configureRuntimeHistory(runtime, store); err != nil {
				runtime.PublishHostEvent(
					"error",
					"history configuration rejected: "+err.Error(),
				)
			}
			runtime.PublishHostEvent(
				"config",
				"reloaded "+store.Path()+" (PC-side settings only)",
			)
		},
		func(err error) {
			runtime.PublishHostEvent(
				"error",
				"configuration reload rejected; retaining last good values: "+err.Error(),
			)
		},
	)
}

func bindRuntimeDevicePersistence(
	runtime *control.Runtime,
	store *appconfig.Store,
) {
	runtime.SetDeviceObserver(func(info ports.Info, _ native.Hello) {
		_, err := store.RememberDevice(appconfig.DeviceIdentity{
			Port: info.Name, VID: info.VID, PID: info.PID,
			SerialNumber: info.SerialNumber,
			Name:         firstDeviceName(info.FriendlyName, info.Product),
			InstanceID:   info.InstanceID,
			LastSeen:     time.Now(),
		})
		if err != nil {
			runtime.PublishHostEvent(
				"error",
				"persist last successful device: "+err.Error(),
			)
		}
	})
}

func bindClientDevicePersistence(
	client *controllerapi.Client,
	store *appconfig.Store,
) {
	client.SetDeviceObserver(func(info controllerapi.PortInfo, _ controllerapi.Hello) {
		_, _ = store.RememberDevice(appconfig.DeviceIdentity{
			Port: info.Name, VID: info.VID, PID: info.PID,
			SerialNumber: info.SerialNumber,
			Name:         firstDeviceName(info.FriendlyName, info.Product),
			InstanceID:   info.InstanceID,
			LastSeen:     time.Now(),
		})
	})
}

func firstDeviceName(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type presentationOverrides struct {
	AppName string
	Tagline string
}

func extractGlobalArguments(args []string) ([]string, string, presentationOverrides, error) {
	result := make([]string, 0, len(args))
	var path string
	overrides := presentationOverrides{
		AppName: strings.TrimSpace(os.Getenv("APP_NAME")),
		Tagline: strings.TrimSpace(os.Getenv("APP_TAGLINE")),
	}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--config" {
			if index+1 >= len(args) {
				return nil, "", presentationOverrides{}, errors.New("--config requires a JSON file path")
			}
			path = args[index+1]
			index++
			continue
		}
		if strings.HasPrefix(argument, "--config=") {
			path = strings.TrimPrefix(argument, "--config=")
			if path == "" {
				return nil, "", presentationOverrides{}, errors.New("--config requires a JSON file path")
			}
			continue
		}
		name, inline, recognized := globalPresentationFlag(argument)
		if recognized {
			value := inline
			if value == "" && !strings.Contains(argument, "=") {
				if index+1 >= len(args) {
					return nil, "", presentationOverrides{}, fmt.Errorf("--%s requires a value", name)
				}
				value = args[index+1]
				index++
			}
			value = strings.TrimSpace(value)
			if value == "" {
				return nil, "", presentationOverrides{}, fmt.Errorf("--%s requires a non-empty value", name)
			}
			if name == "app-name" {
				overrides.AppName = value
			} else {
				overrides.Tagline = value
			}
			continue
		}
		result = append(result, argument)
	}
	return result, path, overrides, nil
}

func globalPresentationFlag(argument string) (name, value string, recognized bool) {
	for _, candidate := range []string{"app-name", "tagline", "app-tagline"} {
		flagName := "--" + candidate
		if argument == flagName {
			if candidate == "app-tagline" {
				candidate = "tagline"
			}
			return candidate, "", true
		}
		if strings.HasPrefix(argument, flagName+"=") {
			if candidate == "app-tagline" {
				candidate = "tagline"
			}
			return candidate, strings.TrimPrefix(argument, flagName+"="), true
		}
	}
	return "", "", false
}
