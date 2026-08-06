package native

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

const (
	MaxRFProtocol            byte   = 12
	MaxRFLearnSeconds        byte   = 120
	MinimumStreamPeriodMS    uint16 = 100
	MinimumRelayTestPeriodMS uint16 = 250
)

const StatusProfileCount byte = 19

const (
	DisplaySegments byte = iota
	DisplayLCD
	DisplayBoth
	DisplayHostPanel
	DisplayReleaseHostPanel
	DisplayScheduledSegments
)

const (
	SegmentRepeatOnce byte = iota
	SegmentRepeatLoop
	SegmentRepeatInterval
)

const SegmentForceScroll byte = 0x80

const (
	StatusEffectNone byte = iota
	StatusEffectBreathe
	StatusEffectFlash
	StatusEffectCycle
	StatusEffectTransition
)

type StatusEffectOptions struct {
	Kind              byte
	Red               byte
	Green             byte
	Blue              byte
	AlternateRed      byte
	AlternateGreen    byte
	AlternateBlue     byte
	Brightness        byte
	MinimumBrightness byte
	PeriodMS          uint16
	Repeats           byte
}

// ScheduledSegmentOptions is the compact host-to-MCU presentation contract.
// Interval is encoded in whole seconds so the full 40-byte text limit still
// fits the native protocol's 48-byte maximum payload.
type ScheduledSegmentOptions struct {
	SpeedMS        uint16
	HoldMS         uint16
	IntervalSecond byte
	Repeat         byte
	ForceScroll    bool
}

const (
	MacroRelay byte = iota
	MacroPWM
	MacroRelaysAllOff
	MacroUserPWMAllOff
	MacroFinish
)

const (
	AddressableLEDFill     byte = 0xFF
	AddressableLEDMaxPixel byte = 10
)

const (
	SettingsSilent byte = 1 << iota
	// SettingsProgrammingMode is the durable programming safety latch. While
	// set, firmware keeps motion, relays, PWM, and illumination inactive.
	SettingsProgrammingMode
	SettingsSwapTemperatureRoles
	SettingsMotionDoorPolicy0
	SettingsMotionDoorPolicy1
	SettingsDoorAudioDisabled
	SettingsRelayAudioDisabled
)

const SettingsMotionDoorPolicyMask = SettingsMotionDoorPolicy0 | SettingsMotionDoorPolicy1

const (
	MotionDoorAlways byte = iota
	MotionDoorClosedOnly
	MotionDoorOpenOnly
	MotionDoorNever
)

const (
	KeyEventClick byte = iota
	KeyEventDoubleClick
	KeyEventHoldStart
	KeyEventHoldRepeat
	KeyEventHoldRelease
	KeyEventDown
	KeyEventUp
)

// RemoteKeyGesturePayload injects one canonical front-panel key lifecycle.
// Down performs the initial action immediately; HoldRepeat repeats it and
// Click/HoldStart are classification-only. Use OpMenuAction for a stateless
// one-shot action.
func RemoteKeyGesturePayload(action, event byte) ([]byte, error) {
	if action > MenuIncrease {
		return nil, fmt.Errorf("remote key action %d is outside 0..3", action)
	}
	if event > KeyEventUp {
		return nil, fmt.Errorf("remote key event %d is outside 0..6", event)
	}
	return []byte{action, event}, nil
}

func U16(value uint16) []byte {
	return []byte{byte(value), byte(value >> 8)}
}

func StreamPeriodPayload(periodMS uint16) ([]byte, error) {
	if periodMS != 0 && periodMS < MinimumStreamPeriodMS {
		return nil, fmt.Errorf(
			"stream period must be 0 or at least %d ms",
			MinimumStreamPeriodMS,
		)
	}
	return U16(periodMS), nil
}

// ProgramStatePayload encodes the semantic prefix accepted by opcode 0x45.
// Keeping this one byte avoids coupling the MCU to host owner/reason metadata.
func ProgramStatePayload(running bool) []byte {
	if running {
		return []byte{ProgramStateRunning}
	}
	return []byte{ProgramStateIdle}
}

