package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/deviceterminal"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/model"
)

const maxAPIResponseBytes = int64(ipc.MaxMessageBytes)

type apiTransport struct {
	baseURL string
	token   string
	client  *http.Client
}

var (
	_ ConfigTransport  = (*apiTransport)(nil)
	_ CommandTransport = (*apiTransport)(nil)
)

type apiEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ipc.Error      `json:"error,omitempty"`
}

type redactedAPIError struct {
	prefix  string
	message string
	cause   error
}

func (e *redactedAPIError) Error() string {
	return e.prefix + ": " + e.message
}

func (e *redactedAPIError) Unwrap() error {
	return e.cause
}

func newAPITransport(
	rawURL string,
	token string,
	timeout time.Duration,
	client *http.Client,
) (*apiTransport, error) {
	baseURL, err := validateLoopbackAPIURL(rawURL)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		// Keep this identifier deliberately opaque: callers may safely include
		// the error in logs without accidentally echoing even a whitespace-only
		// credential supplied on the command line.
		return nil, errors.New("invalid_api_token")
	}
	if client == nil {
		client = &http.Client{}
	} else {
		clone := *client
		client = &clone
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if client.Timeout <= 0 || client.Timeout > timeout {
		client.Timeout = timeout
	}
	// Authorization must never follow a redirect, even to another loopback
	// address. The API has fixed routes and never needs redirects.
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &apiTransport{
		baseURL: baseURL,
		token:   token,
		client:  client,
	}, nil
}

func (*apiTransport) Mode() TransportMode { return ModeAPI }
func (*apiTransport) Label() string       { return "GUI · loopback API" }
func (*apiTransport) Events() <-chan model.Event {
	return nil
}
func (transport *apiTransport) Close() error {
	if transport != nil && transport.client != nil {
		transport.client.CloseIdleConnections()
	}
	return nil
}

func (transport *apiTransport) ExecuteCommand(
	ctx context.Context,
	method string,
	params any,
) (json.RawMessage, error) {
	method = normalizeCommandMethod(method)
	var (
		envelope apiEnvelope
		err      error
	)
	switch method {
	case "ping":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodGet, "/ping")
		}
	case "status", "state", "device.get", "get":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodGet, "/status")
		}
	case "device.on", "on":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/device/on")
		}
	case "device.off", "off":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/device/off")
		}
	case "device.toggle", "toggle":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/device/toggle")
		}
	case "device.refresh", "refresh":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/device/refresh")
		}
	case "reconnect":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/reconnect")
		}
	case "window.show", "show":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/window/show")
		}
	case "window.hide", "hide":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/window/hide")
		}
	case "settings.show", "settings":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/settings/show")
		}
	case "diagnostics.show":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/diagnostics/show")
		}
	case "web.show", "web", "dashboard.web":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/web/show")
		}
	case "diagnostics.fetch":
		var decoded struct {
			Endpoint string `json:"endpoint"`
		}
		if err = decodeAPICommandParams(method, params, &decoded); err == nil {
			query := url.Values{"endpoint": []string{decoded.Endpoint}}
			envelope, err = transport.request(
				ctx,
				http.MethodGet,
				"/diagnostics?"+query.Encode(),
			)
		}
	case "terminal.status", "telnet.status":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodGet, "/terminal/status")
		}
	case "terminal.connect", "telnet.connect":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/terminal/connect")
		}
	case "terminal.disconnect", "telnet.disconnect":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/terminal/disconnect")
		}
	case "terminal.read", "telnet.read":
		var decoded struct {
			After  uint64 `json:"after"`
			Limit  int    `json:"limit"`
			WaitMS int    `json:"wait_ms"`
		}
		if err = decodeAPICommandParams(method, params, &decoded); err == nil {
			query := url.Values{
				"after":   []string{strconv.FormatUint(decoded.After, 10)},
				"limit":   []string{strconv.Itoa(decoded.Limit)},
				"wait_ms": []string{strconv.Itoa(decoded.WaitMS)},
			}
			envelope, err = transport.request(
				ctx,
				http.MethodGet,
				"/terminal/output?"+query.Encode(),
			)
		}
	case "terminal.write", "telnet.write":
		envelope, err = transport.requestJSON(
			ctx,
			http.MethodPost,
			"/terminal/input",
			params,
		)
	case "device.config.get":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodGet, "/device/config")
		}
	case "device.config.refresh":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(
				ctx,
				http.MethodPost,
				"/device/config/refresh",
			)
		}
	case "config.get":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodGet, "/config")
		}
	case "config.patch":
		envelope, err = transport.requestJSON(
			ctx,
			http.MethodPatch,
			"/config",
			params,
		)
	case commands.MethodIntegrationRegistrationStatus:
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(
				ctx,
				http.MethodGet,
				"/integration/registration",
			)
		}
	case commands.MethodIntegrationShellRegister,
		commands.MethodIntegrationShellUnregister,
		commands.MethodIntegrationCOMRegister,
		commands.MethodIntegrationCOMUnregister:
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.requestJSON(
				ctx,
				http.MethodPost,
				"/integration/registration",
				map[string]string{"method": method},
			)
		}
	case "notifications.show", "notify":
		envelope, err = transport.requestJSON(
			ctx,
			http.MethodPost,
			"/notifications",
			params,
		)
	case "audio.play", "audio.test", "sound.test":
		if params == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/audio/play")
		} else {
			envelope, err = transport.requestJSON(
				ctx,
				http.MethodPost,
				"/audio/play",
				params,
			)
		}
	case "audio.import":
		envelope, err = transport.requestJSON(
			ctx,
			http.MethodPost,
			"/audio/import",
			params,
		)
	case "version":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodGet, "/version")
		}
	case "quit", "exit":
		err = requireAPICommandNoParams(method, params)
		if err == nil {
			envelope, err = transport.request(ctx, http.MethodPost, "/quit")
		}
	default:
		return nil, fmt.Errorf("unsupported local API command %q", method)
	}
	if err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), envelope.Result...), nil
}

