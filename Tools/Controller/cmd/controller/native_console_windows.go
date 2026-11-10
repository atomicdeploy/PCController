//go:build windows && !no_native_gui

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"io"
	"net"
	"os"
	"path/filepath"
	"pccontroller.local/controller/internal/consolewindow"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const nativeConsolePipeEnv = "PCCONTROLLER_GUI_CONSOLE_PIPE"

// This server runs inside the explicitly launched TUI process. It can only
// change its own sole-owned console, and its pipe ACL admits this user only.
func startNativeConsoleBridge() (func(), error) {
	name := os.Getenv(nativeConsolePipeEnv)
	if name == "" {
		return func() {}, nil
	}
	os.Unsetenv(nativeConsolePipeEnv)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("identify console owner: %w", err)
	}
	listener, err := winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")"})
	if err != nil {
		return nil, fmt.Errorf("start TUI console control: %w", err)
	}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
				var request struct{ Toggle bool }
				if json.NewDecoder(io.LimitReader(connection, 128)).Decode(&request) != nil {
					return
				}
				state := consolewindow.Inspect()
				reply := struct {
					Visible bool
					Error   string
				}{Visible: state.Visible}
				if !state.Attached || !state.SoleOwner {
					reply.Error = "TUI console is not independently owned"
				} else if request.Toggle {
					hwnd, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
					show := uintptr(5)
					if state.Visible {
						show = 0
					}
					windows.NewLazySystemDLL("user32.dll").NewProc("ShowWindow").Call(hwnd, show)
					reply.Visible = consolewindow.Inspect().Visible
				}
				_ = json.NewEncoder(connection).Encode(reply)
			}()
		}
	}()
	return func() { _ = listener.Close() }, nil
}

// Only the explicitly launched GUI child rebinds streams. Go's exec.Cmd uses
// NUL for nil streams, even with CREATE_NEW_CONSOLE; allocating a window alone
// does not make those handles interactive. Never change a user's redirected CLI.
func prepareNativeConsoleStreams() error {
	if os.Getenv(nativeConsolePipeEnv) == "" {
		return nil
	}
	return bindNativeConsoleStreams()
}

func bindNativeConsoleStreams() error {
	var files [3]*os.File
	for i, name := range []string{"CONIN$", "CONOUT$", "CONOUT$"} {
		file, err := os.OpenFile(name, os.O_RDWR, 0)
		if err != nil {
			for _, opened := range files {
				if opened != nil {
					_ = opened.Close()
				}
			}
			return fmt.Errorf("open TUI console streams: %w", err)
		}
		files[i] = file
	}
	handles := []uint32{windows.STD_INPUT_HANDLE, windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE}
	var previous [3]windows.Handle
	for i, id := range handles {
		previous[i], _ = windows.GetStdHandle(id)
		if err := windows.SetStdHandle(id, windows.Handle(files[i].Fd())); err != nil {
			for j := 0; j < i; j++ {
				_ = windows.SetStdHandle(handles[j], previous[j])
			}
			for _, file := range files {
				_ = file.Close()
			}
			return fmt.Errorf("bind TUI console streams: %w", err)
		}
	}
	os.Stdin, os.Stdout, os.Stderr = files[0], files[1], files[2]
	return nil
}

type nativeConsoleLaunch struct {
	Path      string
	Args, Env []string
}

func (launch *nativeConsoleLaunch) start() (*os.Process, error) {
	// Unlike exec.Cmd.Start, zero Files entries mean actual NULL startup
	// handles, not NUL files. conhost interprets valid inherited streams as a
	// ConPTY session; passing NUL/pipes makes it exit at EOF instead of opening
	// the independently owned visible console requested here.
	pid, handle, err := syscall.StartProcess(launch.Path, launch.Args,
		&syscall.ProcAttr{Env: launch.Env, Files: []uintptr{0, 0, 0}})
	if err != nil {
		return nil, err
	}
	process, err := os.FindProcess(pid)
	_ = syscall.CloseHandle(syscall.Handle(handle))
	return process, err
}