func RelayTestPayload(periodMS uint16) ([]byte, error) {
	if periodMS != 0 && periodMS < MinimumRelayTestPeriodMS {
		return nil, fmt.Errorf(
			"relay test period must be 0 or at least %d ms",
			MinimumRelayTestPeriodMS,
		)
	}
	return U16(periodMS), nil
}

func DisplayTextPayload(target byte, durationMS uint16, value string) ([]byte, error) {
	if target > DisplayReleaseHostPanel {
		return nil, fmt.Errorf("legacy display target %d is outside 0..4", target)
	}
	if len(value) > 40 {
		return nil, fmt.Errorf("display text is %d bytes, maximum is 40", len(value))
	}
	for _, char := range []byte(value) {
		if char < 0x20 || char > 0x7E {
			return nil, fmt.Errorf("display text must contain printable ASCII")
		}
	}
	payload := make([]byte, 4+len(value))
	payload[0] = target
	binary.LittleEndian.PutUint16(payload[1:3], durationMS)
	payload[3] = byte(len(value))
	copy(payload[4:], value)
	return payload, nil
}

func ScheduledSegmentPayload(options ScheduledSegmentOptions, value string) ([]byte, error) {
	if options.Repeat > SegmentRepeatInterval {
		return nil, fmt.Errorf("segment repeat mode %d is outside 0..2", options.Repeat)
	}
	if options.SpeedMS < 80 || options.SpeedMS > 5000 {
		return nil, fmt.Errorf("segment speed must be 80..5000 ms")
	}
	if options.Repeat == SegmentRepeatInterval && options.IntervalSecond == 0 {
		return nil, fmt.Errorf("segment interval mode requires 1..255 seconds")
	}
	if len(value) > 40 {
		return nil, fmt.Errorf("display text is %d bytes, maximum is 40", len(value))
	}
	for _, char := range []byte(value) {
		if char < 0x20 || char > 0x7E {
			return nil, fmt.Errorf("display text must contain printable ASCII")
		}
	}
	flags := options.Repeat
	if options.ForceScroll {
		flags |= SegmentForceScroll
	}
	payload := make([]byte, 8+len(value))
	payload[0] = DisplayScheduledSegments
	binary.LittleEndian.PutUint16(payload[1:3], options.SpeedMS)
	payload[3] = byte(len(value))
	payload[4] = flags
	binary.LittleEndian.PutUint16(payload[5:7], options.HoldMS)
	payload[7] = options.IntervalSecond
	copy(payload[8:], value)
	return payload, nil
}

// HostPanelPayload captures the physical front panel and writes one exact
// 4-digit/2x16 representation. The 16-bit meta field packs a 4-bit UI state
// and a 12-bit editable value exactly as firmware schema 2 expects.
func HostPanelPayload(segments, line1, line2 string, state byte, value uint16) ([]byte, error) {
	if state > 0x0F || value > 0x0FFF {
		return nil, fmt.Errorf("host panel state/value %d/%d is outside 4/12 bits", state, value)
	}
	segments = padASCII(segments, 4)
	line1 = padASCII(line1, 16)
	line2 = padASCII(line2, 16)
	text := segments + line1 + line2
	return DisplayTextPayload(DisplayHostPanel, uint16(state)<<12|value, text)
}

func HostPanelReleasePayload() []byte {
	return []byte{DisplayReleaseHostPanel, 0, 0, 0}
}

func padASCII(value string, width int) string {
	if len(value) > width {
		value = value[:width]
	}
	for len(value) < width {
		value += " "
	}
	return value
}

func MacroStartPayload(id byte, label string) ([]byte, error) {
	if len(label) > 4 {
		return nil, fmt.Errorf("macro label is %d bytes, maximum is 4", len(label))
	}
	payload := []byte{id, ' ', ' ', ' ', ' '}
	for index, char := range []byte(label) {
		if char < 0x20 || char > 0x7E {
			return nil, fmt.Errorf("macro label must contain printable ASCII")
		}
		payload[index+1] = char
	}
	return payload, nil
}