func (transport *apiTransport) GetConfig(
	ctx context.Context,
) (config.Config, error) {
	result, err := transport.ExecuteCommand(ctx, "config.get", nil)
	if err != nil {
		return config.Config{}, err
	}
	return decodeConfigCommandResult("local API", result)
}

func (transport *apiTransport) PatchConfig(
	ctx context.Context,
	patch any,
) (config.Config, error) {
	result, err := transport.ExecuteCommand(ctx, "config.patch", patch)
	if err != nil {
		return config.Config{}, err
	}
	return decodeConfigCommandResult("local API", result)
}

func normalizeCommandMethod(method string) string {
	method = strings.ToLower(strings.TrimSpace(method))
	return strings.TrimLeft(method, "-/")
}

func requireAPICommandNoParams(method string, params any) error {
	if params == nil {
		return nil
	}
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode %s command parameters: %w", method, err)
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) ||
		bytes.Equal(trimmed, []byte("{}")) {
		return nil
	}
	return fmt.Errorf("%s command does not accept parameters", method)
}

func decodeAPICommandParams(
	method string,
	params any,
	target any,
) error {
	if params == nil {
		return fmt.Errorf("%s command parameters are required", method)
	}
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode %s command parameters: %w", method, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid %s command parameters: %w", method, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s command parameters contain trailing data", method)
	}
	return nil
}

func (transport *apiTransport) Status(ctx context.Context) (model.Snapshot, error) {
	envelope, err := transport.request(ctx, http.MethodGet, "/status")
	if err != nil {
		return model.Snapshot{}, err
	}
	var snapshot model.Snapshot
	if err := json.Unmarshal(envelope.Result, &snapshot); err != nil {
		return model.Snapshot{}, fmt.Errorf("decode local API status: %w", err)
	}
	return snapshot, nil
}

func (transport *apiTransport) Execute(
	ctx context.Context,
	action Action,
) (model.Snapshot, error) {
	if err := validateAction(action); err != nil {
		return model.Snapshot{}, err
	}
	path := map[Action]string{
		ActionToggle:    "/device/toggle",
		ActionOn:        "/device/on",
		ActionOff:       "/device/off",
		ActionRefresh:   "/device/refresh",
		ActionReconnect: "/reconnect",
	}[action]
	envelope, err := transport.request(ctx, http.MethodPost, path)
	if err != nil {
		return model.Snapshot{}, err
	}
	if action == ActionRefresh {
		var snapshot model.Snapshot
		if err := json.Unmarshal(envelope.Result, &snapshot); err != nil {
			return model.Snapshot{}, fmt.Errorf("decode refreshed local API status: %w", err)
		}
		return snapshot, nil
	}
	snapshot, err := transport.Status(ctx)
	if err != nil {
		return model.Snapshot{}, &ConfirmationError{Action: action, Cause: err}
	}
	return snapshot, nil
}

func (transport *apiTransport) FetchDiagnostics(
	ctx context.Context,
	endpoint string,
) (DiagnosticResult, error) {
	if err := validateDiagnosticEndpoint(endpoint); err != nil {
		return DiagnosticResult{}, err
	}
	query := url.Values{"endpoint": []string{endpoint}}
	envelope, err := transport.request(
		ctx,
		http.MethodGet,
		"/diagnostics?"+query.Encode(),
	)
	if err != nil {
		return DiagnosticResult{}, err
	}
	return decodeDiagnosticResult(endpoint, envelope.Result)
}

func (transport *apiTransport) TerminalStatus(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	envelope, err := transport.request(ctx, http.MethodGet, "/terminal/status")
	if err != nil {
		return deviceterminal.Snapshot{}, err
	}
	var snapshot deviceterminal.Snapshot
	if err := json.Unmarshal(envelope.Result, &snapshot); err != nil {
		return deviceterminal.Snapshot{}, fmt.Errorf(
			"decode local API terminal status: %w",
			err,
		)
	}
	return snapshot, nil
}

