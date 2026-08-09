// Package hostui provides optional desktop integration for the PC host. It is
// deliberately independent from appconfig so configuration remains owned by
// the host backend rather than the presentation layer.
package hostui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrUnsupported = errors.New("desktop integration is unsupported on this platform")

type Modifier uint32

const (
	ModifierAlt Modifier = 1 << iota
	ModifierControl
	ModifierShift
	ModifierWindows
	ModifierNoRepeat Modifier = 0x4000
)

type Accelerator struct {
	Modifiers  Modifier `json:"modifiers"`
	VirtualKey uint32   `json:"virtual_key"`
	Canonical  string   `json:"canonical"`
}

type HotkeyBinding struct {
	Name        string `json:"name"`
	Accelerator string `json:"accelerator"`
	Command     string `json:"command"`
}

type HotkeyEvent struct {
	Binding HotkeyBinding `json:"binding"`
	At      time.Time     `json:"at"`
}

type HotkeyStatus struct {
	Supported bool            `json:"supported"`
	Running   bool            `json:"running"`
	Bindings  []HotkeyBinding `json:"bindings,omitempty"`
	LastError string          `json:"last_error,omitempty"`
	LastEvent *HotkeyEvent    `json:"last_event,omitempty"`
}

type HotkeyRegistrar interface {
	Start(context.Context, []HotkeyBinding, func(HotkeyEvent)) error
	Stop() error
	Status() HotkeyStatus
}

func NewHotkeyRegistrar() HotkeyRegistrar { return newPlatformHotkeyRegistrar() }

func ParseAccelerator(value string) (Accelerator, error) {
	parts := strings.Split(value, "+")
	if len(parts) == 0 {
		return Accelerator{}, errors.New("hotkey accelerator is empty")
	}
	var modifiers Modifier
	var key uint32
	var keyName string
	seen := make(map[string]bool)
	for _, raw := range parts {
		part := strings.ToUpper(strings.TrimSpace(raw))
		if part == "" || seen[part] {
			return Accelerator{}, fmt.Errorf("invalid or duplicate accelerator component %q", raw)
		}
		seen[part] = true
		switch part {
		case "ALT", "OPTION":
			modifiers |= ModifierAlt
		case "CTRL", "CONTROL":
			modifiers |= ModifierControl
		case "SHIFT":
			modifiers |= ModifierShift
		case "WIN", "WINDOWS", "META", "SUPER":
			modifiers |= ModifierWindows
		default:
			if key != 0 {
				return Accelerator{}, errors.New("accelerator must contain exactly one non-modifier key")
			}
			parsed, canonical, err := parseVirtualKey(part)
			if err != nil {
				return Accelerator{}, err
			}
			key, keyName = parsed, canonical
		}
	}
	if key == 0 {
		return Accelerator{}, errors.New("accelerator has no key")
	}
	if modifiers == 0 && !(key >= 0x70 && key <= 0x87) {
		return Accelerator{}, errors.New("bare global hotkeys are allowed only for F1..F24")
	}
	modifiers |= ModifierNoRepeat
	var canonical []string
	if modifiers&ModifierControl != 0 {
		canonical = append(canonical, "Ctrl")
	}
	if modifiers&ModifierAlt != 0 {
		canonical = append(canonical, "Alt")
	}
	if modifiers&ModifierShift != 0 {
		canonical = append(canonical, "Shift")
	}
	if modifiers&ModifierWindows != 0 {
		canonical = append(canonical, "Win")
	}
	canonical = append(canonical, keyName)
	return Accelerator{Modifiers: modifiers, VirtualKey: key, Canonical: strings.Join(canonical, "+")}, nil
}

func parseVirtualKey(value string) (uint32, string, error) {
	if len(value) == 1 {
		character := value[0]
		if (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			return uint32(character), value, nil
		}
	}
	if strings.HasPrefix(value, "F") {
		number, err := strconv.Atoi(strings.TrimPrefix(value, "F"))
		if err == nil && number >= 1 && number <= 24 {
			return uint32(0x70 + number - 1), fmt.Sprintf("F%d", number), nil
		}
	}
	named := map[string]uint32{
		"BACKSPACE": 0x08, "TAB": 0x09, "ENTER": 0x0D, "ESC": 0x1B,
		"ESCAPE": 0x1B, "SPACE": 0x20, "PAGEUP": 0x21, "PAGEDOWN": 0x22,
		"END": 0x23, "HOME": 0x24, "LEFT": 0x25, "UP": 0x26,
		"RIGHT": 0x27, "DOWN": 0x28, "INSERT": 0x2D, "DELETE": 0x2E,
		"VOLUME_MUTE": 0xAD, "VOLUME_DOWN": 0xAE, "VOLUME_UP": 0xAF,
		"MEDIA_NEXT": 0xB0, "MEDIA_PREVIOUS": 0xB1, "MEDIA_STOP": 0xB2,
		"MEDIA_PLAY_PAUSE": 0xB3,
	}
	if key, ok := named[value]; ok {
		return key, value, nil
	}
	return 0, "", fmt.Errorf("unknown hotkey key %q", value)
}

func validateBindings(bindings []HotkeyBinding) ([]Accelerator, error) {
	if len(bindings) > 64 {
		return nil, errors.New("at most 64 global hotkeys may be registered")
	}
	accelerators := make([]Accelerator, len(bindings))
	names := make(map[string]bool)
	keys := make(map[string]bool)
	for index, binding := range bindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" || names[strings.ToLower(name)] {
			return nil, fmt.Errorf("hotkey %d has an empty or duplicate name", index)
		}
		names[strings.ToLower(name)] = true
		if strings.TrimSpace(binding.Command) == "" {
			return nil, fmt.Errorf("hotkey %q has no command", binding.Name)
		}
		accelerator, err := ParseAccelerator(binding.Accelerator)
		if err != nil {
			return nil, fmt.Errorf("hotkey %q: %w", binding.Name, err)
		}
		key := fmt.Sprintf("%08X:%08X", accelerator.Modifiers, accelerator.VirtualKey)
		if keys[key] {
			return nil, fmt.Errorf("hotkey %q duplicates another accelerator", binding.Name)
		}
		keys[key] = true
		accelerators[index] = accelerator
	}
	return accelerators, nil
}
