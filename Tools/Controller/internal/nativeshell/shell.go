package nativeshell

import (
	"context"
	"errors"
	"strings"
)

// Shell owns the optional native web-mode affordance. Close is idempotent and
// waits until the platform message loop has released its resources.
type Shell interface {
	Close() error
}

// Options deliberately accepts narrow callbacks instead of a controller
// runtime. This keeps the native window/message loop isolated from serial and
// primary-IPC ownership.
type Options struct {
	Snapshot          func() State
	OpenPage          func(page string) error
	Reconnect         func(context.Context) error
	Exit              func()
	HandleSystemEvent func(SystemEvent)
	ReportError       func(error)
}

// Start creates the platform shell. Unsupported platforms return a harmless
// no-op Shell so web mode keeps identical lifecycle semantics everywhere.
func Start(ctx context.Context, options Options) (Shell, error) {
	if ctx == nil {
		return nil, errors.New("native shell context is nil")
	}
	if options.Snapshot == nil {
		return nil, errors.New("native shell snapshot callback is nil")
	}
	if options.OpenPage == nil {
		return nil, errors.New("native shell page callback is nil")
	}
	if options.Reconnect == nil {
		return nil, errors.New("native shell reconnect callback is nil")
	}
	if options.Exit == nil {
		return nil, errors.New("native shell exit callback is nil")
	}
	return startPlatform(ctx, options)
}

type noopShell struct{}

func (noopShell) Close() error { return nil }

func report(options Options, err error) {
	if err != nil && options.ReportError != nil {
		options.ReportError(err)
	}
}

func dispatch(ctx context.Context, options Options, command Command) {
	if page, ok := PageForCommand(command); ok {
		// Re-read authoritative state at dispatch time. A menu can remain open
		// while the USB transport disappears; a stale click must not launch a
		// browser for an offline controller.
		if !normalizeState(options.Snapshot()).Connected {
			return
		}
		report(options, options.OpenPage(page))
		return
	}
	switch command {
	case CommandReconnect:
		report(options, options.Reconnect(ctx))
	case CommandExit:
		options.Exit()
	}
}

func normalizeTitle(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
	if value == "" {
		return "Controller"
	}
	return value
}
