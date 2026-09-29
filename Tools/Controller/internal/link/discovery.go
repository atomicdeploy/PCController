package link

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/productidentity"
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
	// AllowPortRebind is armed only after a previously authenticated physical
	// USB session disappears. Initial and user-requested explicit opens remain
	// strict, while reconnect may replace a stale COM number with one unique
	// matching USB identity.
	AllowPortRebind bool
	// CandidateSelected reports the exact enumerated device immediately before
	// its transport is opened. UIs can therefore show what is being attempted
	// without parsing an eventual error or waiting for authentication to fail.
	CandidateSelected func(ports.Info)
}

type OpenResult struct {
	Session *Session
	Port    ports.Info
	Hello   native.Hello
}

type authenticationResult struct {
	hello native.Hello
	err   error
}

const authenticationTimeoutSlack = 500 * time.Millisecond

var openSessionContext = OpenContext

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
			Name: options.Filter.Port, Product: productidentity.DefaultAppTitle() + " Virtual Board",
		}, options)
	}
	all, err := ports.List()
	if err != nil {
		return OpenResult{}, err
	}
	candidates := ports.Candidates(all, options.Filter)
	if len(candidates) == 0 && options.AllowPortRebind {
		candidates = ports.ReconnectCandidates(all, options.Filter)
	}
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
	var lastResult OpenResult
	for _, candidate := range candidates {
		if options.CandidateSelected != nil {
			options.CandidateSelected(candidate)
		}
		result, err := OpenAuthenticated(ctx, candidate, options)
		lastResult = result
		if lastResult.Port.Name == "" {
			lastResult.Port = candidate
		}
		if err == nil {
			return result, nil
		}
		if result.Session != nil {
			// Authentication cleanup still owns a live transport. Stop discovery
			// so the caller can quarantine and retry this exact Session instead
			// of racing it with another candidate.
			return result, fmt.Errorf("%s: %w", candidate.Name, err)
		}
		failures = append(failures, fmt.Errorf("%s: %w", candidate.Name, err))
	}
	return lastResult, errors.Join(failures...)
}

func OpenAuthenticated(
	ctx context.Context,
	port ports.Info,
	options DiscoveryOptions,
) (OpenResult, error) {
	session, err := openSessionContext(ctx, port.Name, options.BaudRate)
	if err != nil {
		if session != nil {
			return OpenResult{Session: session, Port: port}, err
		}
		return OpenResult{Port: port}, err
	}

	// Authenticate can be inside a Windows overlapped ReadFile/WriteFile after
	// either an individual HELLO attempt or its caller expires. Context checks
	// around that syscall cannot interrupt the operation; only closing the
	// transport can. Bound the complete provisional-authentication lifetime and
	// give its cancellation a concrete owner as soon as the Session exists.
	authContext, cancelAuthentication := context.WithTimeout(
		ctx,
		authenticationTimeout(options),
	)
	defer cancelAuthentication()
	cancelCloseDone := make(chan error, 1)
	stopCancelClose := context.AfterFunc(authContext, func() {
		cancelCloseDone <- session.Close()
	})
	authDone := make(chan authenticationResult, 1)
	go func() {
		hello, authErr := authenticateOpened(authContext, session, port, options)
		authDone <- authenticationResult{hello: hello, err: authErr}
	}()

	select {
	case auth := <-authDone:
		if stopCancelClose() {
			// stop is the ownership boundary: after it succeeds, no late context
			// callback can close a Session returned to the caller. If cancellation
			// was already observable, reject the provisional Session explicitly.
			if ctxErr := authContext.Err(); ctxErr != nil {
				return closeFailedAuthentication(session, port, errors.Join(auth.err, ctxErr))
			}
			if auth.err != nil {
				return closeFailedAuthentication(session, port, auth.err)
			}
			return OpenResult{Session: session, Port: port, Hello: auth.hello}, nil
		}

		// Cancellation already owns Close. Join it before deciding the result so
		// a successful authentication can never race a late close callback.
		closeErr := <-cancelCloseDone
		cancelErr := authContext.Err()
		if cancelErr == nil {
			cancelErr = ErrClosed
		}
		openErr := errors.Join(auth.err, cancelErr)
		if closeErr != nil {
			return OpenResult{Session: session, Port: port}, errors.Join(
				openErr,
				fmt.Errorf("close %s after authentication cancellation: %w", port.Name, closeErr),
			)
		}
		return OpenResult{Port: port}, openErr

	case <-authContext.Done():
		// AfterFunc owns the first Close attempt. A retryable failure must return
		// the exact Session to Runtime quarantine; do not lose the handle or race
		// another open. On successful Close, join authentication before returning
		// so no provisional goroutine can publish a late successful result.
		closeErr := <-cancelCloseDone
		if closeErr != nil {
			return OpenResult{Session: session, Port: port}, errors.Join(
				authContext.Err(),
				fmt.Errorf("close %s after authentication cancellation: %w", port.Name, closeErr),
			)
		}
		auth := <-authDone
		return OpenResult{Port: port}, errors.Join(authContext.Err(), auth.err)
	}
}

func authenticationTimeout(options DiscoveryOptions) time.Duration {
	startupWait := options.StartupWait
	if startupWait < 0 {
		startupWait = 0
	}
	requestTimeout := options.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 700 * time.Millisecond
	}
	attempts := options.HelloAttempts
	if attempts < 1 {
		attempts = 1
	}
	betweenAttempts := time.Duration(attempts-1) * 100 * time.Millisecond
	return startupWait + time.Duration(attempts)*requestTimeout +
		betweenAttempts + authenticationTimeoutSlack
}

func closeFailedAuthentication(
	session *Session,
	port ports.Info,
	authErr error,
) (OpenResult, error) {
	if closeErr := session.Close(); closeErr != nil {
		return OpenResult{Session: session, Port: port}, errors.Join(
			authErr,
			fmt.Errorf("close %s after authentication failure: %w", port.Name, closeErr),
		)
	}
	return OpenResult{Port: port}, authErr
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
		return native.Hello{}, fmt.Errorf("controller application HELLO: %w", err)
	}
	return hello, nil
}
