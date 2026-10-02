package appconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	BoardModeUnconfigured     = "unconfigured"
	BoardModeOrdinaryRelays   = "ordinary-relays"
	BoardModeCinemaSeatMotion = "cinema-seat-motion"
	MaxBoardProfiles          = 32
)

// BoardProfile is host-owned because the same controller PCB can be wired for
// unrelated installations. It never changes firmware EEPROM implicitly.
type BoardProfile struct {
	Key             string                            `json:"key"`
	Mode            string                            `json:"mode"`
	ExposeRawRelays bool                              `json:"expose_raw_relays,omitempty"`
	Presentation    map[string]PeripheralPresentation `json:"presentation,omitempty"`
}

// PeripheralPresentation is mutable operator vocabulary attached to a stable
// peripheral/control key. Empty optional fields mean no override.
type PeripheralPresentation struct {
	Name   string `json:"name,omitempty"`
	Icon   string `json:"icon,omitempty"`
	Group  string `json:"group,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
	Locked bool   `json:"locked,omitempty"`
}

// BoardIdentity describes the strongest currently available physical-device
// identity. USB instance and port identities are deliberately marked unstable
// so clients never mistake an adapter path for provisioned board identity.
type BoardIdentity struct {
	Value  string
	Source string
	Stable bool
}

func ResolveBoardIdentity(serialNumber, instanceID, port string) BoardIdentity {
	if value := strings.TrimSpace(serialNumber); value != "" {
		return BoardIdentity{Value: "serial:" + strings.ToLower(value), Source: "usb-serial", Stable: true}
	}
	if value := strings.TrimSpace(instanceID); value != "" {
		return BoardIdentity{Value: "instance:" + strings.ToLower(value), Source: "usb-instance", Stable: false}
	}
	if value := strings.TrimSpace(port); value != "" {
		return BoardIdentity{Value: "port:" + strings.ToLower(value), Source: "serial-port", Stable: false}
	}
	return BoardIdentity{Source: "unavailable", Stable: false}
}

func NormalizeBoardMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return BoardModeUnconfigured
	}
	return value
}

func (value Config) validateBoardProfiles() error {
	if len(value.BoardProfiles) > MaxBoardProfiles {
		return fmt.Errorf("board_profiles may contain at most %d entries", MaxBoardProfiles)
	}
	for identity, profile := range value.BoardProfiles {
		identity = strings.TrimSpace(identity)
		if identity == "" || len(identity) > 512 || !printableASCII(identity) {
			return fmt.Errorf("board_profiles identity must be 1..512 printable ASCII bytes")
		}
		key := strings.TrimSpace(profile.Key)
		if key == "" || len(key) > 64 || !profileToken(key) {
			return fmt.Errorf("board_profiles[%q].key must be 1..64 lower-case letters, digits, dot, dash, or underscore", identity)
		}
		switch NormalizeBoardMode(profile.Mode) {
		case BoardModeOrdinaryRelays, BoardModeCinemaSeatMotion:
		default:
			return fmt.Errorf("board_profiles[%q].mode must be ordinary-relays or cinema-seat-motion", identity)
		}
		if len(profile.Presentation) > MaxPeripheralNames {
			return fmt.Errorf("board_profiles[%q].presentation may contain at most %d entries", identity, MaxPeripheralNames)
		}
		for controlKey, presentation := range profile.Presentation {
			if controlKey = strings.TrimSpace(controlKey); controlKey == "" || len(controlKey) > 64 || !profileToken(controlKey) {
				return fmt.Errorf("board_profiles[%q].presentation key must be 1..64 lower-case letters, digits, dot, dash, or underscore", identity)
			}
			for field, text := range map[string]string{"name": presentation.Name, "icon": presentation.Icon, "group": presentation.Group} {
				text = strings.TrimSpace(text)
				if utf8.RuneCountInString(text) > 64 || (text != "" && !printableText(text)) {
					return fmt.Errorf("board_profiles[%q].presentation[%q].%s must be at most 64 printable characters", identity, controlKey, field)
				}
			}
		}
	}
	return nil
}

func profileToken(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
			character == '.' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

// BoardProfileRevision returns a deterministic opaque revision suitable for
// optimistic concurrency. Callers must never parse or increment it.
func BoardProfileRevision(identity string, profile BoardProfile, legacyNames map[string]string) string {
	names := make([]string, 0, len(legacyNames))
	for key := range legacyNames {
		names = append(names, key)
	}
	sort.Strings(names)
	canonical := struct {
		Identity string       `json:"identity"`
		Profile  BoardProfile `json:"profile"`
		Names    [][2]string  `json:"names,omitempty"`
	}{Identity: identity, Profile: profile}
	for _, key := range names {
		canonical.Names = append(canonical.Names, [2]string{key, legacyNames[key]})
	}
	content, _ := json.Marshal(canonical)
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:12])
}

func cloneBoardProfiles(source map[string]BoardProfile) map[string]BoardProfile {
	result := make(map[string]BoardProfile, len(source))
	for identity, profile := range source {
		copyProfile := profile
		copyProfile.Presentation = make(map[string]PeripheralPresentation, len(profile.Presentation))
		for key, presentation := range profile.Presentation {
			copyProfile.Presentation[key] = presentation
		}
		result[identity] = copyProfile
	}
	return result
}
