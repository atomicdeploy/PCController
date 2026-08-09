package native

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	BoardKindPCController byte = 1
	SettingsShape         byte = 3
	IdentitySchemaCompact byte = 3
	RFEntriesSchema       byte = 1
	MenuListSchema        byte = 1
	TemperatureSchema     byte = 1
	StatusPayloadSize          = 48
	RFEntryPayloadSize         = 12
)

// HELLO capability bits are the authoritative guard for optional operations.
const (
	CapabilityFrontPanelSnapshot uint32 = 1 << 13
	CapabilityI2CTransfer        uint32 = 1 << 16
	CapabilityMenuDirectory      uint32 = 1 << 17
	CapabilityRFLearnReplace     uint32 = 1 << 18
	CapabilityHostFrontPanel     uint32 = 1 << 19
	CapabilityBuzzerBusy         uint32 = 1 << 20
	CapabilityMotionBreak        uint32 = 1 << 21
	CapabilityTimedMacroQueue    uint32 = 1 << 22
	CapabilityMenuLayout         uint32 = 1 << 23
	CapabilityProgramState       uint32 = 1 << 24
	CapabilityScheduledSegments  uint32 = 1 << 25
	CapabilitySegmentPush        uint32 = 1 << 26
	CapabilityBuzzerPush         uint32 = 1 << 27
	CapabilityStatusEffects      uint32 = 1 << 28
	CapabilityStatusLEDPush      uint32 = 1 << 29
	CapabilityStatusProfiles     uint32 = 1 << 30
	CapabilityBoardName          uint32 = 1 << 31
)

const MaximumBoardNameLength = 8

type BoardName struct {
	Name      string `json:"name"`
	Persisted bool   `json:"persisted"`
}

func ValidateBoardName(name string) error {
	if len(name) > MaximumBoardNameLength {
		return fmt.Errorf("board name is %d bytes; maximum is %d", len(name), MaximumBoardNameLength)
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("board name must not have leading or trailing whitespace")
	}
	for index := 0; index < len(name); index++ {
		if name[index] < 0x20 || name[index] > 0x7E {
			return fmt.Errorf("board name must contain printable ASCII characters only")
		}
	}
	return nil
}

// SettingsWithBoardNamePayload extends the canonical 15-byte settings command
// with a name length and name. An unextended settings write deliberately keeps
// the name so ordinary settings changes do not erase the board's identity.
func SettingsWithBoardNamePayload(settings Settings, name string) ([]byte, error) {
	if err := ValidateBoardName(name); err != nil {
		return nil, err
	}
	payload, err := settings.Payload()
	if err != nil {
		return nil, err
	}
	payload = append(payload, byte(len(name)))
	payload = append(payload, name...)
	return payload, nil
}

// ParseBoardNameFromSettings reads the optional extension of SETTINGS. Keeping
// identity in that existing exchange saves flash and protocol slots on AVR.
func ParseBoardNameFromSettings(payload []byte) (BoardName, error) {
	if len(payload) < 18 || len(payload) > 18+MaximumBoardNameLength || payload[16] > 1 ||
		int(payload[17]) > MaximumBoardNameLength || len(payload) != 18+int(payload[17]) ||
		(payload[16] == 0 && payload[17] != 0) {
		return BoardName{}, fmt.Errorf("invalid SETTINGS board-name extension")
	}
	name := string(payload[18:])
	if err := ValidateBoardName(name); err != nil {
		return BoardName{}, err
	}
	return BoardName{Name: name, Persisted: payload[16] != 0}, nil
}

const (
	StatusBuzzerBusy     uint16 = 1 << 12
	StatusProgramRunning uint16 = 1 << 13
	StatusHostOffline    uint16 = 1 << 14
	StatusHot            uint16 = 1 << 15
)

// SupportsHostMenuOverlay remains an explicit semantic probe for the
// anticipatory host-menu code. No capability bit is currently assigned by the
// firmware, so bit 24 must never be mistaken for this feature.
func SupportsHostMenuOverlay(Hello) bool { return false }

// BuzzerBusy only interprets status bit 12 when HELLO advertises the new
// meaning; legacy firmware used the same raw bit for macro-active.
func BuzzerBusy(hello Hello, status Status) (busy bool, known bool) {
	if hello.Capabilities&CapabilityBuzzerBusy == 0 {
		return false, false
	}
	return status.Flags&StatusBuzzerBusy != 0, true
}

// Device error codes mirror ControllerProtocol::Error.
const (
	ErrorBadEnvelope byte = iota + 1
	ErrorUnsupported
	ErrorBadPayload
	ErrorHardwareUnavailable
	ErrorBusy
	ErrorUnsafe
)

const (
	TemperatureEntryPayloadSize = 11
	temperatureMaximumEntries   = (MaxPayload - 2) / TemperatureEntryPayloadSize
)

