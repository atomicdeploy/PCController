//go:build windows

package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/deviceterminal"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/model"
)

type ipcTransport struct {
	commandCall func(context.Context, ipc.Request) (ipc.Response, error)
}

var (
	_ ConfigTransport  = (*ipcTransport)(nil)
	_ CommandTransport = (*ipcTransport)(nil)
)

func newIPCTransport(ctx context.Context) (*ipcTransport, error) {
	transport := &ipcTransport{}
	if _, err := transport.Status(ctx); err != nil {
		return nil, err
	}
	return transport, nil
}

func (*ipcTransport) Mode() TransportMode { return ModeIPC }
func (*ipcTransport) Label() string       { return "GUI · named pipe" }
func (*ipcTransport) Events() <-chan model.Event {
	return nil
}
func (*ipcTransport) Close() error { return nil }

func (transport *ipcTransport) ExecuteCommand(
	ctx context.Context,
	method string,
	params any,
) (json.RawMessage, error) {
	request, err := ipc.NewRequest(
		fmt.Sprintf("tui-%d", time.Now().UnixNano()),
		method,
		params,
	)
	if err != nil {
		return nil, err
	}
	caller := ipc.Call
	if transport != nil && transport.commandCall != nil {
		caller = transport.commandCall
	}
	response, err := caller(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("named-pipe IPC: %w", err)
	}
	if !response.OK {
		if response.Error == nil {
			return nil, fmt.Errorf("%s command failed", request.Method)
		}
		return nil, fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}
	return append(json.RawMessage(nil), response.Result...), nil
}

func (transport *ipcTransport) GetConfig(
	ctx context.Context,
) (config.Config, error) {
	result, err := transport.ExecuteCommand(ctx, "config.get", nil)
	if err != nil {
		return config.Config{}, err
	}
	return decodeConfigCommandResult("GUI", result)
}

func (transport *ipcTransport) PatchConfig(
	ctx context.Context,
	patch any,
) (config.Config, error) {
	result, err := transport.ExecuteCommand(ctx, "config.patch", patch)
	if err != nil {
		return config.Config{}, err
	}
	return decodeConfigCommandResult("GUI", result)
}

func (*ipcTransport) Status(ctx context.Context) (model.Snapshot, error) {
	response, err := callIPC(ctx, "status")
	if err != nil {
		return model.Snapshot{}, err
	}
	var snapshot model.Snapshot
	if err := response.DecodeResult(&snapshot); err != nil {
		return model.Snapshot{}, fmt.Errorf("decode GUI status: %w", err)
	}
	return snapshot, nil
}

func (transport *ipcTransport) Execute(
	ctx context.Context,
	action Action,
) (model.Snapshot, error) {
	if err := validateAction(action); err != nil {
		return model.Snapshot{}, err
	}
	method := map[Action]string{
		ActionToggle:    "device.toggle",
		ActionOn:        "device.on",
		ActionOff:       "device.off",
		ActionRefresh:   "device.refresh",
		ActionReconnect: "reconnect",
	}[action]
	response, err := callIPC(ctx, method)
	if err != nil {
		return model.Snapshot{}, err
	}
	if action == ActionRefresh {
		var snapshot model.Snapshot
		if err := response.DecodeResult(&snapshot); err != nil {
			return model.Snapshot{}, fmt.Errorf("decode refreshed GUI status: %w", err)
		}
		return snapshot, nil
	}
	snapshot, err := transport.Status(ctx)
	if err != nil {
		return model.Snapshot{}, &ConfirmationError{Action: action, Cause: err}
	}
	return snapshot, nil
}

