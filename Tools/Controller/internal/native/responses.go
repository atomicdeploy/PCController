package native

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

const (
	BoardKindPCController   byte = 1
	SettingsSchemaLegacy    byte = 1
	SettingsSchema          byte = 2
	IdentitySchemaLegacy    byte = 1
	IdentitySchema          byte = 2
	IdentitySchemaCompact   byte = 3
	RFEntriesSchema         byte = 1
	MenuListSchema          byte = 1
	TemperatureSchema       byte = 1
	StatusPayloadSizeLegacy      = 43
	StatusPayloadSize            = 48
	RFEntryPayloadSize           = 12
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
	// CapabilityHostMenuOverlay gates the volatile PC-owned directory/content
	// protocol. Current AVR builds may omit it when the feature does not fit;
	// callers must retain built-in flash labels and disable live host nodes.
	CapabilityHostMenuOverlay uint32 = 1 << 24
)

const StatusBuzzerBusy uint16 = 0x1000

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

const TemperatureEntryPayloadSize = 11

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
		panel.HostEditableValue = uint16(payload[45]) | uint16(payload[46]&0x0F)<<8
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
)

// EventBoot and EventFault are compatibility names for the reset telemetry
// event. Firmware wire type 7 is [type, MCUSR cause, reset count LE u32].
const (
	EventBoot  = EventReset
	EventFault = EventReset
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
)

type Hello struct {
	FirmwareMajor  byte   `json:"firmware_major"`
	FirmwareMinor  byte   `json:"firmware_minor"`
	FirmwarePatch  byte   `json:"firmware_patch"`
	BoardKind      byte   `json:"board_kind"`
	Capabilities   uint32 `json:"capabilities"`
	Name           string `json:"name"`
	IdentitySchema byte   `json:"identity_schema"`
	BuildHash      uint32 `json:"build_hash"`
	BuildDate      string `json:"build_date,omitempty"`
	BuildTime      string `json:"build_time,omitempty"`
	BuildTimestamp uint32 `json:"build_timestamp_packed,omitempty"`
	BuildStamp     string `json:"build_timestamp,omitempty"`
}

func ParseHello(payload []byte) (Hello, error) {
	// Schema 3 is the current flash-compact identity: it intentionally drops
	// semantic firmware versions and the repeated name in favor of hash/time.
	if len(payload) == 14 && payload[0] == IdentitySchemaCompact {
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
	if len(payload) < 9 {
		return Hello{}, fmt.Errorf("HELLO payload is %d bytes, need at least 9", len(payload))
	}
	nameLength := int(payload[8])
	if len(payload) < 9+nameLength {
		return Hello{}, fmt.Errorf(
			"HELLO name length is %d but payload has %d bytes",
			nameLength,
			len(payload),
		)
	}
	hello := Hello{
		FirmwareMajor: payload[0],
		FirmwareMinor: payload[1],
		FirmwarePatch: payload[2],
		BoardKind:     payload[3],
		Capabilities:  binary.LittleEndian.Uint32(payload[4:8]),
		Name:          string(payload[9 : 9+nameLength]),
	}
	extension := payload[9+nameLength:]
	if len(extension) == 0 {
		return hello, nil
	}
	hello.IdentitySchema = extension[0]
	switch hello.IdentitySchema {
	case IdentitySchemaLegacy:
		const legacyPayloadSize = 1 + 4 + 11 + 8
		if len(extension) < legacyPayloadSize {
			return Hello{}, fmt.Errorf(
				"HELLO schema-1 identity extension is %d bytes, need %d",
				len(extension),
				legacyPayloadSize,
			)
		}
		hello.BuildHash = binary.LittleEndian.Uint32(extension[1:5])
		hello.BuildDate = string(extension[5:16])
		hello.BuildTime = string(extension[16:24])
	case IdentitySchema:
		const packedPayloadSize = 1 + 4 + 4
		if len(extension) < packedPayloadSize {
			return Hello{}, fmt.Errorf(
				"HELLO schema-2 identity extension is %d bytes, need %d",
				len(extension),
				packedPayloadSize,
			)
		}
		hello.BuildHash = binary.LittleEndian.Uint32(extension[1:5])
		hello.BuildTimestamp = binary.LittleEndian.Uint32(extension[5:9])
		stamp, err := FormatBuildTimestamp(hello.BuildTimestamp)
		if err != nil {
			return Hello{}, err
		}
		hello.BuildStamp = stamp
	default:
		return Hello{}, fmt.Errorf(
			"unsupported HELLO identity schema %d",
			hello.IdentitySchema,
		)
	}
	return hello, nil
}

// FormatBuildTimestamp decodes the schema-2 date<<16|time value as YYMMDDHHMMSS.
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
		return "", fmt.Errorf("invalid schema-2 build timestamp 0x%08X", packed)
	}
	value := time.Date(year, month, day, hour, minute, second, 0, time.UTC)
	if value.Year() != year || value.Month() != month || value.Day() != day {
		return "", fmt.Errorf("invalid schema-2 build calendar date 0x%08X", packed)
	}
	return value.Format("060102150405"), nil
}

