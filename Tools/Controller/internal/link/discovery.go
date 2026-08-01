package link

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

type DiscoveryOptions struct {
	Filter         ports.Filter
	BaudRate       int
	StartupWait    time.Duration
	RequestTimeout time.Duration
	HelloAttempts  int
	// ResetAfterOpen is consulted exactly once for each newly opened physical
	// serial transport. It is never called for TCP endpoints or HELLO retries.
	ResetAfterOpen func(ports.Info) bool
	ResetPulse     time.Duration
}

type OpenResult struct {
	Session *Session
	Port    ports.Info
	Hello   native.Hello
}

func AutoOpen(ctx context.Context, options DiscoveryOptions) (OpenResult, error) {
	if options.BaudRate == 0 {
		options.BaudRate = DefaultBaudRate
	}
	if options.StartupWait == 0 {
		options.StartupWait = 350 * time.Millisecond
	}
	if options.RequestTimeout == 0 {
		options.RequestTimeout = 700 * time.Millisecond
	}
	if options.HelloAttempts == 0 {
		options.HelloAttempts = 3
	}

	if IsNetworkEndpoint(options.Filter.Port) {
		return OpenAuthenticated(ctx, ports.Info{
			Name: options.Filter.Port, Product: "PCController Virtual Board",
		}, options)
	}
	all, err := ports.List()
	if err != nil {
		return OpenResult{}, err
	}
	candidates := ports.Candidates(all, options.Filter)
	if len(candidates) == 0 {
		return OpenResult{}, errors.New("no serial ports match the configured filters")
	}
	if len(candidates) > 1 {
		if preferred, ok := ports.PreferredCandidate(
			candidates,
			options.Filter.Preferred,
		); ok {
			candidates = []ports.Info{preferred}
		} else {
			return OpenResult{}, &ports.AmbiguousError{
				Candidates: append([]ports.Info(nil), candidates...),
			}
		}
	}

	var failures []error
	for _, candidate := range candidates {
		result, err := OpenAuthenticated(ctx, candidate, options)
		if err == nil {
			return result, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", candidate.Name, err))
	}
	return OpenResult{}, errors.Join(failures...)
}

func OpenAuthenticated(
	ctx context.Context,
	port ports.Info,
	options DiscoveryOptions,
) (OpenResult, error) {
	session, err := OpenContext(ctx, port.Name, options.BaudRate)
	if err != nil {
		return OpenResult{}, err
	}
	success := false
	defer func() {
		if !success {
			_ = session.Close()
		}
	}()

	hello, err := authenticateOpened(ctx, session, port, options)
	if err != nil {
		return OpenResult{}, err
	}

	success = true
	return OpenResult{Session: session, Port: port, Hello: hello}, nil
}

func authenticateOpened(
	ctx context.Context,
	session *Session,
	port ports.Info,
	options DiscoveryOptions,
) (native.Hello, error) {
	if !IsNetworkEndpoint(port.Name) &&
		options.ResetAfterOpen != nil &&
		options.ResetAfterOpen(port) {
		pulse := options.ResetPulse
		if pulse <= 0 {
			pulse = 120 * time.Millisecond
		}
		if err := session.PulseDTR(ctx, pulse); err != nil {
			return native.Hello{}, fmt.Errorf("reset %s after reconnect: %w", port.Name, err)
		}
	}

	if options.StartupWait > 0 {
		timer := time.NewTimer(options.StartupWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return native.Hello{}, ctx.Err()
		case <-session.Done():
			return native.Hello{}, ErrClosed
		case <-timer.C:
		}
	}

	hello, err := session.AuthenticateWithRetry(
		ctx,
		options.HelloAttempts,
		options.RequestTimeout,
	)
	if err != nil {
		return native.Hello{}, fmt.Errorf("PCController application HELLO: %w", err)
	}
	return hello, nil
}