type FrontPanel struct {
	Schema            byte    `json:"schema"`
	RawSegments       [4]byte `json:"raw_segments"`
	Brightness        byte    `json:"brightness"`
	Blink             bool    `json:"blink"`
	SegmentsActive    bool    `json:"segments_active"`
	CategorySelector  bool    `json:"category_selector"`
	LCDAddress        byte    `json:"lcd_address"`
	LCDAvailable      bool    `json:"lcd_available"`
	LCDBacklight      bool    `json:"lcd_backlight"`
	LCDLine1          string  `json:"lcd_line_1"`
	LCDLine2          string  `json:"lcd_line_2"`
	PressedKeys       byte    `json:"pressed_keys"`
	MenuPage          byte    `json:"menu_page"`
	ProgramMode       byte    `json:"program_mode"`
	HostCaptured      bool    `json:"host_captured"`
	HostState         byte    `json:"host_state"`
	HostEditableValue uint16  `json:"host_editable_value"`
}

// SegmentState is the changed-only display frame pushed by the board. It is
// intentionally smaller than FrontPanel: hosts use FRONT_PANEL_GET for an
// initial/explicit refresh and this event for low-latency live mirroring.
type SegmentState struct {
	RawSegments [4]byte `json:"raw_segments"`
	Brightness  byte    `json:"brightness"`
}

func ParseSegmentState(payload []byte) (SegmentState, error) {
	if len(payload) != 5 {
		return SegmentState{}, fmt.Errorf("SEGMENT_CHANGED payload is %d bytes, need exactly 5", len(payload))
	}
	state := SegmentState{Brightness: payload[4]}
	copy(state.RawSegments[:], payload[:4])
	return state, nil
}

// BuzzerState describes the tone most recently started by firmware. Duration
// lets every host surface stop its local mirror without a second board event.
type BuzzerState struct {
	FrequencyHz uint16 `json:"frequency_hz"`
	DurationMS  uint16 `json:"duration_ms"`
	Muted       bool   `json:"muted"`
}

func ParseBuzzerState(payload []byte) (BuzzerState, error) {
	if len(payload) != 5 {
		return BuzzerState{}, fmt.Errorf("BUZZER_CHANGED payload is %d bytes, need exactly 5", len(payload))
	}
	if payload[4] > 1 {
		return BuzzerState{}, fmt.Errorf("BUZZER_CHANGED muted flag is %d, need 0 or 1", payload[4])
	}
	return BuzzerState{
		FrequencyHz: binary.LittleEndian.Uint16(payload[:2]),
		DurationMS:  binary.LittleEndian.Uint16(payload[2:4]),
		Muted:       payload[4] != 0,
	}, nil
}

// StatusLEDState is the changed-only physical RGB result pushed after the MCU
// compositor applies local safety priority, brightness, and procedural effects.
type StatusLEDState struct {
	Red        byte `json:"red"`
	Green      byte `json:"green"`
	Blue       byte `json:"blue"`
	Brightness byte `json:"brightness"`
	Effect     byte `json:"effect"`
	Condition  byte `json:"condition"`
}

func ParseStatusLEDState(payload []byte) (StatusLEDState, error) {
	if len(payload) != 6 {
		return StatusLEDState{}, fmt.Errorf("STATUS_LED_CHANGED payload is %d bytes, need exactly 6", len(payload))
	}
	if payload[4] > StatusEffectTransition {
		return StatusLEDState{}, fmt.Errorf("STATUS_LED_CHANGED effect %d is outside 0..%d", payload[4], StatusEffectTransition)
	}
	return StatusLEDState{
		Red: payload[0], Green: payload[1], Blue: payload[2],
		Brightness: payload[3], Effect: payload[4], Condition: payload[5],
	}, nil
}

type StatusProfile struct {
	Condition byte                `json:"condition"`
	Persisted bool                `json:"persisted"`
	Effect    StatusEffectOptions `json:"effect"`
}

func ParseStatusProfile(payload []byte) (StatusProfile, error) {
	if len(payload) != 13 && len(payload) != 14 {
		return StatusProfile{}, fmt.Errorf("STATUS_PROFILE payload is %d bytes, need 13 or 14", len(payload))
	}
	if payload[0] >= StatusProfileCount {
		return StatusProfile{}, fmt.Errorf("status profile condition %d is outside 0..%d", payload[0], StatusProfileCount-1)
	}
	effect, err := parseStatusEffectPayload(payload[1:13])
	if err != nil {
		return StatusProfile{}, err
	}
	// Thirteen-byte responses predate the appended persistence flag. Treat
	// them as authoritative so a newer host never overwrites unknown firmware.
	persisted := len(payload) == 13 || payload[13] != 0
	return StatusProfile{Condition: payload[0], Persisted: persisted, Effect: effect}, nil
}

