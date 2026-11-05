//go:build windows && !no_native_gui

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"io"
	"net"
	"os"
	"os/exec"
	"pccontroller.local/controller/internal/consolewindow"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const nativeConsolePipeEnv = "PCCONTROLLER_GUI_CONSOLE_PIPE"

// This server runs inside the explicitly launched TUI process. It can only
// change its own sole-owned console, and its pipe ACL admits this user only.
func startNativeConsoleBridge() func() {
	name := os.Getenv(nativeConsolePipeEnv)
	if name == "" {
		return func() {}
	}
	os.Unsetenv(nativeConsolePipeEnv)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return func() {}
	}
	listener, err := winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")"})
	if err != nil {
		return func() {}
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
	return func() { _ = listener.Close() }
}

type nativeConsole struct {
	mu      sync.Mutex
	pipe    string
	done    chan struct{}
	visible atomic.Bool
	launch  func(string) (*exec.Cmd, error)
}

func newNativeConsole(configPath, address, auth string) *nativeConsole {
	return &nativeConsole{launch: func(pipe string) (*exec.Cmd, error) {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		command := exec.Command(executable, "--config", configPath, "tui", "--ipc-addr", address, "--ipc-token-ref", "env:PCCONTROLLER_NATIVE_GUI_TOKEN")
		command.Env = append(os.Environ(), "PCCONTROLLER_NATIVE_GUI_TOKEN="+auth, nativeConsolePipeEnv+"="+pipe)
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010}
		if err := command.Start(); err != nil {
			return nil, err
		}
		return command, nil
	}}
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
		cmd, err := c.launch(c.pipe)
		if err != nil {
			return err
		}
		c.done = make(chan struct{})
		done := c.done
		go func() { _ = cmd.Wait(); c.visible.Store(false); close(done) }()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := winio.DialPipeContext(ctx, c.pipe)
	if err != nil {
		return err
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
		return &net.OpError{Op: "toggle console", Net: "pipe", Err: syscall.EINVAL}
	}
	c.visible.Store(reply.Visible)
	return nil
}
