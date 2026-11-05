// Package commands provides the single command surface shared by the Windows
// UI, tray menu, global hotkeys, WM_COPYDATA, named-pipe IPC, local HTTP API,
// and optional COM Automation server.
package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"rayanlamp.local/app/internal/appcore"
	"rayanlamp.local/app/internal/buildinfo"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/deviceterminal"
	"rayanlamp.local/app/internal/ipc"
)

const (
	maxTitleRunes   = 128
	maxMessageRunes = 4096
	maxAudioPathLen = 32767
)

// AudioImportResult identifies a validated content-addressed custom WAV.
type AudioImportResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Hooks contains actions owned by the Windows shell rather than the network
// core. Implementations must be safe to call from non-UI goroutines.
type Hooks struct {
	ShowWindow      func()
	HideWindow      func()
	OpenSettings    func()
	OpenDiagnostics func()
	OpenWebUI       func()
	// CanOpenWebUI is an optional preflight used by primaries whose Web
	// surface can be enabled or fail independently at runtime. Returning an
	// error prevents OpenWebUI from reporting a false success.
	CanOpenWebUI    func() error
	Quit            func()
	Notify          func(title, message string) error
	PlayAudio       func(cue string, count int) error
	ImportAudioFile func(path string) (AudioImportResult, error)
	// PrepareConfig validates host-owned resources before persistence. It may
	// return a repaired configuration (for example, bundled audio when a
	// selected custom WAV was removed) so public and runtime state agree.
	PrepareConfig func(config.Config) (config.Config, error)
	ConfigApplied func(config.Config)
}

// Dispatcher translates the stable IPC protocol into application operations.
type Dispatcher struct {
	core       *appcore.Core
	configPath string

	patchMu sync.Mutex
	hooksMu sync.RWMutex
	hooks   Hooks

	registration *registrationController
}

func New(core *appcore.Core, configPath string) (*Dispatcher, error) {
	if core == nil {
		return nil, errors.New("commands: nil application core")
	}
	if strings.TrimSpace(configPath) == "" {
		return nil, errors.New("commands: empty configuration path")
	}
	return &Dispatcher{
		core:         core,
		configPath:   configPath,
		registration: defaultRegistrationController(),
	}, nil
}

func (d *Dispatcher) SetHooks(hooks Hooks) {
	d.hooksMu.Lock()
	d.hooks = hooks
	d.hooksMu.Unlock()
}

