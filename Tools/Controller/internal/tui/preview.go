package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/shell"
)

// RichPreviewSnapshot is deterministic representative device state for UI
// development. It is injected directly and never opens, scans, or resets a
// serial port.
func RichPreviewSnapshot() control.Snapshot {
	now := time.Now()
	return control.Snapshot{
		Connected: true,
		Port: ports.Info{
			Name: "VIRTUAL", VID: "1A86", PID: "7523",
			FriendlyName: productidentity.DefaultAppTitle() + " Virtual Board (COM18 profile)",
		},
		Hello: native.Hello{
			Name: "PCController", BoardKind: native.BoardKindPCController,
			Capabilities: native.CapabilityINA219 | native.CapabilityTemperatures | native.CapabilityPWM |
				native.CapabilityRelayMotion | native.CapabilitySegments | native.CapabilityLCD |
				native.CapabilityPersistentSettings | native.CapabilityMenuRemote | native.CapabilityBluetoothAudio |
				native.CapabilityRemoteKeys | native.CapabilityFrontPanelSnapshot | native.CapabilityMenuDirectory | native.CapabilityRFLearnReplace |
				native.CapabilityHostFrontPanel | native.CapabilityBuzzerBusy | native.CapabilityMenuLayout | native.CapabilityTimedMacroQueue |
				native.CapabilityStatusEffects | native.CapabilityProgramState,
			IdentitySchema: native.IdentitySchemaCompact, BuildHash: 0x5DF10D05,
			BuildTimestamp: 0x3501645C, BuildStamp: "260801123456",
		},
		Status: native.Status{
			UptimeMS: 4_392_210, SupplyMV: 12_224, BusMV: 12_198,
			CurrentMA: 286, PowerMW: 3492, TLEDCenti: 2812, TBTCenti: 2637,
			ActiveKeys: 0x01, ActiveRelays: 0x50, MenuPage: 0,
			ProgramMode: 1, DoorOpen: true, BluetoothState: 2,
			PWMAvailable: true, PWMChannel: 3, PWMValue: 2816, LCDAddress: 0x27,
			Flags: native.StatusINA219Available | native.StatusPWMAvailable |
				native.StatusTLEDAvailable | native.StatusTBTAvailable,
			INA219Available: true, TLEDAvailable: true, TBTAvailable: true,
			ResetCause: 0, ResetCount: 2075,
		},
		Settings: native.Settings{
			Flags: 0, LightMode: 2,
			OnBrightness: 224, OffBrightness: 8, DisplayBrightness: 5,
			DisplayClosedBrightness: 0, MotionExitHoldSeconds: 2,
			StatusBrightness: 160, OutputPersistence: native.OutputPersistUserPWM,
			StreamPeriodMS: 200, DefaultPage: 0, ExtendedFlags: 0xF0,
			MotionBreakMSValue: 1,
		},
		HaveStatus: true, HaveSettings: true, StatusUpdated: now,
		ConnectionState: "connected", ConnectionUpdated: now,
	}
}

