//go:build windows && !no_native_gui

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/desktop/desktop"
)

func prepareNativeHybridConsole(output io.Writer) (io.Writer, error) {
	if output != os.Stdout {
		return nil, errors.New("combined TUI/GUI mode requires an interactive console, not redirected output")
	}
	if terminalOutput(os.Stdin) && terminalOutput(os.Stdout) {
		return os.Stdout, nil // Includes Windows Terminal; never replace its streams.
	}
	for _, stream := range []*os.File{os.Stdin, os.Stdout} {
		kind, err := windows.GetFileType(windows.Handle(stream.Fd()))
		if err == nil && (kind == windows.FILE_TYPE_DISK || kind == windows.FILE_TYPE_PIPE) {
			return nil, errors.New("combined TUI/GUI mode cannot replace redirected input or output")
		}
	}
	state := consolewindow.Inspect()
	if state.Attached {
		if !state.SoleOwner || state.InteractiveStreams {
			return nil, errors.New("combined TUI/GUI mode cannot replace redirected or shared console streams")
		}
	} else {
		if result, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("AllocConsole").Call(); result == 0 {
			return nil, fmt.Errorf("create combined-mode console: %w", err)
		}
	}
	// Explicit combined mode may be launched without any console handles (e.g.
	// from Explorer). Only bind the newly allocated or independently owned one.
	if err := bindNativeConsoleStreams(); err != nil {
		return nil, err
	}
	return os.Stdout, nil
}

func nativeHybridConsoleActions() (func() error, func() bool) {
	state := consolewindow.Inspect()
	if !state.SoleOwner || !state.Visible {
		// A shared shell or Terminal tab is not an independently hideable window.
		return nil, func() bool { return true }
	}
	return func() error {
		state := consolewindow.Inspect()
		if !state.SoleOwner || !state.Attached {
			return errors.New("combined console is no longer independently owned")
		}
		hwnd, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		show := uintptr(5)
		if state.Visible {
			show = 0
		}
		windows.NewLazySystemDLL("user32.dll").NewProc("ShowWindow").Call(hwnd, show)
		return nil
	}, func() bool { return consolewindow.Inspect().Visible }
}

func requestNativeWindow(parent context.Context, requests chan<- desktop.WindowRequest, action string) error {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	select {
	case requests <- desktop.WindowRequest{Action: action, Result: result}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		// Never replay a toggle: it may already have changed the window.
		return fmt.Errorf("desktop window action outcome unknown: %w", ctx.Err())
	}
}