func (d *Dispatcher) Handler(ctx context.Context, request ipc.Request) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.ProtocolVersion != 0 && request.ProtocolVersion != ipc.ProtocolVersion {
		return nil, commandError{"protocol_mismatch", "unsupported IPC protocol version"}
	}

	method := normalizeMethod(request.Method)
	switch method {
	case "ping":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		return map[string]any{
			"name":            buildinfo.Name,
			"version":         buildinfo.Version,
			"edition":         buildinfo.Edition,
			"developer_tools": buildinfo.DeveloperTools,
			"instance":        "primary",
		}, nil
	case "status", "state", "lamp.get", "get":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		return d.core.Snapshot(), nil
	case "lamp.on", "on":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		state, err := d.core.On(ctx)
		return state, deviceCommandError(err)
	case "lamp.off", "off":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		state, err := d.core.Off(ctx)
		return state, deviceCommandError(err)
	case "lamp.toggle", "toggle":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		state, err := d.core.Toggle(ctx)
		return state, deviceCommandError(err)
	case "lamp.refresh", "refresh":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		_, err := d.core.Refresh(ctx)
		return d.core.Snapshot(), deviceCommandError(err)
	case "reconnect":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		err := d.core.Reconnect()
		return map[string]bool{"reconnecting": err == nil}, deviceCommandError(err)
	case "window.show", "show":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		hook := d.currentHooks().ShowWindow
		if hook == nil {
			return nil, commandError{"unavailable", "no window is available in this primary"}
		}
		hook()
		return map[string]bool{"visible": true}, nil
	case "window.hide", "hide":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		hook := d.currentHooks().HideWindow
		if hook == nil {
			return nil, commandError{"unavailable", "no window is available in this primary"}
		}
		hook()
		return map[string]bool{"visible": false}, nil
	case "settings.show", "settings":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		hook := d.currentHooks().OpenSettings
		if hook == nil {
			return nil, commandError{"unavailable", "settings UI is unavailable in this primary"}
		}
		hook()
		return map[string]bool{"opened": true}, nil
	case "diagnostics.show":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		hook := d.currentHooks().OpenDiagnostics
		if hook == nil {
			return nil, commandError{"unavailable", "diagnostics UI is unavailable in this primary"}
		}
		hook()
		return map[string]bool{"opened": true}, nil
	case "web.show", "web", "dashboard.web":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		hooks := d.currentHooks()
		hook := hooks.OpenWebUI
		if hook == nil {
			return nil, commandError{"unavailable", "web UI is unavailable in this primary"}
		}
		if hooks.CanOpenWebUI != nil {
			if err := hooks.CanOpenWebUI(); err != nil {
				return nil, commandError{"unavailable", err.Error()}
			}
		}
		hook()
		return map[string]bool{"opened": true}, nil
	case "diagnostics.fetch":
		return d.fetchDiagnostic(ctx, request.Params)
	case "terminal.status", "telnet.status":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		return d.core.TerminalStatus(), nil
	case "terminal.connect", "telnet.connect":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		snapshot, err := d.core.TerminalConnect(ctx)
		return snapshot, terminalCommandError(err)
	case "terminal.disconnect", "telnet.disconnect":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		return d.core.TerminalDisconnect(), nil
	case "terminal.read", "telnet.read":
		return d.readTerminal(ctx, request.Params)
	case "terminal.write", "telnet.write":
		return d.writeTerminal(ctx, request.Params)
	case "config.get":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		return d.core.Config(), nil
	case "device.config.get":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		return d.core.DeviceConfigSnapshot(), nil
	case "device.config.refresh":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		if _, err := d.core.RefreshDiagnostics(ctx); err != nil {
			// Refreshing all audited endpoints is best effort. Return a coded
			// failure rather than pretending that a partial cache is fresh.
			return d.core.DeviceConfigSnapshot(), commandError{
				"device_unavailable",
				err.Error(),
			}
		}
		return d.core.DeviceConfigSnapshot(), nil
	case "config.patch":
		return d.patchConfig(request.Params)
	case "notifications.show", "notify":
		return d.showNotification(request.Params)
	case "audio.play", "audio.test", "sound.test":
		cue, count, err := decodeAudioParams(request.Params)
		if err != nil {
			return nil, err
		}
		hook := d.currentHooks().PlayAudio
		if hook == nil {
			return nil, commandError{"unavailable", "audio cues are unavailable"}
		}
		if err := hook(cue, count); err != nil {
			return nil, commandError{"unavailable", err.Error()}
		}
		return map[string]any{
			"played": true,
			"cue":    cue,
			"count":  count,
		}, nil
	case "audio.import":
		var params struct {
			Path string `json:"path"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return nil, err
		}
		params.Path = strings.TrimSpace(params.Path)
		if params.Path == "" || len(params.Path) > maxAudioPathLen ||
			!utf8.ValidString(params.Path) ||
			strings.IndexByte(params.Path, 0) >= 0 {
			return nil, commandError{
				"invalid_argument",
				"custom WAV path must be a non-empty valid local path",
			}
		}
		hook := d.currentHooks().ImportAudioFile
		if hook == nil {
			return nil, commandError{
				"unavailable",
				"custom audio import is unavailable",
			}
		}
		imported, err := hook(params.Path)
		if err != nil {
			return nil, commandError{"invalid_argument", err.Error()}
		}
		if !validAudioAssetID(imported.ID) ||
			strings.TrimSpace(imported.Name) == "" {
			return nil, commandError{
				"unavailable",
				"custom audio importer returned invalid metadata",
			}
		}
		imported.ID = strings.ToLower(imported.ID)
		imported.Name = strings.TrimSpace(imported.Name)
		return imported, nil
	case MethodIntegrationRegistrationStatus,
		MethodIntegrationShellRegister,
		MethodIntegrationShellUnregister,
		MethodIntegrationCOMRegister,
		MethodIntegrationCOMUnregister:
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		return d.registration.handle(method)
	case "version":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		return versionResult(), nil
	case "quit", "exit":
		if err := requireNoParams(request.Params); err != nil {
			return nil, err
		}
		hook := d.currentHooks().Quit
		if hook == nil {
			return nil, commandError{"unavailable", "quit is unavailable in this primary"}
		}
		hook()
		return map[string]bool{"quitting": true}, nil
	default:
		return nil, commandError{"not_found", fmt.Sprintf("unknown command %q", method)}
	}
}

func validAudioAssetID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') &&
			(character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}

func (d *Dispatcher) readTerminal(
	ctx context.Context,
	raw json.RawMessage,
) (any, error) {
	var params struct {
		After  uint64 `json:"after"`
		Limit  int    `json:"limit"`
		WaitMS int    `json:"wait_ms"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, err
	}
	if params.Limit < 0 || params.Limit > deviceterminal.MaxReadBytes {
		return nil, commandError{
			"invalid_argument",
			fmt.Sprintf("terminal read limit must be between 0 and %d bytes", deviceterminal.MaxReadBytes),
		}
	}
	maxWaitMS := int(deviceterminal.MaxLongPoll / time.Millisecond)
	if params.WaitMS < 0 || params.WaitMS > maxWaitMS {
		return nil, commandError{
			"invalid_argument",
			fmt.Sprintf("terminal wait_ms must be between 0 and %d", maxWaitMS),
		}
	}
	result, err := d.core.TerminalRead(
		ctx,
		params.After,
		params.Limit,
		time.Duration(params.WaitMS)*time.Millisecond,
	)
	if err != nil {
		return nil, terminalCommandError(err)
	}
	return result, nil
}

