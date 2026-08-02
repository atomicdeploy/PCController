package native

import (
	"fmt"
	"strings"
)

const (
	HostMenuSchema         byte = 1
	HostMenuMaximumEntries      = 8
	HostMenuRoot           byte = 0xFF
	HostMenuBuiltinLast    byte = 0x0E
	HostMenuCategoryFirst  byte = 0x70
	HostMenuCategoryLast   byte = 0x73
	HostMenuHostFirst      byte = 0x80
	HostMenuHostLast       byte = 0xEF
)

const (
	HostMenuVisible byte = 1 << iota
	HostMenuSelectable
	HostMenuEditable
	HostMenuAction
	HostMenuReadOnly
	HostMenuBuiltinLabelOverride
	HostMenuLiveContent
)

const (
	HostMenuReasonEnter byte = iota
	HostMenuReasonSelectionChanged
	HostMenuReasonRefresh
	HostMenuReasonRetry
	HostMenuReasonDenied
)

const (
	HostMenuPhaseInactive byte = iota
	HostMenuPhaseLoading
	HostMenuPhaseReady
	HostMenuPhaseFailed
)

const (
	HostMenuVisualSteady byte = iota
	HostMenuVisualBlink
	HostMenuVisualEditDim
	HostMenuVisualAlternate
)

// HostMenuDirectoryEntry is a compact volatile node descriptor. Built-in page
// IDs remain immutable and get presentation ranks from cap-23 MENU_LAYOUT;
// host-only siblings sort by their RAM-only ID, so no redundant order byte is
// carried in this SRAM-sensitive cap-24 directory.
type HostMenuDirectoryEntry struct {
	ID     byte `json:"id"`
	Parent byte `json:"parent_id"`
	Flags  byte `json:"flags"`
}

type HostMenuDirectory struct {
	Schema     byte                     `json:"schema"`
	Generation byte                     `json:"generation"`
	Entries    []HostMenuDirectoryEntry `json:"entries"`
}

func EncodeHostMenuDirectory(directory HostMenuDirectory) ([]byte, error) {
	if directory.Schema == 0 {
		directory.Schema = HostMenuSchema
	}
	entryCount := len(directory.Entries)
	if entryCount > HostMenuMaximumEntries {
		return nil, fmt.Errorf("host-menu directory has %d entries, maximum is %d", entryCount, HostMenuMaximumEntries)
	}
	if err := ValidateHostMenuDirectory(directory); err != nil {
		return nil, err
	}
	payload := make([]byte, 3+HostMenuMaximumEntries*3)
	payload = payload[:3+entryCount*3]
	payload[0], payload[1], payload[2] = directory.Schema, directory.Generation, byte(entryCount)
	for index, entry := range directory.Entries {
		offset := 3 + index*3
		payload[offset], payload[offset+1] = entry.ID, entry.Parent
		payload[offset+2] = entry.Flags
	}
	return payload, nil
}

func ParseHostMenuDirectory(payload []byte) (HostMenuDirectory, error) {
	if len(payload) < 3 {
		return HostMenuDirectory{}, fmt.Errorf("HOST_MENU_DIRECTORY payload is %d bytes, need at least 3", len(payload))
	}
	count := int(payload[2])
	if count > HostMenuMaximumEntries {
		return HostMenuDirectory{}, fmt.Errorf("host-menu directory has %d entries, maximum is %d", count, HostMenuMaximumEntries)
	}
	if len(payload) != 3+count*3 {
		return HostMenuDirectory{}, fmt.Errorf("HOST_MENU_DIRECTORY count %d requires exactly %d bytes, payload has %d", count, 3+count*3, len(payload))
	}
	directory := HostMenuDirectory{Schema: payload[0], Generation: payload[1], Entries: make([]HostMenuDirectoryEntry, 0, count)}
	for index := 0; index < count; index++ {
		offset := 3 + index*3
		directory.Entries = append(directory.Entries, HostMenuDirectoryEntry{
			ID: payload[offset], Parent: payload[offset+1], Flags: payload[offset+2],
		})
	}
	if err := ValidateHostMenuDirectory(directory); err != nil {
		return HostMenuDirectory{}, err
	}
	return directory, nil
}

