// Package comserver implements PCControllerDesktop's opt-in, out-of-process COM
// Automation server. Its type-info-free IDispatch surface provides On, Off,
// Toggle, Show, Hide, Reconnect, Notify, GetState, and the read-only State
// property.
package comserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"pccontroller.local/desktop/internal/ipc"
)

const (
	// ProgID is the stable, version-independent COM programmatic identifier.
	ProgID = "DRSDavidSoft.PCController"

	// VersionedProgID is the current version-specific alias used by CurVer.
	VersionedProgID = "DRSDavidSoft.PCController.1"

	// Preserve the imported automation class's immutable identity; changing
	// branding and ProgIDs must not silently assign this class a different GUID.
	CLSID = "{D9603C3B-7D90-53B2-AC01-7656B027603F}"

	displayName = "PCControllerDesktop Automation"

	dispatchMethod      = 0x1
	dispatchPropertyGet = 0x2
	dispatchPropertyPut = 0x4
	dispatchPropertyRef = 0x8

	dispIDOn        int32 = 1
	dispIDOff       int32 = 2
	dispIDToggle    int32 = 3
	dispIDShow      int32 = 4
	dispIDReconnect int32 = 5
	dispIDNotify    int32 = 6
	dispIDGetState  int32 = 7
	dispIDState     int32 = 8
	// Hide was added after the original Automation contract. Keep the existing
	// DISPIDs stable and append this method instead of renumbering members.
	dispIDHide    int32 = 9
	dispIDMessage int32 = 101

	maxNotifyRunes = 4096
)

var (
	// ErrUnsupported reports that COM Automation is only available on Windows.
	ErrUnsupported = errors.New("comserver: COM Automation is only supported on Windows")
	// ErrRegistrationOwnedByAnotherExecutable reports a per-user COM
	// registration whose exact ownership markers target another executable.
	ErrRegistrationOwnedByAnotherExecutable = errors.New(
		"comserver: registration belongs to another executable",
	)
	errUnknownMember     = errors.New("unknown Automation member")
	errBadParameterCount = errors.New("incorrect Automation parameter count")
	errTypeMismatch      = errors.New("Automation parameter type mismatch")
	errMemberAccess      = errors.New("invalid Automation invocation flags")
)

type bridge struct {
	handler ipc.Handler
	nextID  atomic.Uint64
}

func newBridge(handler ipc.Handler) (*bridge, error) {
	if handler == nil {
		return nil, errors.New("comserver: nil IPC handler")
	}
	return &bridge{handler: handler}, nil
}

func resolveName(name string) (int32, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "on":
		return dispIDOn, true
	case "off":
		return dispIDOff, true
	case "toggle":
		return dispIDToggle, true
	case "show":
		return dispIDShow, true
	case "hide":
		return dispIDHide, true
	case "reconnect":
		return dispIDReconnect, true
	case "notify":
		return dispIDNotify, true
	case "getstate":
		return dispIDGetState, true
	case "state":
		return dispIDState, true
	case "message":
		return dispIDMessage, true
	default:
		return -1, false
	}
}

func (b *bridge) invoke(ctx context.Context, dispID int32, flags uint16, args []any) (any, error) {
	method, params, stateResult, err := invocation(dispID, flags, args)
	if err != nil {
		return nil, err
	}
	request, err := ipc.NewRequest(
		"com-"+strconv.FormatUint(b.nextID.Add(1), 10),
		method,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("comserver: encode command: %w", err)
	}
	result, err := b.handler(ctx, request)
	if err != nil {
		return nil, err
	}
	if stateResult {
		return automationState(result), nil
	}
	return result, nil
}