func (d *Dispatcher) writeTerminal(
	ctx context.Context,
	raw json.RawMessage,
) (any, error) {
	var params struct {
		DataBase64 string `json:"data_base64"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, err
	}
	if params.DataBase64 == "" {
		return nil, commandError{"invalid_argument", "terminal input is required"}
	}
	if len(params.DataBase64) > base64.StdEncoding.EncodedLen(deviceterminal.MaxInputBytes) {
		return nil, commandError{"request_too_large", deviceterminal.ErrInputTooLarge.Error()}
	}
	data, err := base64.StdEncoding.Strict().DecodeString(params.DataBase64)
	if err != nil {
		return nil, commandError{"invalid_argument", "terminal input must be canonical base64"}
	}
	if len(data) == 0 {
		return nil, commandError{"invalid_argument", "terminal input is required"}
	}
	snapshot, err := d.core.TerminalWrite(ctx, data)
	return snapshot, terminalCommandError(err)
}

func terminalCommandError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, deviceterminal.ErrBusy):
		return commandError{"busy", err.Error()}
	case errors.Is(err, deviceterminal.ErrNotConnected):
		return commandError{"not_connected", err.Error()}
	case errors.Is(err, deviceterminal.ErrReadOnly):
		return commandError{"not_supported", err.Error()}
	case errors.Is(err, deviceterminal.ErrInputTooLarge):
		return commandError{"request_too_large", err.Error()}
	case errors.Is(err, deviceterminal.ErrClosed):
		return commandError{"unavailable", err.Error()}
	default:
		return commandError{"device_unavailable", err.Error()}
	}
}

func (d *Dispatcher) currentHooks() Hooks {
	d.hooksMu.RLock()
	defer d.hooksMu.RUnlock()
	return d.hooks
}

func (d *Dispatcher) fetchDiagnostic(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, err
	}
	params.Endpoint = strings.TrimSpace(params.Endpoint)
	if params.Endpoint == "" {
		return nil, commandError{"invalid_endpoint", "diagnostic endpoint is required"}
	}
	if err := d.core.Config().Diagnostics.ValidateRequest(http.MethodGet, params.Endpoint); err != nil {
		return nil, commandError{"invalid_endpoint", err.Error()}
	}
	data, err := d.core.FetchDiagnostic(ctx, params.Endpoint)
	if err != nil {
		return nil, commandError{"device_unavailable", err.Error()}
	}
	return map[string]any{
		"endpoint": params.Endpoint,
		"content":  string(data),
		"bytes":    len(data),
	}, nil
}

func (d *Dispatcher) patchConfig(patch json.RawMessage) (any, error) {
	d.patchMu.Lock()
	defer d.patchMu.Unlock()

	if len(bytes.TrimSpace(patch)) == 0 {
		return nil, commandError{"invalid_config", "configuration patch is required"}
	}
	var patchObject map[string]json.RawMessage
	if err := json.Unmarshal(patch, &patchObject); err != nil || patchObject == nil {
		return nil, commandError{"invalid_config", "configuration patch must be a JSON object"}
	}

	var incomingObject map[string]any
	if err := json.Unmarshal(patch, &incomingObject); err != nil {
		return nil, commandError{"invalid_config", err.Error()}
	}
	canonicalizeUIPatchAliases(incomingObject)

	// Merge against the latest persisted configuration while holding config's
	// package-wide persistence lock. Window placement is written independently
	// by the shell with config.Update; starting from d.core.Config here would
	// allow an unrelated IPC patch to put stale bounds back on disk.
	var previous config.Config
	updated, err := config.Update(d.configPath, func(current *config.Config) error {
		previous = *current
		currentData, marshalErr := json.Marshal(current)
		if marshalErr != nil {
			return fmt.Errorf("encode current configuration: %w", marshalErr)
		}
		var currentObject map[string]any
		if unmarshalErr := json.Unmarshal(currentData, &currentObject); unmarshalErr != nil {
			return fmt.Errorf("decode current configuration: %w", unmarshalErr)
		}
		mergeObject(currentObject, incomingObject)

		mergedData, marshalErr := json.Marshal(currentObject)
		if marshalErr != nil {
			return commandError{"invalid_config", marshalErr.Error()}
		}
		var candidate config.Config
		decoder := json.NewDecoder(bytes.NewReader(mergedData))
		decoder.DisallowUnknownFields()
		if decodeErr := decoder.Decode(&candidate); decodeErr != nil {
			return commandError{"invalid_config", decodeErr.Error()}
		}
		if normalizeErr := candidate.Normalize(); normalizeErr != nil {
			return commandError{"invalid_config", normalizeErr.Error()}
		}
		if validateErr := appcore.ValidateConfig(candidate); validateErr != nil {
			return commandError{"invalid_config", validateErr.Error()}
		}
		if prepare := d.currentHooks().PrepareConfig; prepare != nil {
			prepared, prepareErr := prepare(candidate)
			if prepareErr != nil {
				return commandError{"invalid_config", prepareErr.Error()}
			}
			candidate = prepared
			if normalizeErr := candidate.Normalize(); normalizeErr != nil {
				return commandError{"invalid_config", normalizeErr.Error()}
			}
			if validateErr := appcore.ValidateConfig(candidate); validateErr != nil {
				return commandError{"invalid_config", validateErr.Error()}
			}
		}
		*current = candidate
		return nil
	})
	if err != nil {
		var coded ipc.CodedError
		if errors.As(err, &coded) {
			return nil, err
		}
		return nil, fmt.Errorf("save configuration: %w", err)
	}

	// Persist before changing runtime state. A failed save therefore leaves the
	// active client and its event stream untouched.
	if err := d.core.ApplyConfig(updated); err != nil {
		rollbackErr := config.Save(d.configPath, previous)
		if rollbackErr != nil {
			return nil, errors.Join(
				fmt.Errorf("apply configuration: %w", err),
				fmt.Errorf("restore previous configuration: %w", rollbackErr),
			)
		}
		return nil, fmt.Errorf("apply configuration: %w", err)
	}
	if hook := d.currentHooks().ConfigApplied; hook != nil {
		hook(updated)
	}
	return updated, nil
}

func (d *Dispatcher) showNotification(raw json.RawMessage) (any, error) {
	var params struct {
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, err
	}
	params.Title = strings.TrimSpace(params.Title)
	params.Message = strings.TrimSpace(params.Message)
	if params.Message == "" {
		return nil, commandError{"invalid_argument", "notification message is required"}
	}
	if !utf8.ValidString(params.Title) || !utf8.ValidString(params.Message) {
		return nil, commandError{"invalid_argument", "notification text must be valid UTF-8"}
	}
	if utf8.RuneCountInString(params.Title) > maxTitleRunes ||
		utf8.RuneCountInString(params.Message) > maxMessageRunes {
		return nil, commandError{"invalid_argument", "notification text is too long"}
	}
	hook := d.currentHooks().Notify
	if hook == nil {
		return nil, commandError{"unavailable", "notifications are unavailable"}
	}
	if err := hook(params.Title, params.Message); err != nil {
		return nil, commandError{"unavailable", err.Error()}
	}
	return map[string]bool{"shown": true}, nil
}

func decodeAudioParams(raw json.RawMessage) (string, int, error) {
	params := struct {
		Cue   string `json:"cue"`
		Count int    `json:"count"`
	}{
		Cue:   "message",
		Count: 1,
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null")) &&
		!bytes.Equal(trimmed, []byte("{}")) {
		if err := decodeParams(raw, &params); err != nil {
			return "", 0, err
		}
	}
	params.Cue = strings.ToLower(strings.TrimSpace(params.Cue))
	switch params.Cue {
	case "on", "off", "message", "error":
	default:
		return "", 0, commandError{
			"invalid_argument",
			"audio cue must be on, off, message, or error",
		}
	}
	if params.Count < 1 || params.Count > 10 {
		return "", 0, commandError{
			"invalid_argument",
			"audio count must be between 1 and 10",
		}
	}
	return params.Cue, params.Count, nil
}

func normalizeMethod(method string) string {
	method = strings.ToLower(strings.TrimSpace(method))
	method = strings.TrimLeft(method, "-/")
	return method
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return commandError{"invalid_argument", "command parameters are required"}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return commandError{"invalid_argument", "invalid command parameters: " + err.Error()}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return commandError{"invalid_argument", "command parameters contain trailing data"}
	}
	return nil
}

func requireNoParams(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) ||
		bytes.Equal(trimmed, []byte("{}")) {
		return nil
	}
	return commandError{"invalid_argument", "this command does not accept parameters"}
}

func deviceCommandError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return commandError{"device_unavailable", err.Error()}
}

func mergeObject(target, patch map[string]any) {
	for key, value := range patch {
		if value == nil {
			delete(target, key)
			continue
		}
		patchChild, isObject := value.(map[string]any)
		if !isObject {
			target[key] = value
			continue
		}
		targetChild, ok := target[key].(map[string]any)
		if !ok {
			targetChild = make(map[string]any)
		}
		mergeObject(targetChild, patchChild)
		target[key] = targetChild
	}
}

// canonicalizeUIPatchAliases keeps the legacy show_title_bar boolean and the
// three-way title_bar_mode setting from fighting after a deep merge. Modern
// clients may patch only title_bar_mode, while older integrations may still
// patch only show_title_bar; either spelling must deterministically update both
// serialized fields before Config.Normalize performs migration.
func canonicalizeUIPatchAliases(patch map[string]any) {
	ui, ok := patch["ui"].(map[string]any)
	if !ok {
		return
	}

	if rawMode, present := ui["title_bar_mode"]; present {
		mode, validString := rawMode.(string)
		if !validString {
			return
		}
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case config.TitleBarCustom:
			ui["show_title_bar"] = true
		case config.TitleBarNone, config.TitleBarNative:
			ui["show_title_bar"] = false
		}
		return
	}

	if rawLegacy, present := ui["show_title_bar"]; present {
		if show, validBool := rawLegacy.(bool); validBool {
			if show {
				ui["title_bar_mode"] = config.TitleBarCustom
			} else {
				ui["title_bar_mode"] = config.TitleBarNone
			}
		}
	}
}

func versionResult() map[string]string {
	return map[string]string{
		"name":         buildinfo.Name,
		"display_name": buildinfo.DisplayName,
		"version":      buildinfo.Version,
		"company":      buildinfo.Company,
		"description":  buildinfo.Description,
		"edition":      buildinfo.Edition,
	}
}

type commandError struct {
	code    string
	message string
}

func (e commandError) Error() string { return e.message }
func (e commandError) Code() string  { return e.code }