func (hello Hello) IsPCController() bool {
	return hello.BoardKind == BoardKindPCController && hello.Name == "PCController"
}

func ParseSettings(payload []byte) (Settings, error) {
	if len(payload) < 10 {
		return Settings{}, fmt.Errorf("SETTINGS payload is %d bytes, need 10", len(payload))
	}
	if payload[0] != SettingsSchemaLegacy && payload[0] != SettingsSchema {
		return Settings{}, fmt.Errorf("unsupported settings schema %d", payload[0])
	}
	settings := Settings{
		Flags:             payload[1],
		LightMode:         payload[2],
		OnBrightness:      payload[3],
		OffBrightness:     payload[4],
		DisplayBrightness: payload[5],
		StatusBrightness:  payload[6],
		PWMBootMode:       payload[7],
		StreamPeriodMS:    binary.LittleEndian.Uint16(payload[8:10]),
	}
	if payload[0] == SettingsSchema {
		if len(payload) < 12 {
			return Settings{}, fmt.Errorf("SETTINGS schema 2 payload is %d bytes, need 12", len(payload))
		}
		settings.DefaultPage = payload[10]
		settings.ExtendedFlags = payload[11]
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
	RawInputs      byte   `json:"raw_inputs"`
	ActiveKeys     byte   `json:"active_keys"`
	ActiveRelays   byte   `json:"active_relays"`
	MenuPage       byte   `json:"menu_page"`
	ProgramMode    byte   `json:"program_mode"`
	DoorOpen       bool   `json:"door_open"`
	BluetoothState byte   `json:"bluetooth_audio_state"`
	PWMMode        byte   `json:"pwm_mode"`
	PWMChannel     byte   `json:"pwm_channel"`
	PWMValue       uint16 `json:"pwm_value"`
	LCDAddress     byte   `json:"lcd_address"`
	PWMErrors      byte   `json:"pwm_errors"`
	FramingErrors  uint16 `json:"framing_errors"`
	CRCErrors      uint16 `json:"crc_errors"`
	ResetCause     byte   `json:"reset_cause"`
	ResetCount     uint32 `json:"reset_count"`
}

func ParseStatus(payload []byte) (Status, error) {
	if len(payload) < StatusPayloadSizeLegacy {
		return Status{}, fmt.Errorf(
			"STATUS payload is %d bytes, need at least %d",
			len(payload),
			StatusPayloadSizeLegacy,
		)
	}
	if len(payload) > StatusPayloadSizeLegacy && len(payload) < StatusPayloadSize {
		return Status{}, fmt.Errorf(
			"STATUS payload is %d bytes, need legacy %d or current %d",
			len(payload),
			StatusPayloadSizeLegacy,
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
		PWMMode:        payload[33],
		PWMChannel:     payload[34],
		PWMValue:       binary.LittleEndian.Uint16(payload[35:37]),
		LCDAddress:     payload[37],
		PWMErrors:      payload[38],
		FramingErrors:  binary.LittleEndian.Uint16(payload[39:41]),
		CRCErrors:      binary.LittleEndian.Uint16(payload[41:43]),
	}
	if len(payload) >= StatusPayloadSize {
		status.ResetCause = payload[43]
		status.ResetCount = binary.LittleEndian.Uint32(payload[44:48])
	}
	return status, nil
}

type PWMValues struct {
	Mode            byte       `json:"mode"`
	SelectedChannel byte       `json:"selected_channel"`
	Values          [16]uint16 `json:"values"`
}

func ParsePWMValues(payload []byte) (PWMValues, error) {
	if len(payload) < 34 {
		return PWMValues{}, fmt.Errorf("PWM_VALUES payload is %d bytes, need 34", len(payload))
	}
	result := PWMValues{Mode: payload[0], SelectedChannel: payload[1]}
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
	Type         byte         `json:"type"`
	Key          byte         `json:"key,omitempty"`
	Gesture      byte         `json:"gesture,omitempty"`
	Source       byte         `json:"source,omitempty"`
	SourceID     byte         `json:"source_id,omitempty"`
	DoorOpen     bool         `json:"door_open,omitempty"`
	Bluetooth    byte         `json:"bluetooth,omitempty"`
	PWMChannel   byte         `json:"pwm_channel,omitempty"`
	RFID         byte         `json:"rf_id,omitempty"`
	MacroState   byte         `json:"macro_state,omitempty"`
	MacroID      byte         `json:"macro_id,omitempty"`
	ResetCause   byte         `json:"reset_cause,omitempty"`
	ResetCount   uint32       `json:"reset_count,omitempty"`
	RFCode       uint32       `json:"rf_code,omitempty"`
	RFBits       byte         `json:"rf_bits,omitempty"`
	RFProtocol   byte         `json:"rf_protocol,omitempty"`
	RFPulseUS    uint16       `json:"rf_pulse_us,omitempty"`
	RFLearnedID  byte         `json:"rf_learned_id,omitempty"`
	RFLearnState byte         `json:"rf_learning_state,omitempty"`
	RFLearnCount byte         `json:"rf_learning_count,omitempty"`
	RelayMask    byte         `json:"relay_mask,omitempty"`
	DeviceMicros uint32       `json:"device_micros,omitempty"`
	Timed        bool         `json:"timed,omitempty"`
	Macro        *MacroStatus `json:"macro,omitempty"`
	Raw          []byte       `json:"raw,omitempty"`
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
		if len(payload) != 3 {
			return DeviceEvent{}, fmt.Errorf(
				"RF learning EVENT is %d bytes, need exactly 3",
				len(payload),
			)
		}
		event.RFLearnState, event.RFLearnCount = payload[1], payload[2]
	case EventRelay:
		if len(payload) != 2 {
			return DeviceEvent{}, fmt.Errorf(
				"relay EVENT is %d bytes, need exactly 2",
				len(payload),
			)
		}
		event.RelayMask = payload[1]
	}
	return event, nil
}

const MenuEntryPayloadSize = 6

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

const (
	MenuLayoutLegacySchema byte = 1
	MenuLayoutSchema       byte = 2
)

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
	if payload[0] != MenuLayoutLegacySchema && payload[0] != MenuLayoutSchema {
		return MenuLayout{}, fmt.Errorf("unsupported menu-layout schema %d", payload[0])
	}
	count := int(payload[1])
	if count < 1 || count > 16 {
		return MenuLayout{}, fmt.Errorf("MENU_LAYOUT count %d is outside 1..16", count)
	}
	expectedLength := 4 + count
	if payload[0] == MenuLayoutSchema {
		expectedLength = 4 + (count+1)/2
	}
	if len(payload) != expectedLength {
		return MenuLayout{}, fmt.Errorf("MENU_LAYOUT schema %d count %d requires exactly %d bytes, payload has %d", payload[0], count, expectedLength, len(payload))
	}
	mask := uint16(payload[2]) | uint16(payload[3])<<8
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
	if payload[0] == MenuLayoutLegacySchema {
		copy(order, payload[4:])
	} else {
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
	payload := make([]byte, 4+(count+1)/2)
	payload[0] = MenuLayoutSchema
	payload[1] = byte(count)
	payload[2] = byte(layout.VisibleMask)
	payload[3] = byte(layout.VisibleMask >> 8)
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

func ParseRFEntries(payload []byte) (RFEntriesPage, error) {
	if len(payload) < 4 {
		return RFEntriesPage{}, fmt.Errorf("RF_ENTRIES payload is %d bytes, need at least 4", len(payload))
	}
	if payload[0] != RFEntriesSchema {
		return RFEntriesPage{}, fmt.Errorf("unsupported RF entries schema %d", payload[0])
	}
	count := int(payload[3])
	needed := 4 + count*RFEntryPayloadSize
	if len(payload) < needed {
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
