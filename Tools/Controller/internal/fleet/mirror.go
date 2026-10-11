package fleet

import (
	"encoding/json"
	"errors"
	"fmt"
)

// MirrorConfig deliberately names board identities independently of port/peer
// aliases. One primary runtime owns each UART; other boards are IPC peers.
type MirrorConfig struct {
	LocalBoardID  BoardID       `json:"local_board_id,omitempty"`
	CoordinatorID string        `json:"coordinator_id,omitempty"`
	ReceiveFrom   string        `json:"receive_from,omitempty"`
	Receive       MirrorPolicy  `json:"receive"`
	Groups        []MirrorGroup `json:"groups,omitempty"`
}

type MirrorMember struct {
	BoardID BoardID `json:"board_id"`
	Peer    string  `json:"peer,omitempty"`
}

type MirrorGroup struct {
	ID      string         `json:"id"`
	Members []MirrorMember `json:"members"`
	Policy  MirrorPolicy   `json:"policy"`
}

// Relay numbers 1..4 are motion/interlocked outputs, NOT general relays.
// They must not be copied as a bitmask between independently safe boards.
type MirrorPolicy struct {
	Domains     []string `json:"domains,omitempty"`
	Relays      []int    `json:"relays,omitempty"`
	PWMChannels []int    `json:"pwm_channels,omitempty"`
}

// MirrorCommand is a finite, volatile semantic allowlist, never a wire frame,
// arbitrary CLI command, raw I2C transfer, EEPROM write or programming operation.
type MirrorCommand struct {
	Kind        string    `json:"kind"`
	Channel     int       `json:"channel,omitempty"`
	Value       uint16    `json:"value,omitempty"`
	Active      bool      `json:"active,omitempty"`
	Page        byte      `json:"page,omitempty"`
	Cells       [4]byte   `json:"cells,omitempty"`
	Brightness  byte      `json:"brightness,omitempty"`
	RGB         [3]byte   `json:"rgb,omitempty"`
	Lines       [2]string `json:"lines,omitempty"`
	FrequencyHz uint16    `json:"frequency_hz,omitempty"`
	DurationMS  uint16    `json:"duration_ms,omitempty"`
}

// Zero is meaningful (relay off, PWM zero, page zero). Emit the fields of the
// chosen semantic domain explicitly, without meaningless array placeholders.
func (command MirrorCommand) MarshalJSON() ([]byte, error) {
	value := map[string]any{"kind": command.Kind}
	switch command.Kind {
	case "menu":
		value["page"] = command.Page
	case "segments":
		value["cells"] = command.Cells
	case "lcd":
		value["lines"] = command.Lines
	case "status_led":
		value["rgb"], value["brightness"] = command.RGB, command.Brightness
	case "relay":
		value["channel"], value["active"] = command.Channel, command.Active
	case "pwm":
		value["channel"], value["value"] = command.Channel, command.Value
	case "buzzer":
		value["frequency_hz"], value["duration_ms"] = command.FrequencyHz, command.DurationMS
	}
	return json.Marshal(value)
}

func (command MirrorCommand) Key() string {
	if command.Kind == "relay" || command.Kind == "pwm" {
		return fmt.Sprintf("%s.%d", command.Kind, command.Channel)
	}
	return command.Kind
}

func (command MirrorCommand) Validate() error {
	switch command.Kind {
	case "menu":
	case "segments":
		if command.Brightness != 0 {
			return errors.New("segment mirror changes cells only; brightness is local physical policy")
		}
	case "lcd":
		for _, line := range command.Lines {
			if len(line) > 16 {
				return errors.New("LCD lines must be at most 16 bytes")
			}
			for _, ch := range line {
				if ch < 32 || ch > 126 {
					return errors.New("LCD mirror requires printable ASCII")
				}
			}
		}
	case "status_led":
		if command.Brightness != 255 {
			return errors.New("RGB mirror carries rendered bytes at full scale; brightness is not applied twice")
		}
	case "buzzer":
		if command.DurationMS == 0 || command.DurationMS > 60000 || (command.FrequencyHz != 0 && (command.FrequencyHz < 20 || command.FrequencyHz > 20000)) {
			return errors.New("buzzer requires 0 or 20..20000 Hz and 1..60000 ms")
		}
	case "relay":
		if command.Channel < 5 || command.Channel > 8 {
			return errors.New("only general relays R5..R8 can be mirrored; R1..R4 require coordinated motion safety")
		}
	case "pwm":
		if command.Channel < 0 || command.Channel > 15 || command.Value > 4095 {
			return errors.New("PWM requires channel 0..15 and value 0..4095")
		}
	default:
		return errors.New("unsupported mirror domain")
	}
	// Reject irrelevant fields rather than allowing an intent's meaning to drift.
	normalized := MirrorCommand{Kind: command.Kind}
	switch command.Kind {
	case "menu":
		normalized.Page = command.Page
	case "segments":
		normalized.Cells = command.Cells
	case "lcd":
		normalized.Lines = command.Lines
	case "status_led":
		normalized.RGB, normalized.Brightness = command.RGB, command.Brightness
	case "buzzer":
		normalized.FrequencyHz, normalized.DurationMS = command.FrequencyHz, command.DurationMS
	case "relay":
		normalized.Channel, normalized.Active = command.Channel, command.Active
	case "pwm":
		normalized.Channel, normalized.Value = command.Channel, command.Value
	}
	if normalized != command {
		return errors.New("mirror command contains fields outside its domain")
	}
	return nil
}

