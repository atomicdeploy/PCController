package native

import (
	"encoding/binary"
	"fmt"
)

const (
	AutomationAllocateID byte = 0xFF
	AutomationEnabled    byte = 1 << 0
)

// Automation event kinds are intentionally bounded to signals the board can
// produce without host participation. EventMask zero is a wildcard; otherwise
// an event matches when (value & mask) == (EventValue & mask).
const (
	AutomationEventDoor byte = iota + 1
	AutomationEventBluetooth
	AutomationEventHost
	AutomationEventRelay
	AutomationEventLearnedRF
	AutomationEventKey
	AutomationEventAlert
	AutomationEventBoot
)

const (
	AutomationHostDisconnected byte = iota
	AutomationHostConnected
)

// Automation actions only name existing safety-aware firmware paths. Motion
// start is deliberately absent: an offline rule may stop motion, never start it.
const (
	AutomationActionSafeStop byte = iota + 1
	AutomationActionMotionStop
	AutomationActionRelay
	AutomationActionPWM
	AutomationActionStatusCue
	AutomationActionBuzzer
	AutomationActionRFTransmit
	AutomationActionHostMacroRequest
)

const (
	AutomationRelayOff byte = iota
	AutomationRelayOn
	AutomationRelayToggle
)

// AutomationRecord is both the host API model and the semantic portion of one
// EEPROM record. The wire format prepends ID and omits the EEPROM-only CRC-8:
// id, flags, event kind/value/mask, action kind/target, value LE16, extra LE16,
// options. Options are reserved and must currently be zero.
type AutomationRecord struct {
	ID           byte   `json:"id"`
	Flags        byte   `json:"flags"`
	EventKind    byte   `json:"event_kind"`
	EventValue   byte   `json:"event_value"`
	EventMask    byte   `json:"event_mask"`
	ActionKind   byte   `json:"action_kind"`
	ActionTarget byte   `json:"action_target"`
	Value        uint16 `json:"value"`
	Extra        uint16 `json:"extra"`
	Options      byte   `json:"options"`
}

func (record AutomationRecord) Enabled() bool {
	return record.Flags&AutomationEnabled != 0
}