// RichPreviewModel seeds graphs and important events as well as live state.
func RichPreviewModel(welcome bool) Model {
	engine := shell.New(100)
	model := NewPreview(engine, RichPreviewSnapshot(), welcome)
	model.integrations = func() hostui.IntegrationStatus {
		return hostui.IntegrationStatus{
			Hotkeys:       hostui.HotkeyStatus{Supported: true, Running: true, Bindings: []hostui.HotkeyBinding{{Name: "attention", Accelerator: "Ctrl+Alt+P", Command: "rgb effect play attention"}}},
			Notifications: hostui.NotificationStatus{Supported: true, Available: true, Accepted: 7},
			Messaging:     hostui.ServiceStatus{Name: "Messages", Enabled: true, State: "connected", Endpoint: "IPC + WebSocket"},
			Discovery:     hostui.ServiceStatus{Name: "Discovery", Enabled: true, State: "watching", Detail: "Win32 device notifications"},
			Webhooks:      hostui.ServiceStatus{Name: "Webhooks", Enabled: true, State: "ready", Endpoint: "2 routes"},
			SocketIO:      hostui.ServiceStatus{Name: "Socket.IO", Enabled: false, State: "disabled", Endpoint: "optional adapter"},
		}
	}
	base := time.Date(2026, time.August, 1, 12, 30, 0, 0, time.Local)
	for index := 0; index < 72; index++ {
		model.samples = append(model.samples, measurementSample{
			At:       base.Add(time.Duration(index) * 4 * time.Second),
			SupplyMV: 12220 + int32(index%5), BusMV: 12195 + int32(index%7),
			CurrentMA: 270 + int32((index*7)%24), PowerMW: 3310 + int32((index*31)%230),
			TLEDCenti: 2680 + int16(index*2), TBTCenti: 2630 + int16(index%5),
			HaveSupply: true, HaveBus: true, HaveCurrent: true, HavePower: true,
			HaveTLED: true, HaveTBT: true,
		})
	}
	model.timeline = []timelineEntry{
		{base.Add(10 * time.Second), "connection", "connected VIRTUAL", true},
		{base.Add(40 * time.Second), "door", "enclosure door opened", true},
		{base.Add(70 * time.Second), "rf", "RF down code=1381717 bits=24 protocol=1 learned-id=0", true},
		{base.Add(72 * time.Second), "relay", "R5 switched on by RF mapping", true},
		{base.Add(110 * time.Second), "bluetooth", "BT Audio disconnected / pairing", true},
	}
	model.previewMacros = []appconfig.Macro{
		{
			ID: 1, Name: "output-demo", Category: "Demo", Color: "violet", Label: "dEMO",
			LCDMessage: "Output demo", TimingToleranceUS: 2500,
			Steps: []appconfig.MacroStep{
				{AtUS: 0, Kind: "relay", Target: 5, Value: 1},
				{AtUS: 500_000, Kind: "pwm", Target: 0, Value: 1024},
				{AtUS: 1_250_000, Kind: "display", Text: "PLAY"},
				{AtUS: 2_000_000, Kind: "buzzer", Value: 880},
				{AtUS: 3_000_000, Kind: "relay", Target: 5, Value: 0},
			},
		},
		{
			ID: 2, Name: "door-notify", Category: "Safety", Color: "red", Label: "door",
			LCDMessage: "Door warning", TimingToleranceUS: 1500,
			Steps: []appconfig.MacroStep{
				{AtUS: 0, Kind: "rgb", Value: 1},
				{AtUS: 250_000, Kind: "buzzer", Value: 1200},
			},
		},
		{ID: 3, Name: "draft-scene", Category: "Lighting", Color: "blue", Label: "drAF"},
	}
	macroOptions := make([]appconfig.HostMenuOption, 0, len(model.previewMacros))
	for _, macro := range model.previewMacros {
		label := fmt.Sprintf("%d %s", macro.ID, macro.Name)
		if len(label) > 16 {
			label = label[:16]
		}
		macroOptions = append(macroOptions, appconfig.HostMenuOption{Label: label, Value: fmt.Sprint(macro.ID)})
	}
	if model.hostMenus != nil {
		_ = model.hostMenus.UpdateSelectOptions("macro.library", macroOptions)
	}
	model.previewMacroState = control.MacroState{
		Running: true, ID: 1, Name: "output-demo", Category: "Demo", Color: "violet",
		Step: 2, StepCount: 5, DurationUS: 3_000_000,
		StartedAt:     time.Now().Add(-1250 * time.Millisecond),
		AcceptedBytes: 95, BufferFill: 42, LastTimingDeltaUS: 267,
		MaximumTimingErrorUS: 979, TimingToleranceUS: 2500,
		Lifecycle: "playing",
	}
	return model
}

// PreviewFrame returns a stable ANSI render for screenshot and golden tests.
func PreviewFrame(page Page, width, height int) string {
	model := RichPreviewModel(false)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model = updated.(Model)
	model.page = page
	return model.View()
}

func ParsePage(value string) (Page, error) {
	for index, definition := range pageDefinitions {
		if value == definition.Key || equalFold(value, definition.Short) || equalFold(value, definition.Title) {
			return Page(index), nil
		}
	}
	return 0, fmt.Errorf("unknown TUI page %q", value)
}

func equalFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		a, b := left[index], right[index]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