func nativeConsoleCommand(executable string, arguments, environment []string) (*nativeConsoleLaunch, error) {
	directory, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, fmt.Errorf("locate Windows console host: %w", err)
	}
	// A dedicated system console can be hidden independently. A Terminal tab's
	// GetConsoleWindow is a message-only HWND, not the visible Terminal window;
	// hiding that shared Terminal window could hide unrelated tabs as well.
	args := []string{"--", executable}
	args = append(args, arguments...)
	host := filepath.Join(directory, "conhost.exe")
	return &nativeConsoleLaunch{Path: host, Args: append([]string{host}, args...), Env: environment}, nil
}

type nativeConsole struct {
	mu      sync.Mutex
	pipe    string
	done    chan struct{}
	exitErr error // written before closing done; read only after done is closed
	visible atomic.Bool
	launch  func(string) (*os.Process, error)
}

func newNativeConsole(configPath, address, auth string) *nativeConsole {
	return &nativeConsole{launch: func(pipe string) (*os.Process, error) {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		command, err := nativeConsoleCommand(executable,
			nativeConsoleArguments(configPath, address, auth),
			nativeConsoleEnvironment(os.Environ(), auth, pipe))
		if err != nil {
			return nil, err
		}
		return command.start()
	}}
}

func nativeConsoleArguments(configPath, address, auth string) []string {
	args := []string{"--config", configPath, "tui", "--ipc-addr", address}
	if auth != "" {
		args = append(args, "--ipc-token-ref", "env:PCCONTROLLER_NATIVE_GUI_TOKEN")
	}
	return args
}

func nativeConsoleEnvironment(environment []string, auth, pipe string) []string {
	result := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "PCCONTROLLER_NATIVE_GUI_TOKEN") && !strings.EqualFold(key, nativeConsolePipeEnv) {
			result = append(result, entry)
		}
	}
	return append(result, "PCCONTROLLER_NATIVE_GUI_TOKEN="+auth, nativeConsolePipeEnv+"="+pipe)
}
func (c *nativeConsole) toggle() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done != nil {
		select {
		case <-c.done:
			c.done = nil
		default:
		}
	}
	first := c.done == nil
	if first {
		token := make([]byte, 16)
		if _, err := rand.Read(token); err != nil {
			return err
		}
		c.pipe = `\\.\pipe\pccontroller-console-` + hex.EncodeToString(token)
		process, err := c.launch(c.pipe)
		if err != nil {
			return err
		}
		c.done = make(chan struct{})
		done := c.done
		go func() {
			state, err := process.Wait()
			if err == nil && !state.Success() {
				err = fmt.Errorf("exit code %d", state.ExitCode())
			}
			c.exitErr = err
			c.visible.Store(false)
			close(done)
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := waitNativeConsolePipe(ctx, c.pipe, c.done)
	if err != nil {
		select {
		case <-c.done:
			if c.exitErr != nil {
				return fmt.Errorf("TUI closed during startup: %w", c.exitErr)
			}
			return errors.New("TUI closed during startup")
		default:
			return fmt.Errorf("connect to TUI console: %w", err)
		}
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	if err = json.NewEncoder(connection).Encode(struct{ Toggle bool }{Toggle: !first}); err != nil {
		return err
	}
	var reply struct {
		Visible bool
		Error   string
	}
	if err = json.NewDecoder(io.LimitReader(connection, 1024)).Decode(&reply); err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("TUI console: %s", reply.Error)
	}
	c.visible.Store(reply.Visible)
	return nil
}

func waitNativeConsolePipe(ctx context.Context, pipe string, done <-chan struct{}) (net.Conn, error) {
	for {
		select {
		case <-done:
			return nil, errors.New("console process exited")
		default:
		}
		connection, err := winio.DialPipeContext(ctx, pipe)
		if err == nil {
			return connection, nil
		}
		// go-winio retries PIPE_BUSY, but FILE_NOT_FOUND is returned immediately
		// while the child is still loading. Retry startup discovery, not commands.
		if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			var pathErr *os.PathError
			if errors.As(err, &pathErr) {
				return nil, pathErr.Err
			}
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-done:
			timer.Stop()
			return nil, errors.New("console process exited")
		case <-timer.C:
		}
	}
}
