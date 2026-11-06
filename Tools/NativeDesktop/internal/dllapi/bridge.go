// Package dllapi implements the cgo-independent policy behind the optional
// native SDK DLL.
package dllapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"pccontroller.local/desktop/sdk"
)

const (
	// ABIVersion encodes major 1, minor 0.
	ABIVersion uint32 = 0x00010000

	StatusOK              int32 = 0
	StatusInvalidArgument int32 = -1
	StatusUnavailable     int32 = -2
	StatusTimeout         int32 = -3
	StatusUnsupported     int32 = -4
	StatusInternal        int32 = -5

	MaxInputBytes = 1 << 20
)

type caller interface {
	Call(context.Context, string, any) (sdk.Response, error)
	Ping(context.Context) bool
}

type clientFactory func(time.Duration) (caller, error)

var productionFactory clientFactory = func(
	timeout time.Duration,
) (caller, error) {
	return sdk.New(sdk.Options{Timeout: timeout})
}

type publicEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *sdk.Error      `json:"error,omitempty"`
}

// Execute validates an ABI call and invokes the public attach-only Go SDK.
// It returns a JSON envelope for both dispatcher and ABI/transport failures.
func Execute(
	method []byte,
	params []byte,
	timeoutMilliseconds uint32,
) (int32, []byte) {
	return executeWith(
		productionFactory,
		method,
		params,
		timeoutMilliseconds,
	)
}

func executeWith(
	factory clientFactory,
	method []byte,
	params []byte,
	timeoutMilliseconds uint32,
) (int32, []byte) {
	if factory == nil {
		return failure(
			StatusInternal,
			"internal_error",
			"PCControllerDesktop SDK client factory is unavailable",
		)
	}
	if len(method) == 0 || len(method) > 64 ||
		len(method)+len(params) > MaxInputBytes ||
		!utf8.Valid(method) {
		return failure(
			StatusInvalidArgument,
			"invalid_argument",
			"method must be valid UTF-8 between 1 and 64 bytes",
		)
	}
	methodText := strings.TrimSpace(string(method))
	if methodText == "" {
		return failure(
			StatusInvalidArgument,
			"invalid_argument",
			"method is required",
		)
	}
	for _, character := range methodText {
		if character < 0x20 || character == 0x7f {
			return failure(
				StatusInvalidArgument,
				"invalid_argument",
				"method contains a control character",
			)
		}
	}
	if len(params) > 0 && !json.Valid(params) {
		return failure(
			StatusInvalidArgument,
			"invalid_argument",
			"parameters must contain exactly one valid JSON value",
		)
	}
	timeout, ok := timeoutDuration(timeoutMilliseconds)
	if !ok {
		return failure(
			StatusInvalidArgument,
			"invalid_argument",
			"timeout must be zero or between 100 and 600000 milliseconds",
		)
	}
	client, err := factory(timeout)
	if err != nil {
		return transportFailure(err)
	}

	var decodedParams any
	if len(params) > 0 {
		// RawMessage makes ipc.NewRequest preserve the already validated JSON
		// value without interpreting future dispatcher parameters here.
		decodedParams = json.RawMessage(append([]byte(nil), params...))
	}
	response, err := client.Call(
		context.Background(),
		methodText,
		decodedParams,
	)
	if err != nil {
		return transportFailure(err)
	}
	envelope := publicEnvelope{
		OK:     response.OK,
		Result: response.Result,
		Error:  response.Error,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return failure(
			StatusInternal,
			"internal_error",
			"unable to encode PCControllerDesktop SDK response",
		)
	}
	return StatusOK, data
}

// IsRunning returns one when the primary answers ping, zero when unavailable,
// or a negative status for invalid ABI input.
func IsRunning(timeoutMilliseconds uint32) int32 {
	timeout, ok := timeoutDuration(timeoutMilliseconds)
	if !ok {
		return StatusInvalidArgument
	}
	client, err := productionFactory(timeout)
	if err != nil {
		if errors.Is(err, sdk.ErrUnsupported) {
			return StatusUnsupported
		}
		return StatusInternal
	}
	if client.Ping(context.Background()) {
		return 1
	}
	return 0
}

func timeoutDuration(milliseconds uint32) (time.Duration, bool) {
	if milliseconds == 0 {
		return sdk.DefaultTimeout, true
	}
	if milliseconds < 100 || milliseconds > 600_000 {
		return 0, false
	}
	return time.Duration(milliseconds) * time.Millisecond, true
}

func transportFailure(err error) (int32, []byte) {
	switch {
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		return failure(StatusTimeout, "timeout", "PCControllerDesktop SDK call timed out")
	case errors.Is(err, sdk.ErrUnsupported):
		return failure(
			StatusUnsupported,
			"unsupported",
			sdk.ErrUnsupported.Error(),
		)
	default:
		return failure(
			StatusUnavailable,
			"unavailable",
			"the authoritative PCControllerDesktop primary is unavailable",
		)
	}
}

func failure(status int32, code, message string) (int32, []byte) {
	data, err := json.Marshal(publicEnvelope{
		OK: false,
		Error: &sdk.Error{
			Code:    code,
			Message: message,
		},
	})
	if err != nil {
		return StatusInternal, []byte(
			`{"ok":false,"error":{"code":"internal_error","message":"unable to encode error"}}`,
		)
	}
	return status, data
}