func MacroStepPayload(kind, target byte, value uint16) ([]byte, error) {
	switch kind {
	case MacroRelay:
		if target > 7 {
			return nil, fmt.Errorf("macro relay %d is outside 0..7", target)
		}
		if value > 1 {
			return nil, fmt.Errorf("macro relay value %d is outside 0..1", value)
		}
	case MacroPWM:
		if target > 10 {
			return nil, fmt.Errorf("macro PWM channel %d is outside 0..10", target)
		}
		if value > 4095 {
			return nil, fmt.Errorf("macro PWM value %d is outside 0..4095", value)
		}
	case MacroRelaysAllOff, MacroUserPWMAllOff, MacroFinish:
		if target != 0 || value != 0 {
			return nil, fmt.Errorf("macro all-off step requires target and value zero")
		}
	default:
		return nil, fmt.Errorf("macro step kind %d is unknown", kind)
	}
	return []byte{kind, target, byte(value), byte(value >> 8)}, nil
}

func BuzzerPayload(frequencyHz, durationMS uint16) []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:2], frequencyHz)
	binary.LittleEndian.PutUint16(payload[2:4], durationMS)
	return payload
}

func AddressableLEDPayload(pixel, red, green, blue, brightness byte) ([]byte, error) {
	if pixel != AddressableLEDFill && pixel > AddressableLEDMaxPixel {
		return nil, fmt.Errorf(
			"addressable LED pixel %d is outside 0..%d",
			pixel,
			AddressableLEDMaxPixel,
		)
	}
	// Schema-2 hosts pre-scale once when setting the pixel. This preserves
	// brightness behavior while removing a per-refresh scaler from AVR flash.
	scale := func(value byte) byte {
		return byte((uint16(value) * uint16(brightness+1)) >> 8)
	}
	return []byte{pixel, scale(red), scale(green), scale(blue), 0xFF}, nil
}

func PWMSetPayload(channel byte, value uint16) ([]byte, error) {
	if channel > 15 {
		return nil, fmt.Errorf("PWM channel %d is outside 0..15", channel)
	}
	if value > 4095 {
		return nil, fmt.Errorf("PWM value %d is outside 0..4095", value)
	}
	return []byte{channel, byte(value), byte(value >> 8)}, nil
}

func RelayPayload(index byte, active bool) ([]byte, error) {
	if index > 7 {
		return nil, fmt.Errorf("relay index %d is outside 0..7", index)
	}
	return []byte{index, boolByte(active)}, nil
}

func RelaySidePayload(side, motion byte) ([]byte, error) {
	if side > 1 {
		return nil, fmt.Errorf("relay side %d is outside 0..1", side)
	}
	if motion > 2 {
		return nil, fmt.Errorf("relay motion %d is outside 0..2", motion)
	}
	return []byte{side, motion}, nil
}

func RFTxPayload(code uint32, bits, protocol byte, pulseUS uint16) ([]byte, error) {
	if code == 0 {
		return nil, fmt.Errorf("RF code must be nonzero")
	}
	if bits == 0 || bits > 32 {
		return nil, fmt.Errorf("RF bit length %d is outside 1..32", bits)
	}
	if protocol == 0 || protocol > MaxRFProtocol {
		return nil, fmt.Errorf(
			"RF protocol %d is outside 1..%d",
			protocol,
			MaxRFProtocol,
		)
	}
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload[0:4], code)
	payload[4] = bits
	payload[5] = protocol
	binary.LittleEndian.PutUint16(payload[6:8], pulseUS)
	return payload, nil
}

const (
	RFActionNone byte = iota
	RFActionKey
	RFActionMenu
	RFActionRelay
	RFActionSide
	RFActionPWM
)

const (
	RFBehaviorPress byte = iota
	RFBehaviorToggle
	RFBehaviorMomentary
	RFBehaviorUp
	RFBehaviorDown
	RFBehaviorStop
)

// RFLearnStartPayload selects one of the two current learning modes. The
// indefinite multi-code mode has no timeout; timer mode is bounded on the MCU.
func RFLearnStartPayload(mode, timeoutSeconds byte) ([]byte, error) {
	if mode > RFLearnModeTimer {
		return nil, fmt.Errorf("RF learn mode %d is outside 0..1", mode)
	}
	if mode == RFLearnModeIndefinite {
		if timeoutSeconds != 0 {
			return nil, fmt.Errorf("indefinite RF learning does not accept a timeout")
		}
		return []byte{mode, 0}, nil
	}
	if timeoutSeconds == 0 || timeoutSeconds > MaxRFLearnSeconds {
		return nil, fmt.Errorf(
			"RF learn timer must be 1..%d seconds",
			MaxRFLearnSeconds,
		)
	}
	return []byte{mode, timeoutSeconds}, nil
}

