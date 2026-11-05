// Package sdk provides the supported Go client for controlling the authoritative
// PCControllerDesktop primary instance.
//
// The client is attach-only. It never creates a second device connection, loads
// configuration, or starts a GUI. Calls use the same current-user named pipe
// as the CLI, COM server, URI handler, and other local integrations.
package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"pccontroller.local/desktop/internal/ipc"
)

const (
	// DefaultTimeout is used when Options.Timeout is zero.
	DefaultTimeout = 12 * time.Second

	minTimeout = 100 * time.Millisecond
	maxTimeout = 10 * time.Minute
)

var (
	// ErrUnsupported reports that the named-pipe client is unavailable on the
	// current operating system.
	ErrUnsupported = errors.New("PCControllerDesktop SDK is only supported on Windows")

	// ErrInvalidOptions reports invalid client construction options.
	ErrInvalidOptions = errors.New("invalid PCControllerDesktop SDK options")
)

// Options configures an attach-only Client.
type Options struct {
	// Timeout bounds calls whose context has no earlier deadline. Zero selects
	// DefaultTimeout.
	Timeout time.Duration
}

// Error is a command error returned by the authoritative dispatcher.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Response is the versionless public SDK response envelope.
//
// A completed transport call may still return OK=false when the dispatcher
// rejected the command. Transport failures are returned as Go errors.
type Response struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// DecodeResult decodes a successful command result.
func (r Response) DecodeResult(target any) error {
	if !r.OK {
		if r.Error == nil {
			return errors.New("PCControllerDesktop command failed")
		}
		return fmt.Errorf("%s: %s", r.Error.Code, r.Error.Message)
	}
	if target == nil || len(r.Result) == 0 || string(r.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(r.Result, target); err != nil {
		return fmt.Errorf("decode PCControllerDesktop command result: %w", err)
	}
	return nil
}

type callFunc func(context.Context, ipc.Request) (ipc.Response, error)

// Client sends commands to the current user's primary PCControllerDesktop instance.
// Client is safe for concurrent use.
type Client struct {
	timeout time.Duration
	call    callFunc
	nextID  atomic.Uint64
}

// New constructs an attach-only client. It performs no I/O; use Ping when a
// caller wants to probe availability eagerly.
func New(options Options) (*Client, error) {
	timeout, err := normalizeTimeout(options.Timeout)
	if err != nil {
		return nil, err
	}
	return &Client{
		timeout: timeout,
		call:    platformCall,
	}, nil
}

func newClientWithCall(options Options, call callFunc) (*Client, error) {
	client, err := New(options)
	if err != nil {
		return nil, err
	}
	if call == nil {
		return nil, fmt.Errorf("%w: nil transport", ErrInvalidOptions)
	}
	client.call = call
	return client, nil
}

func normalizeTimeout(timeout time.Duration) (time.Duration, error) {
	if timeout == 0 {
		return DefaultTimeout, nil
	}
	if timeout < minTimeout || timeout > maxTimeout {
		return 0, fmt.Errorf(
			"%w: timeout must be between %s and %s",
			ErrInvalidOptions,
			minTimeout,
			maxTimeout,
		)
	}
	return timeout, nil
}

// Call invokes one canonical dispatcher method.
func (c *Client) Call(
	ctx context.Context,
	method string,
	params any,
) (Response, error) {
	if c == nil || c.call == nil {
		return Response{}, errors.New("PCControllerDesktop SDK client is not initialized")
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return Response{}, errors.New("PCControllerDesktop SDK method is required")
	}
	if !utf8.ValidString(method) {
		return Response{}, errors.New("PCControllerDesktop SDK method must be valid UTF-8")
	}
	if len(method) > 64 {
		return Response{}, errors.New("PCControllerDesktop SDK method exceeds 64 bytes")
	}
	for _, character := range method {
		if character < 0x20 || character == 0x7f {
			return Response{}, errors.New("PCControllerDesktop SDK method contains a control character")
		}
	}

	request, err := ipc.NewRequest(
		fmt.Sprintf("sdk-%d", c.nextID.Add(1)),
		method,
		params,
	)
	if err != nil {
		return Response{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callContext, cancel := withDefaultTimeout(ctx, c.timeout)
	defer cancel()

	raw, err := c.call(callContext, request)
	if err != nil {
		return Response{}, err
	}
	response := Response{
		OK:     raw.OK,
		Result: append(json.RawMessage(nil), raw.Result...),
	}
	if raw.Error != nil {
		response.Error = &Error{
			Code:    raw.Error.Code,
			Message: raw.Error.Message,
		}
	}
	return response, nil
}

func withDefaultTimeout(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok &&
		time.Until(deadline) <= timeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

// Ping reports whether the authoritative primary completed a ping command.
func (c *Client) Ping(ctx context.Context) bool {
	response, err := c.Call(ctx, "ping", nil)
	return err == nil && response.OK
}

// Status returns the current authoritative state snapshot.
func (c *Client) Status(ctx context.Context) (Response, error) {
	return c.Call(ctx, "status", nil)
}

// On asks the primary to turn the device on.
func (c *Client) On(ctx context.Context) (Response, error) {
	return c.Call(ctx, "device.on", nil)
}

// Off asks the primary to turn the device off.
func (c *Client) Off(ctx context.Context) (Response, error) {
	return c.Call(ctx, "device.off", nil)
}

// Toggle asks the primary to toggle the device.
func (c *Client) Toggle(ctx context.Context) (Response, error) {
	return c.Call(ctx, "device.toggle", nil)
}