func (policy MirrorPolicy) Allows(command MirrorCommand) bool {
	if command.Validate() != nil {
		return false
	}
	if !containsString(policy.Domains, command.Kind) {
		return false
	}
	if command.Kind == "relay" {
		return containsInt(policy.Relays, command.Channel)
	}
	if command.Kind == "pwm" {
		return containsInt(policy.PWMChannels, command.Channel)
	}
	return true
}

func (policy MirrorPolicy) Validate() error {
	seen := map[string]bool{}
	for _, domain := range policy.Domains {
		if seen[domain] {
			return errors.New("duplicate mirror domain")
		}
		seen[domain] = true
		switch domain {
		case "menu", "segments", "lcd", "status_led", "buzzer", "relay", "pwm":
		default:
			return fmt.Errorf("unsupported mirror domain %q", domain)
		}
	}
	for _, bounds := range []struct {
		values    []int
		low, high int
		domain    string
	}{{policy.Relays, 5, 8, "relay"}, {policy.PWMChannels, 0, 15, "pwm"}} {
		seenChannels := map[int]bool{}
		if len(bounds.values) != 0 && !seen[bounds.domain] {
			return fmt.Errorf("%s channels require their domain", bounds.domain)
		}
		for _, channel := range bounds.values {
			if channel < bounds.low || channel > bounds.high || seenChannels[channel] {
				return fmt.Errorf("invalid or duplicate %s channel %d", bounds.domain, channel)
			}
			seenChannels[channel] = true
		}
		if seen[bounds.domain] && len(bounds.values) == 0 {
			return fmt.Errorf("%s requires explicit channels", bounds.domain)
		}
	}
	return nil
}

func (config MirrorConfig) Validate() error {
	if config.LocalBoardID != "" && !ValidBoardID(config.LocalBoardID) {
		return errors.New("invalid local mirror board id")
	}
	if err := config.Receive.Validate(); err != nil {
		return err
	}
	if config.ReceiveFrom != "" && !canonicalMatch(selectorIDPattern, config.ReceiveFrom) {
		return errors.New("invalid receive coordinator id")
	}
	if len(config.Receive.Domains) != 0 && (config.ReceiveFrom == "" || config.LocalBoardID == "") {
		return errors.New("mirror receive policy requires local_board_id and receive_from")
	}
	if len(config.Groups) > 4 {
		return errors.New("at most four mirror groups are supported")
	}
	if len(config.Groups) != 0 && !canonicalMatch(selectorIDPattern, config.CoordinatorID) {
		return errors.New("mirror groups require coordinator_id")
	}
	groups, boards, aliases := map[string]bool{}, map[BoardID]bool{}, map[string]bool{}
	for _, group := range config.Groups {
		if !canonicalMatch(selectorIDPattern, group.ID) || groups[group.ID] {
			return errors.New("invalid or duplicate mirror group id")
		}
		groups[group.ID] = true
		if len(group.Members) < 2 || len(group.Members) > 8 {
			return errors.New("mirror groups require 2..8 members")
		}
		if err := group.Policy.Validate(); err != nil {
			return err
		}
		for _, member := range group.Members {
			if !ValidBoardID(member.BoardID) || boards[member.BoardID] {
				return errors.New("invalid or overlapping mirror board identity")
			}
			boards[member.BoardID] = true
			if aliases[member.Peer] {
				return errors.New("a board endpoint may occur in only one mirror group")
			}
			aliases[member.Peer] = true
			if member.Peer == "" {
				if member.BoardID != config.LocalBoardID {
					return errors.New("local mirror member must match local_board_id")
				}
			} else if !canonicalMatch(selectorIDPattern, member.Peer) {
				return errors.New("invalid mirror peer alias")
			}
		}
	}
	return nil
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func containsInt(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