func RFMappingPayload(id, actionKind, actionValue, behavior byte) ([]byte, error) {
	if actionKind > RFActionPWM {
		return nil, fmt.Errorf("RF action kind %d is unknown", actionKind)
	}
	if behavior > RFBehaviorStop {
		return nil, fmt.Errorf("RF behavior %d is unknown", behavior)
	}
	switch actionKind {
	case RFActionKey, RFActionMenu:
		if actionValue > 3 {
			return nil, fmt.Errorf("RF key/menu value %d is outside 0..3", actionValue)
		}
	case RFActionRelay:
		if actionValue > 7 {
			return nil, fmt.Errorf("RF relay value %d is outside 0..7", actionValue)
		}
	case RFActionSide:
		if actionValue > 1 {
			return nil, fmt.Errorf("RF side value %d is outside 0..1", actionValue)
		}
	case RFActionPWM:
		if actionValue > 10 {
			return nil, fmt.Errorf("RF PWM value %d is outside 0..10", actionValue)
		}
	}
	return []byte{id, actionKind, actionValue, behavior}, nil
}

// RFReplacePayload serializes the firmware's packed 12-byte LearnedRemote.
func RFReplacePayload(entry RFEntry) ([]byte, error) {
	if entry.ID >= 20 || entry.Code == 0 || entry.Bits == 0 || entry.Bits > 32 ||
		entry.Protocol == 0 {
		return nil, fmt.Errorf("invalid learned RF record id=%d code=%d bits=%d protocol=%d", entry.ID, entry.Code, entry.Bits, entry.Protocol)
	}
	if _, err := RFMappingPayload(entry.ID, entry.ActionKind, entry.ActionValue, entry.Behavior); err != nil {
		return nil, err
	}
	payload := make([]byte, RFEntryPayloadSize)
	payload[0] = entry.ID
	binary.LittleEndian.PutUint32(payload[1:5], entry.Code)
	payload[5] = entry.Bits
	payload[6] = entry.Protocol
	binary.LittleEndian.PutUint16(payload[7:9], entry.PulseUS)
	payload[9] = entry.ActionKind
	payload[10] = entry.ActionValue
	payload[11] = entry.Behavior
	return payload, nil
}

type Settings struct {
	Flags                   byte   `json:"flags"`
	LightMode               byte   `json:"light_mode"`
	OnBrightness            byte   `json:"on_brightness"`
	OffBrightness           byte   `json:"off_brightness"`
	DisplayBrightness       byte   `json:"display_brightness"`
	StatusBrightness        byte   `json:"status_brightness"`
	OutputPersistence       byte   `json:"output_persistence"`
	StreamPeriodMS          uint16 `json:"stream_period_ms"`
	DefaultPage             byte   `json:"default_page"`
	ExtendedFlags           byte   `json:"extended_flags"`
	DisplayClosedBrightness byte   `json:"display_closed_brightness"`
	MotionExitHoldSeconds   byte   `json:"motion_exit_hold_seconds"`
	RelayRestoreMask        byte   `json:"relay_restore_mask"`
	MotionBreakMSValue      byte   `json:"-"`
	Persisted               bool   `json:"persisted"`
}

// DefaultSettings is the canonical host-owned factory configuration. AVR
// keeps only a volatile safe fallback for operation before provisioning.
func DefaultSettings() Settings {
	return Settings{
		Flags: 0, LightMode: 0, OnBrightness: 128, OffBrightness: 0,
		DisplayBrightness: 5, StatusBrightness: FactoryStatusBrightness,
		OutputPersistence: 0, StreamPeriodMS: 500, DefaultPage: 0,
		ExtendedFlags: 0, DisplayClosedBrightness: 0,
		MotionExitHoldSeconds: SettingsDefaultMotionExitHoldSeconds,
		RelayRestoreMask:      0, MotionBreakMSValue: 1,
	}
}

