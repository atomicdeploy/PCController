package design

import (
	"fmt"
	"strconv"
	"strings"
)

// Color is one canonical sRGB token decomposed into native-friendly channels.
type Color struct {
	R, G, B byte
}

// ParseColor accepts the #RRGGBB notation used by tokens.json.
func ParseColor(value string) (Color, error) {
	value = strings.TrimSpace(value)
	if len(value) != 7 || value[0] != '#' {
		return Color{}, fmt.Errorf("design color %q must use #RRGGBB", value)
	}
	parsed, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		return Color{}, fmt.Errorf("design color %q: %w", value, err)
	}
	return Color{
		R: byte(parsed >> 16),
		G: byte(parsed >> 8),
		B: byte(parsed),
	}, nil
}

// MustColor parses a trusted embedded token and panics if it is malformed.
func MustColor(value string) Color {
	color, err := ParseColor(value)
	if err != nil {
		panic(err)
	}
	return color
}