func ParseFrontPanel(payload []byte) (FrontPanel, error) {
	if len(payload) < 44 {
		return FrontPanel{}, fmt.Errorf("FRONT_PANEL payload is %d bytes, need at least 44", len(payload))
	}
	if payload[0] != 1 && payload[0] != 2 {
		return FrontPanel{}, fmt.Errorf("unsupported front-panel schema %d", payload[0])
	}
	if payload[0] == 2 && len(payload) < 47 {
		return FrontPanel{}, fmt.Errorf("front-panel schema 2 payload is %d bytes, need 47", len(payload))
	}
	panel := FrontPanel{Schema: payload[0], Brightness: payload[5]}
	copy(panel.RawSegments[:], payload[1:5])
	panel.Blink = payload[6]&0x01 != 0
	panel.SegmentsActive = payload[6]&0x02 != 0
	panel.CategorySelector = payload[6]&0x04 != 0
	panel.LCDAddress = payload[7]
	panel.LCDAvailable = payload[8]&0x01 != 0
	panel.LCDBacklight = payload[8]&0x02 != 0
	panel.LCDLine1 = strings.TrimRight(string(payload[9:25]), " \x00")
	panel.LCDLine2 = strings.TrimRight(string(payload[25:41]), " \x00")
	panel.PressedKeys = payload[41]
	panel.MenuPage = payload[42]
	panel.ProgramMode = payload[43]
	if panel.Schema == 2 {
		panel.HostCaptured = payload[44]&0x80 != 0
		panel.HostState = payload[44] & 0x0F
		panel.HostEditableValue = binary.LittleEndian.Uint16(payload[45:47]) & 0x0FFF
	}
	return panel, nil
}

const (
	EventKey byte = iota + 1
	EventDoor
	EventBluetooth
	EventPWMChannel
	EventRFLearned
	EventMacro
	EventReset
	EventRFReceived
	EventRFLearning
	EventRelay
	EventAlert
	EventAppNavigation
	// EventAction carries one successfully applied ordinary command from a
	// physical or RF source. The host ACK remains authoritative for source=Host.
	EventAction
)

const (
	AppNavigationAll byte = iota
	AppNavigationWebUI
	AppNavigationTUI
)

const (
	AlertFault byte = iota + 1
	AlertHot
)

const (
	InputSourcePhysical byte = iota
	InputSourceRF
	InputSourceHost
)

const (
	MacroEventStarted byte = iota + 1
	MacroEventStep
	MacroEventCancelled
	MacroEventCompleted
)

const (
	RFLearningEnded byte = iota
	RFLearningCancelled
	RFLearningFull
	RFLearningStarted
	RFLearningProgress
)

const (
	RFLearnModeIndefinite byte = iota
	RFLearnModeTimer
)

type Hello struct {
	BoardKind      byte   `json:"board_kind"`
	Capabilities   uint32 `json:"capabilities"`
	Name           string `json:"name"`
	IdentitySchema byte   `json:"identity_schema"`
	BuildHash      uint32 `json:"build_hash"`
	BuildTimestamp uint32 `json:"build_timestamp_packed,omitempty"`
	BuildStamp     string `json:"build_timestamp,omitempty"`
}

func ParseHello(payload []byte) (Hello, error) {
	if len(payload) != 14 {
		return Hello{}, fmt.Errorf("HELLO payload is %d bytes, need exactly 14", len(payload))
	}
	if payload[0] != IdentitySchemaCompact {
		return Hello{}, fmt.Errorf("unsupported HELLO identity schema %d", payload[0])
	}
	hello := Hello{
		BoardKind:      payload[1],
		Capabilities:   binary.LittleEndian.Uint32(payload[2:6]),
		Name:           "PCController",
		IdentitySchema: IdentitySchemaCompact,
		BuildHash:      binary.LittleEndian.Uint32(payload[6:10]),
		BuildTimestamp: binary.LittleEndian.Uint32(payload[10:14]),
	}
	stamp, err := FormatBuildTimestamp(hello.BuildTimestamp)
	if err != nil {
		return Hello{}, err
	}
	hello.BuildStamp = stamp
	return hello, nil
}

// FormatBuildTimestamp decodes the packed date<<16|time value as YYMMDDHHMMSS.
func FormatBuildTimestamp(packed uint32) (string, error) {
	if packed == 0 {
		return "", nil
	}
	year := 2000 + int((packed>>25)&0x7F)
	month := time.Month((packed >> 21) & 0x0F)
	day := int((packed >> 16) & 0x1F)
	hour := int((packed >> 11) & 0x1F)
	minute := int((packed >> 5) & 0x3F)
	second := int(packed&0x1F) * 2
	if month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 {
		return "", fmt.Errorf("invalid packed build timestamp 0x%08X", packed)
	}
	value := time.Date(year, month, day, hour, minute, second, 0, time.UTC)
	if value.Year() != year || value.Month() != month || value.Day() != day {
		return "", fmt.Errorf("invalid packed build calendar date 0x%08X", packed)
	}
	return value.Format("060102150405"), nil
}

func (hello Hello) IsPCController() bool {
	return hello.BoardKind == BoardKindPCController && hello.Name == "PCController"
}

