package appconfig

import (
	"errors"
	"strings"
)

const (
	BuzzerPathBoard   = "board"
	BuzzerPathHost    = "host"
	BuzzerPathBoth    = "both"
	BuzzerPathNone    = "none"
	BuzzerPathUnknown = "unknown"
)

// BuzzerRuntimeOverrides are process-lifetime presentation choices. They are
// deliberately separate from the watched configuration. An explicit Path is
// reconciled to the MCU Silent bit by the serial-owning bridge; unspecified
// paths never cause an EEPROM write. Pointer fields distinguish an explicit
// false/empty override from an unspecified value.
type BuzzerRuntimeOverrides struct {
	Path       string  `json:"path,omitempty"`
	Mirror     *bool   `json:"mirror,omitempty"`
	Backend    string  `json:"backend,omitempty"`
	Executable *string `json:"executable,omitempty"`
}

type BuzzerRuntimeState struct {
	Configured           BuzzerMirror `json:"configured"`
	Effective            BuzzerMirror `json:"effective"`
	RequestedPath        string       `json:"requested_path,omitempty"`
	PathOverridden       bool         `json:"path_overridden"`
	MirrorOverridden     bool         `json:"mirror_overridden"`
	BackendOverridden    bool         `json:"backend_overridden"`
	ExecutableOverridden bool         `json:"executable_overridden"`
}

type BuzzerRuntimeStatus struct {
	RequestedPath        string `json:"requested_path"`
	EffectivePath        string `json:"effective_path"`
	BoardStateKnown      bool   `json:"board_state_known"`
	BoardSilent          bool   `json:"board_silent"`
	BoardChangeRequired  bool   `json:"board_change_required"`
	BoardApplyState      string `json:"board_apply_state"`
	BoardApplyError      string `json:"board_apply_error,omitempty"`
	HostMirror           bool   `json:"host_mirror"`
	BackendRequested     string `json:"backend_requested"`
	BackendEffective     string `json:"backend_effective"`
	ExecutableRequested  string `json:"executable_requested,omitempty"`
	ExecutableEffective  string `json:"executable_effective,omitempty"`
	BackendError         string `json:"backend_error,omitempty"`
	PathOverridden       bool   `json:"path_overridden"`
	MirrorOverridden     bool   `json:"mirror_overridden"`
	BackendOverridden    bool   `json:"backend_overridden"`
	ExecutableOverridden bool   `json:"executable_overridden"`
}

func NormalizeBuzzerPath(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", BuzzerPathBoard, BuzzerPathHost, BuzzerPathBoth, BuzzerPathNone:
		return value, nil
	default:
		return "", errors.New("buzzer path must be board, host, both, or none")
	}
}

func NormalizeBuzzerBackend(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "auto", "native", "external", "off":
		return value, nil
	default:
		return "", errors.New("buzzer backend must be auto, native, external, or off")
	}
}

func BuzzerPath(boardSilent, hostEnabled bool) string {
	switch {
	case !boardSilent && hostEnabled:
		return BuzzerPathBoth
	case !boardSilent:
		return BuzzerPathBoard
	case hostEnabled:
		return BuzzerPathHost
	default:
		return BuzzerPathNone
	}
}

func buzzerPathParts(path string) (boardSilent, hostEnabled bool) {
	switch path {
	case BuzzerPathBoard:
		return false, false
	case BuzzerPathHost:
		return true, true
	case BuzzerPathBoth:
		return false, true
	default:
		return true, false
	}
}

func (state BuzzerRuntimeState) Status(
	haveBoard, boardSilent bool,
	backendEffective, executableEffective, backendError string,
) BuzzerRuntimeStatus {
	requested := state.RequestedPath
	if requested == "" {
		requested = state.Effective.Path
	}
	if requested != "" {
		desiredSilent, _ := buzzerPathParts(requested)
		requested = BuzzerPath(desiredSilent, state.Effective.Enabled)
	}
	if requested == "" {
		if haveBoard {
			requested = BuzzerPath(boardSilent, state.Effective.Enabled)
		} else {
			requested = BuzzerPathUnknown
		}
	}
	effectivePath := BuzzerPathUnknown
	if haveBoard {
		effectivePath = BuzzerPath(boardSilent, state.Effective.Enabled)
	}
	backendRequested := strings.ToLower(strings.TrimSpace(state.Effective.Backend))
	if backendRequested == "" {
		backendRequested = "auto"
	}
	if !state.Effective.Enabled || !state.Effective.NativeEnabled || backendRequested == "off" {
		backendEffective = "off"
		executableEffective = ""
		backendError = ""
	}
	status := BuzzerRuntimeStatus{
		RequestedPath: requested, EffectivePath: effectivePath,
		BoardStateKnown: haveBoard, BoardSilent: boardSilent,
		HostMirror:           state.Effective.Enabled,
		BackendRequested:     backendRequested,
		BackendEffective:     strings.ToLower(strings.TrimSpace(backendEffective)),
		ExecutableRequested:  state.Effective.Executable,
		ExecutableEffective:  executableEffective,
		BackendError:         backendError,
		PathOverridden:       state.PathOverridden,
		MirrorOverridden:     state.MirrorOverridden,
		BackendOverridden:    state.BackendOverridden,
		ExecutableOverridden: state.ExecutableOverridden,
	}
	if status.BackendEffective == "" {
		status.BackendEffective = "unavailable"
	}
	if state.Effective.Path != "" && haveBoard {
		desiredSilent, _ := buzzerPathParts(requested)
		status.BoardChangeRequired = desiredSilent != boardSilent
	}
	if state.Effective.Path == "" {
		status.BoardApplyState = "unspecified"
	} else if !haveBoard || status.BoardChangeRequired {
		status.BoardApplyState = "pending"
	} else {
		status.BoardApplyState = "verified"
	}
	return status
}

func applyBuzzerRuntimeOverrides(value *Config, override BuzzerRuntimeOverrides) {
	if override.Path != "" {
		value.Integrations.BuzzerMirror.Path = override.Path
		_, value.Integrations.BuzzerMirror.Enabled = buzzerPathParts(override.Path)
	}
	if override.Mirror != nil {
		value.Integrations.BuzzerMirror.Enabled = *override.Mirror
		if value.Integrations.BuzzerMirror.Path != "" {
			desiredSilent, _ := buzzerPathParts(value.Integrations.BuzzerMirror.Path)
			value.Integrations.BuzzerMirror.Path = BuzzerPath(desiredSilent, *override.Mirror)
		}
	}
	if override.Backend != "" {
		if override.Backend == "off" {
			value.Integrations.BuzzerMirror.Backend = "off"
			value.Integrations.BuzzerMirror.NativeEnabled = false
		} else {
			value.Integrations.BuzzerMirror.Backend = override.Backend
		}
	}
	if override.Executable != nil {
		value.Integrations.BuzzerMirror.Executable = *override.Executable
	}
}