func (*ipcTransport) FetchDiagnostics(
	ctx context.Context,
	endpoint string,
) (DiagnosticResult, error) {
	request, err := newDiagnosticCommandRequest(
		fmt.Sprintf("tui-%d", time.Now().UnixNano()),
		endpoint,
	)
	if err != nil {
		return DiagnosticResult{}, err
	}
	response, err := callIPCRequest(ctx, request)
	if err != nil {
		return DiagnosticResult{}, err
	}
	var result DiagnosticResult
	if err := response.DecodeResult(&result); err != nil {
		return DiagnosticResult{}, fmt.Errorf("decode GUI diagnostics: %w", err)
	}
	return normalizeDiagnosticResult(endpoint, result)
}

func (*ipcTransport) TerminalStatus(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	return ipcTerminalSnapshot(ctx, "terminal.status", nil)
}

func (*ipcTransport) TerminalConnect(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	return ipcTerminalSnapshot(ctx, "terminal.connect", nil)
}

func (*ipcTransport) TerminalDisconnect(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	return ipcTerminalSnapshot(ctx, "terminal.disconnect", nil)
}

func (*ipcTransport) TerminalRead(
	ctx context.Context,
	after uint64,
	limit int,
	wait time.Duration,
) (deviceterminal.ReadResult, error) {
	if err := validateTerminalRead(limit, wait); err != nil {
		return deviceterminal.ReadResult{}, err
	}
	response, err := callIPCWithParams(ctx, "terminal.read", map[string]any{
		"after":   after,
		"limit":   limit,
		"wait_ms": int(wait.Milliseconds()),
	})
	if err != nil {
		return deviceterminal.ReadResult{}, err
	}
	var result deviceterminal.ReadResult
	if err := response.DecodeResult(&result); err != nil {
		return deviceterminal.ReadResult{}, fmt.Errorf(
			"decode GUI terminal output: %w",
			err,
		)
	}
	return result, nil
}

func (*ipcTransport) TerminalWrite(
	ctx context.Context,
	data []byte,
) (deviceterminal.Snapshot, error) {
	if err := validateTerminalInput(data); err != nil {
		return deviceterminal.Snapshot{}, err
	}
	return ipcTerminalSnapshot(ctx, "terminal.write", map[string]string{
		"data_base64": base64.StdEncoding.EncodeToString(data),
	})
}

func ipcTerminalSnapshot(
	ctx context.Context,
	method string,
	params any,
) (deviceterminal.Snapshot, error) {
	response, err := callIPCWithParams(ctx, method, params)
	if err != nil {
		return deviceterminal.Snapshot{}, err
	}
	var snapshot deviceterminal.Snapshot
	if err := response.DecodeResult(&snapshot); err != nil {
		return deviceterminal.Snapshot{}, fmt.Errorf(
			"decode GUI terminal response: %w",
			err,
		)
	}
	return snapshot, nil
}

func newDiagnosticCommandRequest(id string, endpoint string) (ipc.Request, error) {
	if err := validateDiagnosticEndpoint(endpoint); err != nil {
		return ipc.Request{}, err
	}
	return ipc.NewRequest(
		id,
		"diagnostics.fetch",
		map[string]string{"endpoint": endpoint},
	)
}

func callIPC(ctx context.Context, method string) (ipc.Response, error) {
	return callIPCWithParams(ctx, method, nil)
}

func callIPCWithParams(
	ctx context.Context,
	method string,
	params any,
) (ipc.Response, error) {
	request, err := ipc.NewRequest(
		fmt.Sprintf("tui-%d", time.Now().UnixNano()),
		method,
		params,
	)
	if err != nil {
		return ipc.Response{}, err
	}
	return callIPCRequest(ctx, request)
}

func callIPCRequest(ctx context.Context, request ipc.Request) (ipc.Response, error) {
	response, err := ipc.Call(ctx, request)
	if err != nil {
		return ipc.Response{}, fmt.Errorf("named-pipe IPC: %w", err)
	}
	if !response.OK {
		if response.Error == nil {
			return ipc.Response{}, fmt.Errorf("%s command failed", request.Method)
		}
		return ipc.Response{}, fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}
	return response, nil
}