const (
	settingsDisplayClosedBrightnessMask  byte = 0x07
	settingsMotionExitHoldShift               = 3
	SettingsDefaultMotionExitHoldSeconds byte = 2
	SettingsMaximumMotionExitHoldSeconds byte = 31
)

const (
	OutputPersistMotionDefault byte = 1 << iota
	OutputPersistUserRelays
	OutputPersistUserPWM
	OutputPersistDirectionOnly
	OutputPersistenceMask = OutputPersistMotionDefault |
		OutputPersistUserRelays | OutputPersistUserPWM | OutputPersistDirectionOnly
)

// displayOptions packs two current EEPROM settings into the final SETTINGS
// byte: closed brightness in bits 0..2 and motion-menu exit hold in bits 3..7.
func (settings Settings) displayOptions() (byte, error) {
	if settings.DisplayClosedBrightness > settingsDisplayClosedBrightnessMask {
		return 0, fmt.Errorf(
			"closed display brightness %d is outside 0..7",
			settings.DisplayClosedBrightness,
		)
	}
	holdSeconds := settings.MotionExitHoldSeconds
	if holdSeconds == 0 {
		holdSeconds = SettingsDefaultMotionExitHoldSeconds
	}
	if holdSeconds > SettingsMaximumMotionExitHoldSeconds {
		return 0, fmt.Errorf(
			"motion exit hold %d seconds is outside 1..%d",
			holdSeconds,
			SettingsMaximumMotionExitHoldSeconds,
		)
	}
	encodedHold := holdSeconds
	if holdSeconds == SettingsDefaultMotionExitHoldSeconds {
		encodedHold = 0
	}
	return settings.DisplayClosedBrightness |
		(encodedHold << settingsMotionExitHoldShift), nil
}

func decodeDisplayOptions(value byte) (closedBrightness, holdSeconds byte) {
	closedBrightness = value & settingsDisplayClosedBrightnessMask
	holdSeconds = value >> settingsMotionExitHoldShift
	if holdSeconds == 0 {
		holdSeconds = SettingsDefaultMotionExitHoldSeconds
	}
	return closedBrightness, holdSeconds
}

func (settings Settings) MotionDoorPolicy() byte {
	return (settings.Flags & SettingsMotionDoorPolicyMask) >> 3
}

func (settings *Settings) SetMotionDoorPolicy(value byte) error {
	if value > MotionDoorNever {
		return fmt.Errorf("motion door policy %d is outside 0..3", value)
	}
	settings.Flags = (settings.Flags &^ SettingsMotionDoorPolicyMask) |
		(value << 3)
	return nil
}

func (settings Settings) DoorAudioEnabled() bool {
	return settings.Flags&SettingsDoorAudioDisabled == 0
}

func (settings *Settings) SetDoorAudioEnabled(value bool) {
	if value {
		settings.Flags &^= SettingsDoorAudioDisabled
	} else {
		settings.Flags |= SettingsDoorAudioDisabled
	}
}

func (settings Settings) RelayAudioEnabled() bool {
	return settings.Flags&SettingsRelayAudioDisabled == 0
}

func (settings *Settings) SetRelayAudioEnabled(value bool) {
	if value {
		settings.Flags &^= SettingsRelayAudioDisabled
	} else {
		settings.Flags |= SettingsRelayAudioDisabled
	}
}

const (
	SettingsSaveLastPage       byte = 1 << 0
	SettingsStatusColorMask    byte = 0x0E
	SettingsVoltageDecimalMask byte = 0x30
	SettingsCurrentDecimalMask byte = 0xC0
	SettingsDefaultDecimals    byte = 2
)

func (settings Settings) MotionBreakMS() uint16 {
	return uint16(settings.MotionBreakMSValue)
}

func (settings *Settings) SetMotionBreakMS(milliseconds uint16) error {
	if milliseconds < 1 || milliseconds > 255 {
		return fmt.Errorf("motion break %d ms must be 1..255", milliseconds)
	}
	settings.MotionBreakMSValue = byte(milliseconds)
	return nil
}

// MarshalJSON publishes the exact motion dead-time alongside the raw protocol
// fields while keeping its byte-sized wire representation internal.
func (settings Settings) MarshalJSON() ([]byte, error) {
	type wireSettings Settings
	return json.Marshal(struct {
		wireSettings
		MotionBreakMS uint16 `json:"motion_break_ms"`
	}{wireSettings: wireSettings(settings), MotionBreakMS: settings.MotionBreakMS()})
}