// ValidateHostMenuDirectory rejects partial, ambiguous, cyclic, or orphaned
// graphs before an entire generation can replace the current runtime overlay.
func ValidateHostMenuDirectory(directory HostMenuDirectory) error {
	if directory.Schema != HostMenuSchema {
		return fmt.Errorf("unsupported host-menu directory schema %d", directory.Schema)
	}
	if len(directory.Entries) > HostMenuMaximumEntries {
		return fmt.Errorf("host-menu directory has %d entries, maximum is %d", len(directory.Entries), HostMenuMaximumEntries)
	}
	entries := make(map[byte]HostMenuDirectoryEntry, len(directory.Entries))
	for index, entry := range directory.Entries {
		builtin := entry.ID <= HostMenuBuiltinLast
		host := entry.ID >= HostMenuHostFirst && entry.ID <= HostMenuHostLast
		if !builtin && !host {
			return fmt.Errorf("host-menu entry %d uses reserved ID 0x%02X", index, entry.ID)
		}
		if _, exists := entries[entry.ID]; exists {
			return fmt.Errorf("host-menu directory repeats ID 0x%02X", entry.ID)
		}
		if entry.Flags&HostMenuVisible == 0 {
			return fmt.Errorf("host-menu entry 0x%02X must be visible", entry.ID)
		}
		if entry.Flags&HostMenuReadOnly != 0 && entry.Flags&(HostMenuEditable|HostMenuAction) != 0 {
			return fmt.Errorf("host-menu entry 0x%02X cannot be readonly and editable/action", entry.ID)
		}
		if builtin {
			if entry.Flags&HostMenuBuiltinLabelOverride == 0 {
				return fmt.Errorf("built-in host-menu entry 0x%02X must set label-override flag", entry.ID)
			}
		} else {
			if entry.Flags&HostMenuLiveContent == 0 {
				return fmt.Errorf("host-only menu entry 0x%02X requires live-content flag", entry.ID)
			}
			if entry.Flags&HostMenuBuiltinLabelOverride != 0 {
				return fmt.Errorf("host-only menu entry 0x%02X cannot set built-in override flag", entry.ID)
			}
		}
		entries[entry.ID] = entry
	}
	for _, entry := range directory.Entries {
		if entry.Parent == HostMenuRoot ||
			(entry.Parent >= HostMenuCategoryFirst && entry.Parent <= HostMenuCategoryLast) {
			continue
		}
		if _, ok := entries[entry.Parent]; !ok {
			return fmt.Errorf("host-menu entry 0x%02X has missing parent 0x%02X", entry.ID, entry.Parent)
		}
		seen := map[byte]bool{entry.ID: true}
		parent := entry.Parent
		for parent != HostMenuRoot && !(parent >= HostMenuCategoryFirst && parent <= HostMenuCategoryLast) {
			if seen[parent] {
				return fmt.Errorf("host-menu directory contains a cycle through 0x%02X", parent)
			}
			seen[parent] = true
			parent = entries[parent].Parent
		}
	}
	return nil
}

type HostMenuContent struct {
	Schema     byte   `json:"schema"`
	Generation byte   `json:"generation"`
	ID         byte   `json:"id"`
	Revision   byte   `json:"revision"`
	Flags      byte   `json:"flags"`
	Brightness byte   `json:"brightness"`
	Visual     byte   `json:"visual"`
	Segments   string `json:"segments"`
	LCDLine1   string `json:"lcd_line_1"`
	LCDLine2   string `json:"lcd_line_2"`
}

func EncodeHostMenuContent(content HostMenuContent) ([]byte, error) {
	if content.Schema == 0 {
		content.Schema = HostMenuSchema
	}
	if content.Schema != HostMenuSchema {
		return nil, fmt.Errorf("unsupported host-menu content schema %d", content.Schema)
	}
	if !hostMenuWireID(content.ID) {
		return nil, fmt.Errorf("host-menu content uses reserved ID 0x%02X", content.ID)
	}
	segments, err := fixedHostMenuASCII(content.Segments, 4)
	if err != nil {
		return nil, fmt.Errorf("host-menu segments: %w", err)
	}
	line1, err := fixedHostMenuASCII(content.LCDLine1, 16)
	if err != nil {
		return nil, fmt.Errorf("host-menu LCD line 1: %w", err)
	}
	line2, err := fixedHostMenuASCII(content.LCDLine2, 16)
	if err != nil {
		return nil, fmt.Errorf("host-menu LCD line 2: %w", err)
	}
	if content.Brightness > 7 && content.Brightness != 0xFF {
		return nil, fmt.Errorf("host-menu brightness must be 0..7 or 255 to keep board setting")
	}
	if content.Visual > HostMenuVisualAlternate {
		return nil, fmt.Errorf("host-menu visual %d is outside 0..3", content.Visual)
	}
	payload := make([]byte, 43)
	payload[0], payload[1], payload[2] = content.Schema, content.Generation, content.ID
	payload[3], payload[4] = content.Revision, content.Flags
	payload[5], payload[6] = content.Brightness, content.Visual
	copy(payload[7:11], segments)
	copy(payload[11:27], line1)
	copy(payload[27:43], line2)
	return payload, nil
}