func invocation(dispID int32, flags uint16, args []any) (method string, params any, stateResult bool, err error) {
	if flags&(dispatchPropertyPut|dispatchPropertyRef) != 0 {
		return "", nil, false, errMemberAccess
	}

	switch dispID {
	case dispIDOn:
		if flags&dispatchMethod == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 0 {
			return "", nil, false, errBadParameterCount
		}
		return "device.on", nil, false, nil
	case dispIDOff:
		if flags&dispatchMethod == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 0 {
			return "", nil, false, errBadParameterCount
		}
		return "device.off", nil, false, nil
	case dispIDToggle:
		if flags&dispatchMethod == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 0 {
			return "", nil, false, errBadParameterCount
		}
		return "device.toggle", nil, false, nil
	case dispIDShow:
		if flags&dispatchMethod == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 0 {
			return "", nil, false, errBadParameterCount
		}
		return "window.show", nil, false, nil
	case dispIDHide:
		if flags&dispatchMethod == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 0 {
			return "", nil, false, errBadParameterCount
		}
		return "window.hide", nil, false, nil
	case dispIDReconnect:
		if flags&dispatchMethod == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 0 {
			return "", nil, false, errBadParameterCount
		}
		return "reconnect", nil, false, nil
	case dispIDNotify:
		if flags&dispatchMethod == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 1 {
			return "", nil, false, errBadParameterCount
		}
		message, ok := args[0].(string)
		if !ok || !utf8.ValidString(message) {
			return "", nil, false, errTypeMismatch
		}
		if strings.TrimSpace(message) == "" {
			return "", nil, false, fmt.Errorf("%w: message is required", errTypeMismatch)
		}
		if utf8.RuneCountInString(message) > maxNotifyRunes {
			return "", nil, false, fmt.Errorf("%w: message exceeds %d characters", errTypeMismatch, maxNotifyRunes)
		}
		return "notifications.show", map[string]string{"message": message}, false, nil
	case dispIDGetState:
		if flags&dispatchMethod == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 0 {
			return "", nil, false, errBadParameterCount
		}
		return "status", nil, true, nil
	case dispIDState:
		if flags&dispatchPropertyGet == 0 {
			return "", nil, false, errMemberAccess
		}
		if len(args) != 0 {
			return "", nil, false, errBadParameterCount
		}
		return "status", nil, true, nil
	default:
		return "", nil, false, errUnknownMember
	}
}

func automationState(result any) string {
	switch value := result.(type) {
	case nil:
		return "unknown"
	case string:
		return normalizedState(value)
	case []byte:
		return stateFromJSON(value)
	case json.RawMessage:
		return stateFromJSON(value)
	case fmt.Stringer:
		return normalizedState(value.String())
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "unknown"
	}
	return stateFromJSON(data)
}

func stateFromJSON(data []byte) string {
	var direct string
	if err := json.Unmarshal(data, &direct); err == nil {
		return normalizedState(direct)
	}
	var object struct {
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &object); err != nil || len(object.State) == 0 {
		return "unknown"
	}
	if err := json.Unmarshal(object.State, &direct); err == nil {
		return normalizedState(direct)
	}
	var number int
	if err := json.Unmarshal(object.State, &number); err == nil {
		switch number {
		case 1:
			return "off"
		case 2:
			return "on"
		}
	}
	return "unknown"
}

func normalizedState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "1", "true", "high":
		return "on"
	case "off", "0", "false", "low":
		return "off"
	default:
		return "unknown"
	}
}

// RegistrationCommand returns the safely quoted LocalServer32 command used for
// COM activation. The executable is always quoted to avoid unquoted-path
// ambiguity, even if its current path contains no spaces.
func RegistrationCommand(executable string) (string, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return "", errors.New("comserver: executable path is empty")
	}
	if strings.ContainsAny(executable, "\"\x00\r\n") {
		return "", errors.New("comserver: executable path contains an unsupported character")
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return "", fmt.Errorf("comserver: resolve executable path: %w", err)
	}
	return `"` + filepath.Clean(absolute) + `" /com-server`, nil
}

// RegisterCurrentExecutable registers the current process image for the
// current user. Registration is never performed automatically.
func RegisterCurrentExecutable() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("comserver: locate current executable: %w", err)
	}
	return Register(executable)
}