// UnmarshalJSON is the inverse of MarshalJSON. MotionBreakMSValue is hidden
// from the raw wire representation, so recovery artifacts must explicitly
// restore the published semantic field instead of silently accepting zero.
func (settings *Settings) UnmarshalJSON(content []byte) error {
	type wireSettings Settings
	decoded := struct {
		wireSettings
		MotionBreakMS *uint16 `json:"motion_break_ms"`
	}{}
	if err := json.Unmarshal(content, &decoded); err != nil {
		return err
	}
	if decoded.MotionBreakMS == nil {
		return fmt.Errorf("motion_break_ms is required")
	}
	value := Settings(decoded.wireSettings)
	if err := value.SetMotionBreakMS(*decoded.MotionBreakMS); err != nil {
		return err
	}
	*settings = value
	return nil
}

func (settings Settings) SaveLastPage() bool {
	return settings.ExtendedFlags&SettingsSaveLastPage != 0
}

func (settings *Settings) SetSaveLastPage(enabled bool) {
	if enabled {
		settings.ExtendedFlags |= SettingsSaveLastPage
	} else {
		settings.ExtendedFlags &^= SettingsSaveLastPage
	}
}

func (settings Settings) StatusColor() byte {
	return (settings.ExtendedFlags & SettingsStatusColorMask) >> 1
}

func (settings *Settings) SetStatusColor(value byte) error {
	if value > 4 {
		return fmt.Errorf("status color %d is outside 0..4", value)
	}
	settings.ExtendedFlags =
		(settings.ExtendedFlags &^ SettingsStatusColorMask) | (value << 1)
	return nil
}

func (settings Settings) VoltageDecimals() byte {
	return decodeDecimalSetting(
		settings.ExtendedFlags,
		SettingsVoltageDecimalMask,
		4,
	)
}

func (settings *Settings) SetVoltageDecimals(value byte) error {
	return setDecimalSetting(
		&settings.ExtendedFlags,
		SettingsVoltageDecimalMask,
		4,
		value,
		"voltage",
	)
}

func (settings Settings) CurrentDecimals() byte {
	return decodeDecimalSetting(
		settings.ExtendedFlags,
		SettingsCurrentDecimalMask,
		6,
	)
}

func (settings *Settings) SetCurrentDecimals(value byte) error {
	return setDecimalSetting(
		&settings.ExtendedFlags,
		SettingsCurrentDecimalMask,
		6,
		value,
		"current",
	)
}

// Zero canonically encodes two decimals; values 1..3 encode decimal counts 0..2.
func decodeDecimalSetting(flags, mask byte, shift uint) byte {
	encoded := (flags & mask) >> shift
	if encoded == 0 {
		return SettingsDefaultDecimals
	}
	return encoded - 1
}

func setDecimalSetting(
	flags *byte,
	mask byte,
	shift uint,
	value byte,
	name string,
) error {
	if value > 2 {
		return fmt.Errorf("%s decimals %d is outside 0..2", name, value)
	}
	encoded := (value + 1) << shift
	*flags = (*flags &^ mask) | (encoded & mask)
	return nil
}

func (settings Settings) Payload() ([]byte, error) {
	if settings.DisplayBrightness > 7 {
		return nil, fmt.Errorf(
			"display brightness %d is outside 0..7",
			settings.DisplayBrightness,
		)
	}
	displayOptions, err := settings.displayOptions()
	if err != nil {
		return nil, err
	}
	if settings.LightMode > 2 {
		return nil, fmt.Errorf("light mode %d is outside 0..2", settings.LightMode)
	}
	if settings.OutputPersistence&^OutputPersistenceMask != 0 {
		return nil, fmt.Errorf(
			"output persistence flags 0x%02X exceed mask 0x%02X",
			settings.OutputPersistence, OutputPersistenceMask,
		)
	}
	if settings.MotionBreakMSValue == 0 {
		return nil, fmt.Errorf("motion break must be 1..255 ms")
	}
	if _, err := StreamPeriodPayload(settings.StreamPeriodMS); err != nil {
		return nil, err
	}
	payload := []byte{
		SettingsShape,
		settings.Flags,
		settings.LightMode,
		settings.OnBrightness,
		settings.OffBrightness,
		settings.DisplayBrightness,
		settings.StatusBrightness,
		settings.OutputPersistence,
		0,
		0,
		settings.DefaultPage,
		settings.ExtendedFlags,
		displayOptions,
		settings.RelayRestoreMask,
		settings.MotionBreakMSValue,
	}
	binary.LittleEndian.PutUint16(payload[8:10], settings.StreamPeriodMS)
	return payload, nil
}

