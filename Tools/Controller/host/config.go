package host

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/programmer"
)

func controllerOptionsFromConfig(
	config appconfig.Config,
	configPath string,
) (controller.Options, error) {
	features := programmer.FirmwareFeatureNames(config.Programming.FirmwareFeatures)
	project := strings.TrimSpace(config.Paths.Project)
	if project != "" && !filepath.IsAbs(project) {
		project = filepath.Join(filepath.Dir(configPath), project)
	}
	options := controller.Options{
		Port:                  config.Connection.Port,
		VID:                   config.Connection.VID,
		PID:                   config.Connection.PID,
		Name:                  config.Connection.Name,
		BaudRate:              config.Connection.BaudRate,
		StartupWait:           time.Duration(config.Connection.StartupWaitMS) * time.Millisecond,
		RequestTimeout:        time.Duration(config.Connection.RequestTimeoutMS) * time.Millisecond,
		HelloAttempts:         config.Connection.HelloAttempts,
		ResetOnReconnect:      config.Connection.ResetOnReconnect,
		ReconnectInitialDelay: time.Duration(config.Connection.ReconnectInitialMS) * time.Millisecond,
		ReconnectMaximumDelay: time.Duration(config.Connection.ReconnectMaximumMS) * time.Millisecond,
		ProjectPath:           project,
		FQBN:                  config.Programming.FQBN,
		FirmwareFeatures:      features,
		ToolchainCLI:          config.Programming.ToolchainCLI,
		ToolchainConfig:       config.Programming.ToolchainConfig,
		Avrdude:               config.Programming.Avrdude,
		AvrdudeConf:           config.Programming.AvrdudeConf,
		Programmer:            config.Programming.Programmer,
		Macros:                publicMacros(config.Macros),
		Melodies:              append([]controller.Melody(nil), config.Melodies...),
		StatusEffects:         append([]controller.StatusLEDEffect(nil), config.StatusEffects...),
		Scripts:               cloneStrings(config.Scripts),
		Automations:           publicAutomations(config.Automations),
		MotionDoorPolicy:      config.Safety.MotionDoorPolicy,
		LCDPresentation: controller.LCDPresentationOptions{
			Enabled:      config.UI.LCDServiceEnabled,
			Debounce:     time.Duration(config.UI.LCDPromptDebounceMS) * time.Millisecond,
			PriorityHold: time.Duration(config.UI.LCDPriorityHoldMS) * time.Millisecond,
		},
		RF:        config.RF,
		OSActions: config.OSActions,
	}
	if config.Connection.LastDevice != nil {
		options.PreferredDevice = &controller.PortInfo{
			Name:         config.Connection.LastDevice.Port,
			VID:          config.Connection.LastDevice.VID,
			PID:          config.Connection.LastDevice.PID,
			SerialNumber: config.Connection.LastDevice.SerialNumber,
			FriendlyName: config.Connection.LastDevice.Name,
			InstanceID:   config.Connection.LastDevice.InstanceID,
		}
	}
	return options, nil
}

func configureHistory(
	client *controller.Client,
	config appconfig.Config,
	configPath string,
) error {
	root := filepath.Dir(configPath)
	timeline := strings.TrimSpace(config.Paths.HistoryFile)
	if timeline == "" {
		timeline = filepath.Join(root, "timeline.jsonl")
	} else if !filepath.IsAbs(timeline) {
		timeline = filepath.Join(root, timeline)
	}
	if err := client.ConfigureHistory(controller.HistoryOptions{
		Retention:      time.Duration(config.UI.HistoryHours) * time.Hour,
		SampleInterval: time.Duration(config.UI.HistorySampleMS) * time.Millisecond,
		TimelineLimit:  config.UI.EventLogLimit,
		TimelinePath:   timeline,
		StatusPath:     filepath.Join(root, "measurements.jsonl"),
	}); err != nil {
		return fmt.Errorf("configure PCController history: %w", err)
	}
	return nil
}

func publicMacros(source []appconfig.Macro) []controller.Macro {
	result := make([]controller.Macro, len(source))
	for index, macro := range source {
		result[index] = controller.Macro{
			ID: macro.ID, Name: macro.Name, Mode: macro.Mode,
			Category: macro.Category, Color: macro.Color, Label: macro.Label,
			LCDMessage:          macro.LCDMessage,
			TimingToleranceUS:   macro.TimingToleranceUS,
			KeepOutputsOnCancel: macro.KeepOutputsOnCancel,
			Steps:               make([]controller.MacroStep, len(macro.Steps)),
		}
		for stepIndex, step := range macro.Steps {
			result[index].Steps[stepIndex] = controller.MacroStep{
				AtUS: step.AtUS, Kind: step.Kind, Target: step.Target,
				Value: step.Value, DurationMS: step.DurationMS, ToValue: step.ToValue,
				Easing: step.Easing, SampleRateHz: step.SampleRateHz,
				RepeatCount: step.RepeatCount, RepeatIntervalMS: step.RepeatIntervalMS,
				FrequencyHz: step.FrequencyHz, Text: step.Text,
				Destination: step.Destination, Code: step.Code, Bits: step.Bits,
				Protocol: step.Protocol, PulseUS: step.PulseUS,
				Red: step.Red, Green: step.Green, Blue: step.Blue,
				Brightness: step.Brightness, ToRed: step.ToRed, ToGreen: step.ToGreen,
				ToBlue: step.ToBlue, ToBrightness: step.ToBrightness, Opcode: step.Opcode,
				PayloadHex: step.PayloadHex,
			}
		}
	}
	return result
}

func publicAutomations(source []appconfig.Automation) []controller.Automation {
	result := make([]controller.Automation, len(source))
	for index, automation := range source {
		result[index] = controller.Automation{
			Name: automation.Name, Enabled: automation.Enabled,
			CooldownMS: automation.CooldownMS,
			Match: controller.AutomationMatch{
				Kind: automation.Match.Kind, Lifecycle: automation.Match.Lifecycle,
				State: automation.Match.State, Contains: automation.Match.Contains,
				Key: automation.Match.Key, Gesture: automation.Match.Gesture,
				Source: automation.Match.Source, RFID: cloneByte(automation.Match.RFID),
				RFCode:     cloneUint32(automation.Match.RFCode),
				RFProtocol: automation.Match.RFProtocol,
			},
			Actions: make([]controller.AutomationAction, len(automation.Actions)),
		}
		for actionIndex, action := range automation.Actions {
			result[index].Actions[actionIndex] = controller.AutomationAction{
				Type: action.Type, Command: action.Command, Macro: action.Macro,
				Executable: action.Executable, Args: append([]string(nil), action.Args...),
				Script: action.Script, Event: action.Event,
				VirtualKey: action.VirtualKey, HoldMS: action.HoldMS,
				Power: action.Power, Confirm: action.Confirm,
			}
			if action.RF != nil {
				result[index].Actions[actionIndex].RF = &controller.RFTransmit{
					Code: action.RF.Code, Bits: action.RF.Bits,
					Protocol: action.RF.Protocol, PulseUS: action.RF.PulseUS,
					Repeats: action.RF.Repeats,
				}
			}
		}
	}
	return result
}

func cloneStrings(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneByte(value *byte) *byte {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneUint32(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