// ValidateAutomationRecord applies the same compact-domain and safety bounds
// as firmware. ID 0xFF is accepted only as the transactional allocate sentinel.
func ValidateAutomationRecord(record AutomationRecord) error {
	if record.ID >= AutomationCapacity && record.ID != AutomationAllocateID {
		return fmt.Errorf("automation id %d is outside 0..%d", record.ID, AutomationCapacity-1)
	}
	if record.Flags&^AutomationEnabled != 0 {
		return fmt.Errorf("automation flags 0x%02X exceed mask 0x%02X", record.Flags, AutomationEnabled)
	}
	if record.EventKind < AutomationEventDoor || record.EventKind > AutomationEventBoot {
		return fmt.Errorf("automation event kind %d is outside %d..%d", record.EventKind, AutomationEventDoor, AutomationEventBoot)
	}
	switch record.EventKind {
	case AutomationEventDoor, AutomationEventHost:
		if record.EventValue > 1 {
			return fmt.Errorf("automation event value %d is outside 0..1", record.EventValue)
		}
	case AutomationEventBluetooth:
		if record.EventValue > 2 {
			return fmt.Errorf("automation Bluetooth state %d is outside 0..2", record.EventValue)
		}
	case AutomationEventLearnedRF:
		if record.EventValue >= 20 {
			return fmt.Errorf("automation learned-RF id %d is outside 0..19", record.EventValue)
		}
	case AutomationEventKey:
		if record.EventValue>>4 > MenuIncrease || record.EventValue&0x0F > KeyEventUp {
			return fmt.Errorf("automation key event 0x%02X is outside action 0..3 / gesture 0..6", record.EventValue)
		}
	case AutomationEventAlert:
		if kind := record.EventValue >> 1; kind < AlertFault || kind > AlertHot {
			return fmt.Errorf("automation alert event 0x%02X has kind outside 1..2", record.EventValue)
		}
	case AutomationEventBoot:
		if record.EventValue != 0 {
			return fmt.Errorf("automation boot event value must be zero")
		}
	}
	if record.ActionKind < AutomationActionSafeStop || record.ActionKind > AutomationActionHostMacroRequest {
		return fmt.Errorf("automation action kind %d is outside %d..%d", record.ActionKind, AutomationActionSafeStop, AutomationActionHostMacroRequest)
	}
	if record.Options != 0 {
		return fmt.Errorf("automation options 0x%02X are reserved", record.Options)
	}
	if record.EventKind == AutomationEventHost &&
		(record.ActionKind == AutomationActionRelay || record.ActionKind == AutomationActionPWM) {
		return fmt.Errorf("automation host event cannot drive relay or PWM outputs")
	}
	switch record.ActionKind {
	case AutomationActionSafeStop:
		if record.ActionTarget != 0 || record.Value != 0 || record.Extra != 0 {
			return fmt.Errorf("automation safe-stop target, value, and extra must be zero")
		}
	case AutomationActionMotionStop:
		if (record.ActionTarget > 1 && record.ActionTarget != 0xFF) || record.Value != 0 || record.Extra != 0 {
			return fmt.Errorf("automation motion-stop requires side 0..1 or 0xFF for all, and zero value/extra")
		}
	case AutomationActionRelay:
		if record.ActionTarget > 7 || record.Value > uint16(AutomationRelayToggle) || record.Extra != 0 {
			return fmt.Errorf("automation relay requires target 0..7, value 0..2, and zero extra")
		}
	case AutomationActionPWM:
		if record.ActionTarget > 10 || record.Value > 4095 || record.Extra != 0 {
			return fmt.Errorf("automation PWM requires user channel 0..10, value 0..4095, and zero extra")
		}
	case AutomationActionStatusCue:
		if record.ActionTarget < 1 || record.ActionTarget > 8 || record.Value == 0 || record.Extra != 0 {
			return fmt.Errorf("automation status cue requires cue 1..8, non-zero duration, and zero extra")
		}
	case AutomationActionBuzzer:
		if record.Value == 0 || record.Extra == 0 {
			return fmt.Errorf("automation buzzer requires non-zero frequency and duration")
		}
	case AutomationActionRFTransmit:
		if record.ActionTarget >= 20 || record.Value != 0 || record.Extra != 0 {
			return fmt.Errorf("automation RF transmit requires learned id 0..19 and zero value/extra")
		}
	case AutomationActionHostMacroRequest:
		if record.Value != 0 || record.Extra != 0 {
			return fmt.Errorf("automation host macro request requires zero value/extra")
		}
	}
	return nil
}

func AutomationListPayload(cursor byte) ([]byte, error) {
	if cursor >= AutomationCapacity {
		return nil, fmt.Errorf("automation cursor %d is outside 0..%d", cursor, AutomationCapacity-1)
	}
	return []byte{cursor}, nil
}

func AutomationPutPayload(record AutomationRecord) ([]byte, error) {
	if err := ValidateAutomationRecord(record); err != nil {
		return nil, err
	}
	return encodeAutomationRecord(record), nil
}

func AutomationRemovePayload(id byte) ([]byte, error) {
	if id >= AutomationCapacity {
		return nil, fmt.Errorf("automation id %d is outside 0..%d", id, AutomationCapacity-1)
	}
	return []byte{id}, nil
}

func AutomationClearPayload() []byte { return nil }

func encodeAutomationRecord(record AutomationRecord) []byte {
	payload := make([]byte, AutomationRecordPayloadSize)
	payload[0] = record.ID
	payload[1] = record.Flags
	payload[2] = record.EventKind
	payload[3] = record.EventValue
	payload[4] = record.EventMask
	payload[5] = record.ActionKind
	payload[6] = record.ActionTarget
	binary.LittleEndian.PutUint16(payload[7:9], record.Value)
	binary.LittleEndian.PutUint16(payload[9:11], record.Extra)
	payload[11] = record.Options
	return payload
}