func StatusRGBPayload(red, green, blue, brightness byte) []byte {
	return []byte{red, green, blue, brightness}
}

const (
	StatusEffectMinimumPeriodMS uint16 = 640
	StatusEffectMaximumPeriodMS uint16 = 60000
)

func StatusEffectPayload(options StatusEffectOptions) ([]byte, error) {
	if options.Kind < StatusEffectBreathe || options.Kind > StatusEffectTransition {
		return nil, fmt.Errorf("status effect kind %d is outside 1..4", options.Kind)
	}
	if options.PeriodMS < StatusEffectMinimumPeriodMS || options.PeriodMS > StatusEffectMaximumPeriodMS {
		return nil, fmt.Errorf("status effect period must be %d..%d ms", StatusEffectMinimumPeriodMS, StatusEffectMaximumPeriodMS)
	}
	if options.MinimumBrightness > options.Brightness {
		return nil, fmt.Errorf("status effect minimum brightness exceeds brightness")
	}
	return statusEffectDescriptor(options), nil
}

func statusEffectDescriptor(options StatusEffectOptions) []byte {
	payload := []byte{
		options.Kind,
		options.Red, options.Green, options.Blue,
		options.AlternateRed, options.AlternateGreen, options.AlternateBlue,
		options.Brightness, options.MinimumBrightness,
		0, 0, options.Repeats,
	}
	binary.LittleEndian.PutUint16(payload[9:11], options.PeriodMS)
	return payload
}

func StatusEffectReleasePayload() []byte { return []byte{StatusEffectNone} }

func StatusProfileGetPayload(condition byte) ([]byte, error) {
	if condition >= StatusProfileCount {
		return nil, fmt.Errorf("status profile condition %d is outside 0..%d", condition, StatusProfileCount-1)
	}
	return []byte{condition}, nil
}

func StatusProfileSetPayload(condition byte, options StatusEffectOptions) ([]byte, error) {
	if condition >= StatusProfileCount {
		return nil, fmt.Errorf("status profile condition %d is outside 0..%d", condition, StatusProfileCount-1)
	}
	if options.Kind > StatusEffectTransition {
		return nil, fmt.Errorf("status profile effect kind %d is outside 0..4", options.Kind)
	}
	if options.MinimumBrightness > options.Brightness {
		return nil, fmt.Errorf("status effect minimum brightness exceeds brightness")
	}
	if options.Kind != StatusEffectNone &&
		(options.PeriodMS < StatusEffectMinimumPeriodMS || options.PeriodMS > StatusEffectMaximumPeriodMS) {
		return nil, fmt.Errorf("status effect period must be %d..%d ms", StatusEffectMinimumPeriodMS, StatusEffectMaximumPeriodMS)
	}
	return append([]byte{condition}, statusEffectDescriptor(options)...), nil
}

func parseStatusEffectPayload(payload []byte) (StatusEffectOptions, error) {
	if len(payload) != 12 {
		return StatusEffectOptions{}, fmt.Errorf("status effect descriptor is %d bytes, need exactly 12", len(payload))
	}
	options := StatusEffectOptions{
		Kind: payload[0], Red: payload[1], Green: payload[2], Blue: payload[3],
		AlternateRed: payload[4], AlternateGreen: payload[5], AlternateBlue: payload[6],
		Brightness: payload[7], MinimumBrightness: payload[8],
		PeriodMS: binary.LittleEndian.Uint16(payload[9:11]), Repeats: payload[11],
	}
	if _, err := StatusProfileSetPayload(0, options); err != nil {
		return StatusEffectOptions{}, err
	}
	return options, nil
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}
