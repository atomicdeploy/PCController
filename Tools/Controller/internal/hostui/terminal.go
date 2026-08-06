package hostui

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaximumTerminalTitleRunes = 128
	MaximumOSCPayloadBytes    = 512
)

type TerminalProgress struct {
	State   int
	Percent int
}

func ValidateTerminalTitle(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaximumTerminalTitleRunes {
		return "", fmt.Errorf("terminal title must be 1..%d printable characters", MaximumTerminalTitleRunes)
	}
	for _, character := range value {
		if !unicode.IsPrint(character) {
			return "", errors.New("terminal title contains a control character")
		}
	}
	return value, nil
}

// ValidateOSCPayload accepts the bytes between OSC and its terminator. The
// caller never supplies ESC, BEL, or ST, which prevents one action from
// breaking out into additional terminal control sequences.
func ValidateOSCPayload(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || len(value) > MaximumOSCPayloadBytes {
		return "", fmt.Errorf("OSC payload must be 1..%d UTF-8 bytes", MaximumOSCPayloadBytes)
	}
	selectorEnd := strings.IndexByte(value, ';')
	if selectorEnd < 0 {
		selectorEnd = len(value)
	}
	if selectorEnd < 1 || selectorEnd > 4 {
		return "", errors.New("OSC payload must start with a 1..4 digit selector")
	}
	for index, character := range value {
		if index < selectorEnd && (character < '0' || character > '9') {
			return "", errors.New("OSC payload must start with a numeric selector")
		}
		if unicode.IsControl(character) {
			return "", errors.New("OSC payload contains a control character or terminator")
		}
	}
	return value, nil
}

func OSCSequence(payload string) (string, error) {
	payload, err := ValidateOSCPayload(payload)
	if err != nil {
		return "", err
	}
	return "\x1b]" + payload + "\x07", nil
}

func WriteOSC(writer io.Writer, payload string) error {
	if writer == nil {
		return errors.New("terminal output is unavailable")
	}
	sequence, err := OSCSequence(payload)
	if err != nil {
		return err
	}
	_, err = io.WriteString(writer, sequence)
	return err
}

func ParseTerminalProgress(value string) (TerminalProgress, error) {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(character rune) bool {
		return unicode.IsSpace(character) || character == ';' || character == ','
	})
	if len(parts) < 1 || len(parts) > 2 {
		return TerminalProgress{}, errors.New("terminal progress requires STATE [0..100]")
	}
	states := map[string]int{
		"clear": 0, "none": 0, "default": 1, "normal": 1,
		"error": 2, "indeterminate": 3, "warning": 4, "warn": 4,
	}
	state, ok := states[strings.ToLower(parts[0])]
	if !ok {
		parsed, err := strconv.Atoi(parts[0])
		if err != nil || parsed < 0 || parsed > 4 {
			return TerminalProgress{}, errors.New("terminal progress state must be clear, normal, error, indeterminate, warning, or 0..4")
		}
		state = parsed
	}
	percent := 0
	if len(parts) == 2 {
		parsed, err := strconv.Atoi(parts[1])
		if err != nil || parsed < 0 || parsed > 100 {
			return TerminalProgress{}, errors.New("terminal progress must be 0..100")
		}
		percent = parsed
	} else if state == 1 || state == 2 || state == 4 {
		return TerminalProgress{}, errors.New("normal, error, and warning terminal progress require 0..100")
	}
	return TerminalProgress{State: state, Percent: percent}, nil
}

func (progress TerminalProgress) OSCPayload() (string, error) {
	if progress.State < 0 || progress.State > 4 || progress.Percent < 0 || progress.Percent > 100 {
		return "", errors.New("terminal progress is outside OSC 9;4 bounds")
	}
	return fmt.Sprintf("9;4;%d;%d", progress.State, progress.Percent), nil
}