func parseAutomationRecord(payload []byte, allowAllocate bool) (AutomationRecord, error) {
	if len(payload) < AutomationRecordPayloadSize {
		return AutomationRecord{}, fmt.Errorf("automation record is %d bytes, need %d", len(payload), AutomationRecordPayloadSize)
	}
	record := AutomationRecord{
		ID: payload[0], Flags: payload[1], EventKind: payload[2],
		EventValue: payload[3], EventMask: payload[4], ActionKind: payload[5],
		ActionTarget: payload[6], Value: binary.LittleEndian.Uint16(payload[7:9]),
		Extra: binary.LittleEndian.Uint16(payload[9:11]), Options: payload[11],
	}
	if err := ValidateAutomationRecord(record); err != nil {
		return AutomationRecord{}, err
	}
	if !allowAllocate && record.ID == AutomationAllocateID {
		return AutomationRecord{}, fmt.Errorf("automation response cannot contain allocation id 0xFF")
	}
	return record, nil
}

type AutomationPage struct {
	Schema     byte               `json:"schema"`
	Generation uint16             `json:"generation"`
	Total      byte               `json:"total"`
	NextCursor byte               `json:"next_cursor"`
	Records    []AutomationRecord `json:"records"`
}

const automationMaximumPageRecords = (MaxPayload - 6) / AutomationRecordPayloadSize

func ParseAutomationList(payload []byte) (AutomationPage, error) {
	if len(payload) < 6 {
		return AutomationPage{}, fmt.Errorf("AUTOMATION_LIST payload is %d bytes, need at least 6", len(payload))
	}
	if payload[0] != AutomationSchema {
		return AutomationPage{}, fmt.Errorf("unsupported automation schema %d", payload[0])
	}
	total, next, count := payload[3], payload[4], int(payload[5])
	if total > AutomationCapacity {
		return AutomationPage{}, fmt.Errorf("AUTOMATION_LIST total %d exceeds capacity %d", total, AutomationCapacity)
	}
	if next != 0xFF && next >= AutomationCapacity {
		return AutomationPage{}, fmt.Errorf("AUTOMATION_LIST next cursor %d exceeds capacity", next)
	}
	if count > automationMaximumPageRecords {
		return AutomationPage{}, fmt.Errorf("AUTOMATION_LIST count %d exceeds protocol maximum %d", count, automationMaximumPageRecords)
	}
	if count > int(total) {
		return AutomationPage{}, fmt.Errorf("AUTOMATION_LIST count %d exceeds total %d", count, total)
	}
	needed := 6 + count*AutomationRecordPayloadSize
	if needed > MaxPayload || len(payload) < needed {
		return AutomationPage{}, fmt.Errorf("AUTOMATION_LIST count %d needs %d bytes, payload has %d", count, needed, len(payload))
	}
	page := AutomationPage{
		Schema: payload[0], Generation: binary.LittleEndian.Uint16(payload[1:3]),
		Total: total, NextCursor: next, Records: make([]AutomationRecord, 0, count),
	}
	lastID := -1
	for index := 0; index < count; index++ {
		offset := 6 + index*AutomationRecordPayloadSize
		record, err := parseAutomationRecord(payload[offset:offset+AutomationRecordPayloadSize], false)
		if err != nil {
			return AutomationPage{}, fmt.Errorf("AUTOMATION_LIST record %d: %w", index, err)
		}
		if int(record.ID) <= lastID {
			return AutomationPage{}, fmt.Errorf("AUTOMATION_LIST record ids are not strictly ascending at %d", record.ID)
		}
		lastID = int(record.ID)
		page.Records = append(page.Records, record)
	}
	return page, nil
}

type AutomationRecordReadback struct {
	Schema     byte             `json:"schema"`
	Generation uint16           `json:"generation"`
	Record     AutomationRecord `json:"record"`
}

func ParseAutomationRecord(payload []byte) (AutomationRecordReadback, error) {
	const prefix = 3
	if len(payload) < prefix+AutomationRecordPayloadSize {
		return AutomationRecordReadback{}, fmt.Errorf("AUTOMATION_RECORD payload is %d bytes, need at least %d", len(payload), prefix+AutomationRecordPayloadSize)
	}
	if payload[0] != AutomationSchema {
		return AutomationRecordReadback{}, fmt.Errorf("unsupported automation schema %d", payload[0])
	}
	record, err := parseAutomationRecord(payload[prefix:prefix+AutomationRecordPayloadSize], false)
	if err != nil {
		return AutomationRecordReadback{}, fmt.Errorf("AUTOMATION_RECORD: %w", err)
	}
	return AutomationRecordReadback{
		Schema: payload[0], Generation: binary.LittleEndian.Uint16(payload[1:3]), Record: record,
	}, nil
}