func ParseSettings(payload []byte) (Settings, error) {
	extended := len(payload) >= 18 && len(payload) <= 18+MaximumBoardNameLength
	if len(payload) != 15 && len(payload) != 16 && !extended {
		return Settings{}, fmt.Errorf("SETTINGS payload is %d bytes, need 15, 16, or 18..%d", len(payload), 18+MaximumBoardNameLength)
	}
	if payload[0] != SettingsShape {
		return Settings{}, fmt.Errorf("unsupported SETTINGS shape %d", payload[0])
	}
	if extended {
		if _, err := ParseBoardNameFromSettings(payload); err != nil {
			return Settings{}, err
		}
	}
	closedBrightness, holdSeconds := decodeDisplayOptions(payload[12])
	settings := Settings{
		Flags:                   payload[1],
		LightMode:               payload[2],
		OnBrightness:            payload[3],
		OffBrightness:           payload[4],
		DisplayBrightness:       payload[5],
		StatusBrightness:        payload[6],
		OutputPersistence:       payload[7],
		StreamPeriodMS:          binary.LittleEndian.Uint16(payload[8:10]),
		DefaultPage:             payload[10],
		ExtendedFlags:           payload[11],
		DisplayClosedBrightness: closedBrightness,
		MotionExitHoldSeconds:   holdSeconds,
		RelayRestoreMask:        payload[13],
		MotionBreakMSValue:      payload[14],
		Persisted:               len(payload) == 15 || payload[15] != 0,
	}
	if _, err := settings.Payload(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

type Status struct {
	UptimeMS       uint32 `json:"uptime_ms"`
	SupplyMV       int32  `json:"supply_mv"`
	BusMV          int32  `json:"bus_mv"`
	CurrentMA      int32  `json:"current_ma"`
	PowerMW        int32  `json:"power_mw"`
	TLEDCenti      int16  `json:"temperature_led_centi_c"`
	TBTCenti       int16  `json:"temperature_bt_audio_centi_c"`
	Flags          uint16 `json:"flags"`
	ProgramRunning bool   `json:"program_running"`
	HostOffline    bool   `json:"host_offline"`
	Hot            bool   `json:"hot"`
	RawInputs      byte   `json:"raw_inputs"`
	ActiveKeys     byte   `json:"active_keys"`
	ActiveRelays   byte   `json:"active_relays"`
	MenuPage       byte   `json:"menu_page"`
	ProgramMode    byte   `json:"program_mode"`
	DoorOpen       bool   `json:"door_open"`
	BluetoothState byte   `json:"bluetooth_audio_state"`
	PWMAvailable   bool   `json:"pwm_available"`
	PWMChannel     byte   `json:"pwm_channel"`
	PWMValue       uint16 `json:"pwm_value"`
	LCDAddress     byte   `json:"lcd_address"`
	PWMErrors      byte   `json:"pwm_errors"`
	FramingErrors  uint16 `json:"framing_errors"`
	CRCErrors      uint16 `json:"crc_errors"`
	ResetCause     byte   `json:"reset_cause"`
	ResetCount     uint32 `json:"reset_count"`
}

func (status Status) UptimeDuration() time.Duration {
	return time.Duration(status.UptimeMS) * time.Millisecond
}

func (status Status) ReadableUptime() string {
	return status.UptimeDuration().String()
}

// MarshalJSON keeps uptime_ms as the stable machine value and adds a derived
// human-readable value to every snapshot, history, REST, RPC, and scripting
// JSON surface. The derived field never enters the compact UART payload.
func (status Status) MarshalJSON() ([]byte, error) {
	type StatusFields Status
	return json.Marshal(struct {
		StatusFields
		Uptime string `json:"uptime"`
	}{
		StatusFields: StatusFields(status),
		Uptime:       status.ReadableUptime(),
	})
}

func (status *Status) UnmarshalJSON(data []byte) error {
	type StatusFields Status
	decoded := struct {
		StatusFields
		Uptime string `json:"uptime"`
	}{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*status = Status(decoded.StatusFields)
	return nil
}

func ParseStatus(payload []byte) (Status, error) {
	if len(payload) < StatusPayloadSize {
		return Status{}, fmt.Errorf(
			"STATUS payload is %d bytes, need at least %d",
			len(payload),
			StatusPayloadSize,
		)
	}
	status := Status{
		UptimeMS:       binary.LittleEndian.Uint32(payload[0:4]),
		SupplyMV:       int32(binary.LittleEndian.Uint32(payload[4:8])),
		BusMV:          int32(binary.LittleEndian.Uint32(payload[8:12])),
		CurrentMA:      int32(binary.LittleEndian.Uint32(payload[12:16])),
		PowerMW:        int32(binary.LittleEndian.Uint32(payload[16:20])),
		TLEDCenti:      int16(binary.LittleEndian.Uint16(payload[20:22])),
		TBTCenti:       int16(binary.LittleEndian.Uint16(payload[22:24])),
		Flags:          binary.LittleEndian.Uint16(payload[24:26]),
		RawInputs:      payload[26],
		ActiveKeys:     payload[27],
		ActiveRelays:   payload[28],
		MenuPage:       payload[29],
		ProgramMode:    payload[30],
		DoorOpen:       payload[31] != 0,
		BluetoothState: payload[32],
		PWMAvailable:   payload[33] != 0,
		PWMChannel:     payload[34],
		PWMValue:       binary.LittleEndian.Uint16(payload[35:37]),
		LCDAddress:     payload[37],
		PWMErrors:      payload[38],
		FramingErrors:  binary.LittleEndian.Uint16(payload[39:41]),
		CRCErrors:      binary.LittleEndian.Uint16(payload[41:43]),
	}
	status.ProgramRunning = status.Flags&StatusProgramRunning != 0
	status.HostOffline = status.Flags&StatusHostOffline != 0
	status.Hot = status.Flags&StatusHot != 0
	status.ResetCause = payload[43]
	status.ResetCount = binary.LittleEndian.Uint32(payload[44:48])
	return status, nil
}

type PWMValues struct {
	Available       bool       `json:"available"`
	SelectedChannel byte       `json:"selected_channel"`
	Values          [16]uint16 `json:"values"`
}

func ParsePWMValues(payload []byte) (PWMValues, error) {
	if len(payload) < 34 {
		return PWMValues{}, fmt.Errorf("PWM_VALUES payload is %d bytes, need 34", len(payload))
	}
	if payload[0] > 1 {
		return PWMValues{}, fmt.Errorf("PWM availability %d is outside 0..1", payload[0])
	}
	result := PWMValues{Available: payload[0] != 0, SelectedChannel: payload[1]}
	for index := range result.Values {
		offset := 2 + index*2
		result.Values[index] = binary.LittleEndian.Uint16(payload[offset : offset+2])
	}
	return result, nil
}

func ParseI2CResult(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("I2C_RESULT has no count")
	}
	count := int(payload[0])
	if count > MaxPayload-1 || len(payload) < count+1 {
		return nil, fmt.Errorf("I2C_RESULT count %d does not match payload", count)
	}
	return append([]byte(nil), payload[1:1+count]...), nil
}

type TemperatureSensor struct {
	Role         byte    `json:"role"`
	ROM          [8]byte `json:"rom"`
	CelsiusCenti int16   `json:"celsius_centi"`
}

func (sensor TemperatureSensor) Identifier() string {
	return fmt.Sprintf(
		"%02X-%02X%02X%02X%02X%02X%02X-%02X",
		sensor.ROM[0],
		sensor.ROM[1],
		sensor.ROM[2],
		sensor.ROM[3],
		sensor.ROM[4],
		sensor.ROM[5],
		sensor.ROM[6],
		sensor.ROM[7],
	)
}

func ParseTemperatures(payload []byte) ([]TemperatureSensor, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("TEMPERATURES payload is %d bytes, need at least 2", len(payload))
	}
	if payload[0] != TemperatureSchema {
		return nil, fmt.Errorf("unsupported temperature schema %d", payload[0])
	}
	count := int(payload[1])
	if count > temperatureMaximumEntries {
		return nil, fmt.Errorf(
			"TEMPERATURES count %d exceeds protocol maximum %d",
			count,
			temperatureMaximumEntries,
		)
	}
	needed := 2 + count*TemperatureEntryPayloadSize
	if needed > MaxPayload || len(payload) < needed {
		return nil, fmt.Errorf(
			"TEMPERATURES count %d needs %d bytes, payload has %d",
			count,
			needed,
			len(payload),
		)
	}
	result := make([]TemperatureSensor, 0, count)
	for index := 0; index < count; index++ {
		offset := 2 + index*TemperatureEntryPayloadSize
		sensor := TemperatureSensor{Role: payload[offset]}
		copy(sensor.ROM[:], payload[offset+1:offset+9])
		sensor.CelsiusCenti = int16(binary.LittleEndian.Uint16(payload[offset+9 : offset+11]))
		result = append(result, sensor)
	}
	return result, nil
}

type DeviceEvent struct {
	Type                    byte         `json:"type"`
	Key                     byte         `json:"key,omitempty"`
	Gesture                 byte         `json:"gesture,omitempty"`
	Source                  byte         `json:"source,omitempty"`
	SourceID                byte         `json:"source_id,omitempty"`
	DoorOpen                bool         `json:"door_open,omitempty"`
	Bluetooth               byte         `json:"bluetooth,omitempty"`
	PWMChannel              byte         `json:"pwm_channel,omitempty"`
	RFID                    byte         `json:"rf_id,omitempty"`
	MacroState              byte         `json:"macro_state,omitempty"`
	MacroID                 byte         `json:"macro_id,omitempty"`
	ResetCause              byte         `json:"reset_cause,omitempty"`
	ResetCount              uint32       `json:"reset_count,omitempty"`
	RFCode                  uint32       `json:"rf_code,omitempty"`
	RFBits                  byte         `json:"rf_bits,omitempty"`
	RFProtocol              byte         `json:"rf_protocol,omitempty"`
	RFPulseUS               uint16       `json:"rf_pulse_us,omitempty"`
	RFLearnedID             byte         `json:"rf_learned_id,omitempty"`
	RFLearnState            byte         `json:"rf_learning_state,omitempty"`
	RFLearnCount            byte         `json:"rf_learning_count,omitempty"`
	RFLearnMode             byte         `json:"rf_learning_mode,omitempty"`
	RFLearnTotalSeconds     byte         `json:"rf_learning_total_seconds,omitempty"`
	RFLearnRemainingSeconds byte         `json:"rf_learning_remaining_seconds,omitempty"`
	RelayMask               byte         `json:"relay_mask,omitempty"`
	AlertKind               byte         `json:"alert_kind,omitempty"`
	AlertActive             bool         `json:"alert_active,omitempty"`
	AppTarget               string       `json:"app_target,omitempty"`
	AppPage                 string       `json:"app_page,omitempty"`
	ActionOpcode            byte         `json:"action_opcode,omitempty"`
	ActionPayload           []byte       `json:"action_payload,omitempty"`
	DeviceMicros            uint32       `json:"device_micros,omitempty"`
	Timed                   bool         `json:"timed,omitempty"`
	Macro                   *MacroStatus `json:"macro,omitempty"`
	Raw                     []byte       `json:"raw,omitempty"`
}

func ParseDeviceEvent(payload []byte) (DeviceEvent, error) {
	if len(payload) == 0 {
		return DeviceEvent{}, fmt.Errorf("EVENT payload is empty")
	}
	body := payload
	timed := payload[0]&0x80 != 0
	var deviceMicros uint32
	if timed {
		if len(payload) < 5 {
			return DeviceEvent{}, fmt.Errorf("timed EVENT is %d bytes, need at least 5", len(payload))
		}
		deviceMicros = binary.LittleEndian.Uint32(payload[len(payload)-4:])
		body = payload[:len(payload)-4]
	}
	event := DeviceEvent{
		Type:         body[0] & 0x7F,
		DeviceMicros: deviceMicros,
		Timed:        timed,
		Raw:          append([]byte(nil), body[1:]...),
		SourceID:     0xFF,
		RFLearnedID:  0xFF,
	}
	payload = body
	switch event.Type {
	case EventKey:
		if len(payload) < 3 {
			return DeviceEvent{}, fmt.Errorf("key EVENT is %d bytes, need 3", len(payload))
		}
		event.Key, event.Gesture = payload[1], payload[2]
		if len(payload) >= 4 {
			event.Source = payload[3]
		}
		if len(payload) >= 5 {
			event.SourceID = payload[4]
		}
	case EventDoor:
		if len(payload) < 2 {
			return DeviceEvent{}, fmt.Errorf("door EVENT is %d bytes, need 2", len(payload))
		}
		event.DoorOpen = payload[1] != 0
	case EventBluetooth:
		if len(payload) < 2 {
			return DeviceEvent{}, fmt.Errorf("Bluetooth EVENT is %d bytes, need 2", len(payload))
		}
		event.Bluetooth = payload[1]
	case EventPWMChannel:
		if len(payload) < 2 {
			return DeviceEvent{}, fmt.Errorf("PWM EVENT is %d bytes, need 2", len(payload))
		}
		event.PWMChannel = payload[1]
	case EventRFLearned:
		if len(payload) < 2 {
			return DeviceEvent{}, fmt.Errorf("RF EVENT is %d bytes, need 2", len(payload))
		}
		event.RFID = payload[1]
	case EventMacro:
		if len(payload) >= 2 && payload[1] == MacroQueueSchema {
			status, err := ParseMacroStatus(payload)
			if err != nil {
				return DeviceEvent{}, err
			}
			event.Macro = &status
			event.MacroState, event.MacroID = status.State, status.ID
			break
		}
		if len(payload) < 3 {
			return DeviceEvent{}, fmt.Errorf("macro EVENT is %d bytes, need 3", len(payload))
		}
		event.MacroState, event.MacroID = payload[1], payload[2]
	case EventReset:
		if len(payload) != 6 {
			return DeviceEvent{}, fmt.Errorf(
				"reset EVENT is %d bytes, need exactly 6",
				len(payload),
			)
		}
		event.ResetCause = payload[1]
		event.ResetCount = binary.LittleEndian.Uint32(payload[2:6])
	case EventRFReceived:
		if len(payload) != 10 {
			return DeviceEvent{}, fmt.Errorf(
				"RF received EVENT is %d bytes, need exactly 10",
				len(payload),
			)
		}
		event.RFCode = binary.LittleEndian.Uint32(payload[1:5])
		event.RFBits = payload[5]
		event.RFProtocol = payload[6]
		event.RFPulseUS = binary.LittleEndian.Uint16(payload[7:9])
		event.RFLearnedID = payload[9]
	case EventRFLearning:
		if len(payload) != 6 {
			return DeviceEvent{}, fmt.Errorf(
				"RF learning EVENT is %d bytes, need exactly 6",
				len(payload),
			)
		}
		if payload[1] > RFLearningProgress || payload[3] > RFLearnModeTimer ||
			(payload[3] == RFLearnModeIndefinite && (payload[4] != 0 || payload[5] != 0)) ||
			payload[5] > payload[4] {
			return DeviceEvent{}, fmt.Errorf("invalid RF learning EVENT fields: % X", payload)
		}
		event.RFLearnState, event.RFLearnCount = payload[1], payload[2]
		event.RFLearnMode = payload[3]
		event.RFLearnTotalSeconds = payload[4]
		event.RFLearnRemainingSeconds = payload[5]
	case EventRelay:
		if len(payload) != 2 {
			return DeviceEvent{}, fmt.Errorf(
				"relay EVENT is %d bytes, need exactly 2",
				len(payload),
			)
		}
		event.RelayMask = payload[1]
	case EventAlert:
		if len(payload) != 3 {
			return DeviceEvent{}, fmt.Errorf(
				"alert EVENT is %d bytes, need exactly 3",
				len(payload),
			)
		}
		if payload[1] < AlertFault || payload[1] > AlertHot || payload[2] > 1 {
			return DeviceEvent{}, fmt.Errorf("invalid alert EVENT fields: % X", payload)
		}
		event.AlertKind, event.AlertActive = payload[1], payload[2] != 0
	case EventAppNavigation:
		if len(payload) < 3 {
			return DeviceEvent{}, fmt.Errorf(
				"app navigation EVENT is %d bytes, need a target and page",
				len(payload),
			)
		}
		if payload[1] > AppNavigationTUI {
			return DeviceEvent{}, fmt.Errorf("invalid app navigation target %d", payload[1])
		}
		rawPage := string(payload[2:])
		page := strings.TrimSpace(rawPage)
		if page == "" || len(page) > 96 || page != rawPage {
			return DeviceEvent{}, fmt.Errorf("app navigation page is empty or too long")
		}
		for index := 0; index < len(page); index++ {
			value := page[index]
			if (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') ||
				(value >= '0' && value <= '9') || strings.ContainsRune("._/-", rune(value)) {
				continue
			}
			return DeviceEvent{}, fmt.Errorf("app navigation page contains an invalid byte")
		}
		event.AppTarget = map[byte]string{
			AppNavigationAll: "*", AppNavigationWebUI: "webui", AppNavigationTUI: "tui",
		}[payload[1]]
		event.AppPage = strings.ToLower(page)
	case EventAction:
		// [type, source, ordinary opcode, payload length, payload...]. The
		// high-bit event marker and trailing MCU timestamp are removed above.
		if len(payload) < 4 {
			return DeviceEvent{}, fmt.Errorf("action EVENT is %d bytes, need at least 4", len(payload))
		}
		if payload[1] > InputSourceHost {
			return DeviceEvent{}, fmt.Errorf("action EVENT source %d is invalid", payload[1])
		}
		length := int(payload[3])
		if length > MacroBoardActionMaximumPayload || len(payload) != 4+length {
			return DeviceEvent{}, fmt.Errorf(
				"action EVENT payload length %d/body %d is invalid; maximum is %d",
				length, len(payload), MacroBoardActionMaximumPayload,
			)
		}
		if !MacroQueueableOpcode(payload[2]) {
			return DeviceEvent{}, fmt.Errorf("action EVENT opcode 0x%02X is not recordable", payload[2])
		}
		event.Source = payload[1]
		event.ActionOpcode = payload[2]
		event.ActionPayload = append([]byte(nil), payload[4:]...)
	}
	return event, nil
}

const (
	MenuEntryPayloadSize   = 6
	menuListMaximumEntries = (MaxPayload - 4) / MenuEntryPayloadSize
)

type MenuEntry struct {
	ID    byte   `json:"id"`
	Mode  byte   `json:"mode"`
	Label string `json:"label"`
}

type MenuListPage struct {
	Total      byte        `json:"total"`
	NextCursor byte        `json:"next_cursor"`
	Entries    []MenuEntry `json:"entries"`
}

// ParseMenuList decodes one cursor page from the firmware-owned menu catalog.
func ParseMenuList(payload []byte) (MenuListPage, error) {
	if len(payload) < 4 {
		return MenuListPage{}, fmt.Errorf(
			"MENU_LIST payload is %d bytes, need at least 4",
			len(payload),
		)
	}
	if payload[0] != MenuListSchema {
		return MenuListPage{}, fmt.Errorf(
			"unsupported menu-list schema %d",
			payload[0],
		)
	}
	count := int(payload[3])
	if count > menuListMaximumEntries {
		return MenuListPage{}, fmt.Errorf(
			"MENU_LIST count %d exceeds protocol maximum %d",
			count,
			menuListMaximumEntries,
		)
	}
	needed := 4 + count*MenuEntryPayloadSize
	if needed > MaxPayload || len(payload) < needed {
		return MenuListPage{}, fmt.Errorf(
			"MENU_LIST count %d needs %d bytes, payload has %d",
			count,
			needed,
			len(payload),
		)
	}
	page := MenuListPage{
		Total: payload[1], NextCursor: payload[2],
		Entries: make([]MenuEntry, 0, count),
	}
	for index := 0; index < count; index++ {
		offset := 4 + index*MenuEntryPayloadSize
		page.Entries = append(page.Entries, MenuEntry{
			ID: payload[offset], Mode: payload[offset+1],
			Label: string(payload[offset+2 : offset+6]),
		})
	}
	return page, nil
}

const MenuLayoutSchema byte = 2

// MenuLayout is the compact board-owned menu visibility and rank record. Rank
// is the index in Order; VisibleMask bits are keyed by stable page ID.
type MenuLayout struct {
	Schema      byte   `json:"schema"`
	VisibleMask uint16 `json:"visible_mask"`
	Order       []byte `json:"order"`
}

// ParseMenuLayout validates the exact schema shared with the AVR. Stable IDs
// never change; at least one page must remain visible. If a current/default
// page is hidden, firmware falls back to the first visible persisted rank.
func ParseMenuLayout(payload []byte) (MenuLayout, error) {
	if len(payload) < 5 {
		return MenuLayout{}, fmt.Errorf("MENU_LAYOUT payload is %d bytes, need at least 5", len(payload))
	}
	if payload[0] != MenuLayoutSchema {
		return MenuLayout{}, fmt.Errorf("unsupported menu-layout schema %d", payload[0])
	}
	count := int(payload[1])
	if count < 1 || count > 16 {
		return MenuLayout{}, fmt.Errorf("MENU_LAYOUT count %d is outside 1..16", count)
	}
	expectedLength := 4 + (count+1)/2
	if len(payload) != expectedLength {
		return MenuLayout{}, fmt.Errorf("MENU_LAYOUT schema %d count %d requires exactly %d bytes, payload has %d", payload[0], count, expectedLength, len(payload))
	}
	mask := binary.LittleEndian.Uint16(payload[2:4])
	allowedMask := uint16(0xFFFF)
	if count < 16 {
		allowedMask = uint16(1<<count) - 1
	}
	if extra := mask &^ allowedMask; extra != 0 {
		return MenuLayout{}, fmt.Errorf("MENU_LAYOUT visibility sets out-of-range bits 0x%04X", extra)
	}
	if mask == 0 {
		return MenuLayout{}, fmt.Errorf("MENU_LAYOUT must keep at least one page visible")
	}
	order := make([]byte, count)
	for rank := range order {
		packed := payload[4+rank/2]
		if rank&1 == 0 {
			order[rank] = packed & 0x0F
		} else {
			order[rank] = packed >> 4
		}
	}
	if count&1 != 0 && payload[len(payload)-1]>>4 != 0x0F {
		return MenuLayout{}, fmt.Errorf("MENU_LAYOUT odd-count padding nibble must be 0xF")
	}
	seen := make([]bool, count)
	for rank, id := range order {
		if int(id) >= count {
			return MenuLayout{}, fmt.Errorf("MENU_LAYOUT rank %d contains unknown page ID %d", rank, id)
		}
		if seen[id] {
			return MenuLayout{}, fmt.Errorf("MENU_LAYOUT repeats page ID %d", id)
		}
		seen[id] = true
	}
	return MenuLayout{Schema: payload[0], VisibleMask: mask, Order: order}, nil
}

func EncodeMenuLayout(layout MenuLayout) ([]byte, error) {
	count := len(layout.Order)
	if count < 1 || count > 16 {
		return nil, fmt.Errorf("MENU_LAYOUT count %d is outside 1..16", count)
	}
	payload := make([]byte, 4+(count+1)/2)
	payload[0] = MenuLayoutSchema
	payload[1] = byte(count)
	binary.LittleEndian.PutUint16(payload[2:4], layout.VisibleMask)
	for rank, id := range layout.Order {
		index := 4 + rank/2
		if rank&1 == 0 {
			payload[index] = id & 0x0F
		} else {
			payload[index] |= id << 4
		}
	}
	if count&1 != 0 {
		payload[len(payload)-1] |= 0xF0
	}
	parsed, err := ParseMenuLayout(payload)
	if err != nil {
		return nil, err
	}
	if parsed.VisibleMask != layout.VisibleMask {
		return nil, fmt.Errorf("MENU_LAYOUT mask did not round-trip")
	}
	return payload, nil
}

type RFEntry struct {
	ID          byte   `json:"id"`
	Code        uint32 `json:"code"`
	Bits        byte   `json:"bits"`
	Protocol    byte   `json:"protocol"`
	PulseUS     uint16 `json:"pulse_us"`
	ActionKind  byte   `json:"action_kind"`
	ActionValue byte   `json:"action_value"`
	Behavior    byte   `json:"behavior"`
}

type RFEntriesPage struct {
	Total      byte      `json:"total"`
	NextCursor byte      `json:"next_cursor"`
	Entries    []RFEntry `json:"entries"`
}

const rfEntriesMaximumEntries = (MaxPayload - 4) / RFEntryPayloadSize

func ParseRFEntries(payload []byte) (RFEntriesPage, error) {
	if len(payload) < 4 {
		return RFEntriesPage{}, fmt.Errorf("RF_ENTRIES payload is %d bytes, need at least 4", len(payload))
	}
	if payload[0] != RFEntriesSchema {
		return RFEntriesPage{}, fmt.Errorf("unsupported RF entries schema %d", payload[0])
	}
	count := int(payload[3])
	if count > rfEntriesMaximumEntries {
		return RFEntriesPage{}, fmt.Errorf(
			"RF_ENTRIES count %d exceeds protocol maximum %d",
			count,
			rfEntriesMaximumEntries,
		)
	}
	needed := 4 + count*RFEntryPayloadSize
	if needed > MaxPayload || len(payload) < needed {
		return RFEntriesPage{}, fmt.Errorf(
			"RF_ENTRIES count %d needs %d bytes, payload has %d",
			count,
			needed,
			len(payload),
		)
	}
	page := RFEntriesPage{
		Total: payload[1], NextCursor: payload[2],
		Entries: make([]RFEntry, 0, count),
	}
	for index := 0; index < count; index++ {
		offset := 4 + index*RFEntryPayloadSize
		page.Entries = append(page.Entries, RFEntry{
			ID:          payload[offset],
			Code:        binary.LittleEndian.Uint32(payload[offset+1 : offset+5]),
			Bits:        payload[offset+5],
			Protocol:    payload[offset+6],
			PulseUS:     binary.LittleEndian.Uint16(payload[offset+7 : offset+9]),
			ActionKind:  payload[offset+9],
			ActionValue: payload[offset+10],
			Behavior:    payload[offset+11],
		})
	}
	return page, nil
}
