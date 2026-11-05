package appconfig

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ChannelRouting is opt-in, host-owned wiring for semantic references. Native
// relay/PWM commands remain physical diagnostics; enabling routing never
// rewrites an existing effect or changes a hardware output.
type ChannelRouting struct {
	Enabled  bool              `json:"enabled"`
	Channels []SemanticChannel `json:"channels"`
}

type SemanticChannel struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Name   string `json:"name"`
	Icon   string `json:"icon,omitempty"`
}

// RoutingTarget deliberately excludes the firmware's interlocked R1..R4 and
// autonomous PWM11..15. Host remapping must not pretend to move their safety,
// sensor, power or status-LED logic to a different electrical output.
func RoutingTarget(key string) (kind string, index byte, ok bool) {
	parts := strings.Split(key, ".")
	if len(parts) != 2 {
		return "", 0, false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || strconv.Itoa(n) != parts[1] {
		return "", 0, false
	}
	switch parts[0] {
	case "relay":
		if n >= 5 && n <= 8 {
			return "relay", byte(n - 1), true
		}
	case "pwm":
		if n >= 0 && n <= 10 {
			return "pwm", byte(n), true
		}
	}
	return "", 0, false
}

func ValidSemanticChannelKey(key string) bool {
	if len(key) == 0 || len(key) > 64 || key[0] < 'a' || key[0] > 'z' || !profileToken(key) {
		return false
	}
	if strings.Contains(key, "..") || strings.HasSuffix(key, ".") {
		return false
	}
	// Physical/catalog namespaces cannot become aliases with two meanings.
	switch strings.Split(key, ".")[0] {
	case "relay", "pwm", "seat", "motion", "display", "sensor", "strip":
		return false
	}
	return true
}

func ValidateChannelRouting(value ChannelRouting) error {
	if len(value.Channels) > 15 {
		return fmt.Errorf("at most 15 independently routed outputs are supported")
	}
	keys, targets := map[string]bool{}, map[string]bool{}
	for _, channel := range value.Channels {
		if !ValidSemanticChannelKey(channel.Key) || keys[channel.Key] {
			return fmt.Errorf("semantic key %q is invalid or duplicated", channel.Key)
		}
		kind, _, ok := RoutingTarget(channel.Target)
		if !ok || kind != channel.Kind {
			return fmt.Errorf("%s requires a compatible general-purpose %s target; %q is unavailable or firmware-owned", channel.Key, channel.Kind, channel.Target)
		}
		if targets[channel.Target] {
			return fmt.Errorf("physical output %s is already assigned; swap both bindings in one update", channel.Target)
		}
		if strings.TrimSpace(channel.Name) == "" || strings.TrimSpace(channel.Name) != channel.Name || utf8.RuneCountInString(channel.Name) > 64 || !printableText(channel.Name) {
			return fmt.Errorf("%s name must be 1..64 printable characters", channel.Key)
		}
		if utf8.RuneCountInString(channel.Icon) > 64 || channel.Icon != "" && !printableText(channel.Icon) {
			return fmt.Errorf("%s icon must be at most 64 printable characters", channel.Key)
		}
		keys[channel.Key], targets[channel.Target] = true, true
	}
	return nil
}

// ValidateRoutingReplacement keeps a semantic key's electrical value type
// immutable. Labels/icons may change and compatible targets may swap atomically.
func ValidateRoutingReplacement(previous, next ChannelRouting) error {
	if err := ValidateChannelRouting(next); err != nil {
		return err
	}
	for _, old := range previous.Channels {
		for _, candidate := range next.Channels {
			if candidate.Key == old.Key && candidate.Kind != old.Kind {
				return fmt.Errorf("%s kind cannot change; create a distinct semantic key", old.Key)
			}
		}
	}
	return nil
}

func ResolveSemanticChannel(routing ChannelRouting, key string, value uint16) (SemanticChannel, error) {
	if !routing.Enabled {
		return SemanticChannel{}, fmt.Errorf("semantic channel routing is disabled")
	}
	if err := ValidateChannelRouting(routing); err != nil {
		return SemanticChannel{}, err
	}
	for _, channel := range routing.Channels {
		if channel.Key != key {
			continue
		}
		maximum := uint16(PWMMaximumValue)
		if channel.Kind == "relay" {
			maximum = 1
		}
		if value > maximum {
			return SemanticChannel{}, fmt.Errorf("%s value must be 0..%d", key, maximum)
		}
		return channel, nil
	}
	return SemanticChannel{}, fmt.Errorf("semantic channel %q is not configured", key)
}