func ParseHostMenuContent(payload []byte) (HostMenuContent, error) {
	if len(payload) != 43 {
		return HostMenuContent{}, fmt.Errorf("HOST_MENU_CONTENT payload is %d bytes, need exactly 43", len(payload))
	}
	if payload[0] != HostMenuSchema || !hostMenuWireID(payload[2]) {
		return HostMenuContent{}, fmt.Errorf("invalid host-menu content schema/ID %d/0x%02X", payload[0], payload[2])
	}
	if payload[5] > 7 && payload[5] != 0xFF {
		return HostMenuContent{}, fmt.Errorf("host-menu brightness %d is invalid", payload[5])
	}
	if payload[6] > HostMenuVisualAlternate {
		return HostMenuContent{}, fmt.Errorf("host-menu visual %d is invalid", payload[6])
	}
	for index := 7; index < len(payload); index++ {
		if payload[index] < 0x20 || payload[index] > 0x7E {
			return HostMenuContent{}, fmt.Errorf("host-menu content byte %d is not printable ASCII", index)
		}
	}
	return HostMenuContent{
		Schema: payload[0], Generation: payload[1], ID: payload[2], Revision: payload[3], Flags: payload[4],
		Brightness: payload[5], Visual: payload[6],
		Segments: strings.TrimRight(string(payload[7:11]), " "),
		LCDLine1: strings.TrimRight(string(payload[11:27]), " "),
		LCDLine2: strings.TrimRight(string(payload[27:43]), " "),
	}, nil
}

type HostMenuState struct {
	Schema     byte `json:"schema"`
	Generation byte `json:"generation"`
	ActiveID   byte `json:"active_id"`
	Phase      byte `json:"phase"`
	Attempt    byte `json:"attempt"`
	Revision   byte `json:"revision"`
}

func ParseHostMenuState(payload []byte) (HostMenuState, error) {
	if len(payload) != 6 || payload[0] != HostMenuSchema {
		return HostMenuState{}, fmt.Errorf("HOST_MENU_STATE payload must be six-byte schema 1")
	}
	if payload[2] != HostMenuRoot && !hostMenuWireID(payload[2]) {
		return HostMenuState{}, fmt.Errorf("HOST_MENU_STATE uses reserved active ID 0x%02X", payload[2])
	}
	if payload[3] > HostMenuPhaseFailed {
		return HostMenuState{}, fmt.Errorf("HOST_MENU_STATE phase %d is invalid", payload[3])
	}
	return HostMenuState{Schema: payload[0], Generation: payload[1], ActiveID: payload[2], Phase: payload[3], Attempt: payload[4], Revision: payload[5]}, nil
}

type HostMenuContentRequest struct {
	Schema     byte `json:"schema"`
	Generation byte `json:"generation"`
	ID         byte `json:"id"`
	Reason     byte `json:"reason"`
	Attempt    byte `json:"attempt"`
}

func ParseHostMenuContentRequest(payload []byte) (HostMenuContentRequest, error) {
	if len(payload) != 5 || payload[0] != HostMenuSchema || !hostMenuWireID(payload[2]) {
		return HostMenuContentRequest{}, fmt.Errorf("HOST_MENU_REQUEST payload must be five-byte schema 1 with a valid ID")
	}
	if payload[3] > HostMenuReasonDenied || payload[4] > 3 {
		return HostMenuContentRequest{}, fmt.Errorf("HOST_MENU_REQUEST reason/attempt %d/%d is invalid", payload[3], payload[4])
	}
	return HostMenuContentRequest{Schema: payload[0], Generation: payload[1], ID: payload[2], Reason: payload[3], Attempt: payload[4]}, nil
}

func hostMenuWireID(id byte) bool {
	return id <= HostMenuBuiltinLast || (id >= HostMenuHostFirst && id <= HostMenuHostLast)
}

func fixedHostMenuASCII(value string, width int) (string, error) {
	if len(value) > width {
		return "", fmt.Errorf("is %d bytes, maximum is %d", len(value), width)
	}
	for _, value := range []byte(value) {
		if value < 0x20 || value > 0x7E {
			return "", fmt.Errorf("must contain printable ASCII")
		}
	}
	return value + strings.Repeat(" ", width-len(value)), nil
}
