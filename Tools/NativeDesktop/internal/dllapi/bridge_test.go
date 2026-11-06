package dllapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"pccontroller.local/desktop/sdk"
)

type fakeCaller struct {
	call func(context.Context, string, any) (sdk.Response, error)
	ping bool
}

func (f fakeCaller) Call(
	ctx context.Context,
	method string,
	params any,
) (sdk.Response, error) {
	return f.call(ctx, method, params)
}

func (f fakeCaller) Ping(context.Context) bool { return f.ping }

func TestExecuteWithPreservesRawParamsAndVersionlessEnvelope(t *testing.T) {
	t.Parallel()

	factory := func(timeout time.Duration) (caller, error) {
		if timeout != 1250*time.Millisecond {
			t.Fatalf("timeout = %s", timeout)
		}
		return fakeCaller{call: func(
			_ context.Context,
			method string,
			params any,
		) (sdk.Response, error) {
			if method != "device.future" {
				t.Fatalf("method = %q", method)
			}
			raw, ok := params.(json.RawMessage)
			if !ok || string(raw) != `{"level":7}` {
				t.Fatalf("params = %#v", params)
			}
			return sdk.Response{
				OK:     true,
				Result: json.RawMessage(`{"accepted":true}`),
			}, nil
		}}, nil
	}
	status, data := executeWith(
		factory,
		[]byte(" device.future "),
		[]byte(`{"level":7}`),
		1250,
	)
	if status != StatusOK {
		t.Fatalf("status = %d, response = %s", status, data)
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if envelope["ok"] != true {
		t.Fatalf("envelope = %#v", envelope)
	}
	if _, leaked := envelope["protocol_version"]; leaked {
		t.Fatalf("internal protocol version leaked: %s", data)
	}
}

func TestExecuteWithKeepsDispatcherFailureAtSuccessfulABIStatus(t *testing.T) {
	t.Parallel()

	status, data := executeWith(
		func(time.Duration) (caller, error) {
			return fakeCaller{call: func(
				context.Context,
				string,
				any,
			) (sdk.Response, error) {
				return sdk.Response{
					OK: false,
					Error: &sdk.Error{
						Code:    "device_unavailable",
						Message: "offline",
					},
				}, nil
			}}, nil
		},
		[]byte("device.on"),
		nil,
		0,
	)
	if status != StatusOK {
		t.Fatalf("ABI status = %d", status)
	}
	var envelope publicEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.OK || envelope.Error == nil ||
		envelope.Error.Code != "device_unavailable" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestExecuteWithValidationDoesNotCreateClient(t *testing.T) {
	t.Parallel()

	factory := func(time.Duration) (caller, error) {
		t.Fatal("factory called for invalid input")
		return nil, nil
	}
	cases := []struct {
		method  []byte
		params  []byte
		timeout uint32
	}{
		{nil, nil, 0},
		{[]byte{0xff}, nil, 0},
		{[]byte("device.on\x00"), nil, 0},
		{[]byte("device.on"), []byte(`{"broken"`), 0},
		{[]byte("device.on"), nil, 99},
		{[]byte("device.on"), nil, 600_001},
	}
	for _, testCase := range cases {
		status, data := executeWith(
			factory,
			testCase.method,
			testCase.params,
			testCase.timeout,
		)
		if status != StatusInvalidArgument {
			t.Fatalf(
				"method=%q params=%q timeout=%d status=%d response=%s",
				testCase.method,
				testCase.params,
				testCase.timeout,
				status,
				data,
			)
		}
		if !json.Valid(data) {
			t.Fatalf("invalid error JSON: %q", data)
		}
	}
}

func TestExecuteWithMapsTransportFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want int32
	}{
		{context.DeadlineExceeded, StatusTimeout},
		{context.Canceled, StatusTimeout},
		{sdk.ErrUnsupported, StatusUnsupported},
		{errors.New("pipe unavailable"), StatusUnavailable},
	}
	for _, testCase := range tests {
		status, data := executeWith(
			func(time.Duration) (caller, error) {
				return fakeCaller{call: func(
					context.Context,
					string,
					any,
				) (sdk.Response, error) {
					return sdk.Response{}, testCase.err
				}}, nil
			},
			[]byte("ping"),
			nil,
			0,
		)
		if status != testCase.want {
			t.Fatalf(
				"error=%v status=%d want=%d response=%s",
				testCase.err,
				status,
				testCase.want,
				data,
			)
		}
		if !json.Valid(data) {
			t.Fatalf("invalid failure JSON: %q", data)
		}
	}
}