func (transport *apiTransport) TerminalConnect(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	return transport.terminalSnapshotCommand(
		ctx,
		http.MethodPost,
		"/terminal/connect",
		nil,
	)
}

func (transport *apiTransport) TerminalDisconnect(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	return transport.terminalSnapshotCommand(
		ctx,
		http.MethodPost,
		"/terminal/disconnect",
		nil,
	)
}

func (transport *apiTransport) TerminalRead(
	ctx context.Context,
	after uint64,
	limit int,
	wait time.Duration,
) (deviceterminal.ReadResult, error) {
	if err := validateTerminalRead(limit, wait); err != nil {
		return deviceterminal.ReadResult{}, err
	}
	query := url.Values{
		"after":   []string{strconv.FormatUint(after, 10)},
		"limit":   []string{strconv.Itoa(limit)},
		"wait_ms": []string{strconv.FormatInt(wait.Milliseconds(), 10)},
	}
	envelope, err := transport.request(
		ctx,
		http.MethodGet,
		"/terminal/output?"+query.Encode(),
	)
	if err != nil {
		return deviceterminal.ReadResult{}, err
	}
	var result deviceterminal.ReadResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return deviceterminal.ReadResult{}, fmt.Errorf(
			"decode local API terminal output: %w",
			err,
		)
	}
	return result, nil
}

func (transport *apiTransport) TerminalWrite(
	ctx context.Context,
	data []byte,
) (deviceterminal.Snapshot, error) {
	if err := validateTerminalInput(data); err != nil {
		return deviceterminal.Snapshot{}, err
	}
	return transport.terminalSnapshotCommand(
		ctx,
		http.MethodPost,
		"/terminal/input",
		map[string]string{"data_base64": base64.StdEncoding.EncodeToString(data)},
	)
}

func (transport *apiTransport) terminalSnapshotCommand(
	ctx context.Context,
	method string,
	path string,
	body any,
) (deviceterminal.Snapshot, error) {
	envelope, err := transport.requestJSON(ctx, method, path, body)
	if err != nil {
		return deviceterminal.Snapshot{}, err
	}
	var snapshot deviceterminal.Snapshot
	if err := json.Unmarshal(envelope.Result, &snapshot); err != nil {
		return deviceterminal.Snapshot{}, fmt.Errorf(
			"decode local API terminal response: %w",
			err,
		)
	}
	return snapshot, nil
}

func (transport *apiTransport) request(
	ctx context.Context,
	method string,
	path string,
) (apiEnvelope, error) {
	return transport.requestJSON(ctx, method, path, nil)
}

func (transport *apiTransport) requestJSON(
	ctx context.Context,
	method string,
	path string,
	body any,
) (apiEnvelope, error) {
	var reader io.Reader = bytes.NewReader(nil)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return apiEnvelope{}, fmt.Errorf("encode local API request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		transport.baseURL+path,
		reader,
	)
	if err != nil {
		return apiEnvelope{}, fmt.Errorf("create local API request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+transport.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := transport.client.Do(request)
	if err != nil {
		return apiEnvelope{}, &redactedAPIError{
			prefix:  "local API is unavailable",
			message: transport.redact(err.Error()),
			cause:   err,
		}
	}
	defer response.Body.Close()

	responseReader := io.LimitReader(response.Body, maxAPIResponseBytes+1)
	data, err := io.ReadAll(responseReader)
	if err != nil {
		return apiEnvelope{}, fmt.Errorf("read local API response: %w", err)
	}
	if int64(len(data)) > maxAPIResponseBytes {
		return apiEnvelope{}, errors.New("local API response exceeds the size limit")
	}
	var envelope apiEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return apiEnvelope{}, fmt.Errorf(
			"local API returned HTTP %d with invalid JSON",
			response.StatusCode,
		)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.OK {
		if envelope.Error != nil {
			return apiEnvelope{}, fmt.Errorf(
				"%s: %s",
				transport.redact(envelope.Error.Code),
				transport.redact(envelope.Error.Message),
			)
		}
		return apiEnvelope{}, fmt.Errorf("local API returned HTTP %d", response.StatusCode)
	}
	return envelope, nil
}

func (transport *apiTransport) redact(message string) string {
	if transport == nil || transport.token == "" {
		return message
	}
	return strings.ReplaceAll(message, transport.token, "[redacted]")
}

func validateLoopbackAPIURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("local API URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("local API URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("local API URL must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("local API URL must not contain credentials, a query, or a fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("local API URL must not contain a path")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", errors.New("local API URL has no host")
	}
	if !strings.EqualFold(host, "localhost") {
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			return "", errors.New("local API URL must use localhost or a loopback IP address")
		}
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}
