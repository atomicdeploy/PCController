//go:build windows && !no_native_gui

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"pccontroller.local/controller/internal/consolewindow"
)

func testConsolePipe(t *testing.T) string {
	t.Helper()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return `\\.\pipe\pccontroller-console-test-` + hex.EncodeToString(nonce[:])
}

func TestNativeConsolePipeWaitsForChildStartup(t *testing.T) {
	pipe := testConsolePipe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		listener, err := winio.ListenPipe(pipe, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer listener.Close()
		go func() { <-ctx.Done(); listener.Close() }()
		connection, err := listener.Accept()
		if err == nil {
			connection.Close()
		}
		serverDone <- err
	}()
	connection, err := waitNativeConsolePipe(ctx, pipe, nil)
	if err != nil {
		t.Fatal(err)
	}
	connection.Close()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestNativeConsolePipeStopsOnDeadlineAndChildExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := waitNativeConsolePipe(ctx, testConsolePipe(t), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("startup deadline = %v", err)
	}
	done := make(chan struct{})
	close(done)
	if _, err := waitNativeConsolePipe(context.Background(), testConsolePipe(t), done); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("early child exit = %v", err)
	}
}

func TestNativeConsoleLaunchIsDedicatedAndKeepsTokenOffCommandLine(t *testing.T) {
	command, err := nativeConsoleCommand(`C:\Program Files\PCController\controller.exe`,
		[]string{"tui", "--ipc-token-ref", "env:TEST_TOKEN"}, []string{"TEST_TOKEN=secret-test-value"})
	if err != nil {
		t.Fatal(err)
	}
	directory, _ := windows.GetSystemDirectory()
	if !strings.EqualFold(command.Path, filepath.Join(directory, "conhost.exe")) {
		t.Fatal("console host was resolved through PATH")
	}
	if strings.Contains(strings.Join(command.Args, " "), "secret-test-value") {
		t.Fatal("token leaked into argv")
	}
	if command.Args[1] != "--" {
		t.Fatal("dedicated console is not launching an explicit client")
	}
}

func TestNativeConsoleOnlyRebindsExplicitChildStreams(t *testing.T) {
	t.Setenv(nativeConsolePipeEnv, "")
	stdin, stdout, stderr := os.Stdin, os.Stdout, os.Stderr
	if err := prepareNativeConsoleStreams(); err != nil {
		t.Fatal(err)
	}
	if os.Stdin != stdin || os.Stdout != stdout || os.Stderr != stderr {
		t.Fatal("ordinary CLI redirection was replaced")
	}
	environment := nativeConsoleEnvironment([]string{"KEEP=1", "pccontroller_native_gui_token=old", "PCCONTROLLER_GUI_CONSOLE_PIPE=old"}, "new", "new-pipe")
	if len(environment) != 3 || environment[0] != "KEEP=1" || strings.Contains(strings.Join(environment, ";"), "old") {
		t.Fatal("console child inherited stale pipe/token overrides")
	}
}

func TestNativeConsoleTokenReferenceIsConditional(t *testing.T) {
	for _, token := range []string{"", "secret-test-token"} {
		arguments := nativeConsoleArguments(`C:\config.json`, "127.0.0.1:8788", token)
		joined := strings.Join(arguments, " ")
		if strings.Contains(joined, "--ipc-token-ref") != (token != "") {
			t.Fatal("tokenless local IPC must not resolve an empty environment secret")
		}
		if strings.Contains(joined, "secret-test-token") {
			t.Fatal("token value exposed in command line")
		}
		if strings.Contains(joined, "--no-auto") || !strings.Contains(joined, "--ipc-addr 127.0.0.1:8788") {
			t.Fatal("console must remain a client of the exact existing IPC host")
		}
	}
}

type consoleHelperReport struct {
	Before, After int
	State         consolewindow.State
}

func consoleStreamMask() int {
	mask := 0
	for i, file := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		var mode uint32
		if windows.GetConsoleMode(windows.Handle(file.Fd()), &mode) == nil {
			mask |= 1 << i
		}
	}
	return mask
}

func TestNativeConsoleBridgeProcessHelper(t *testing.T) {
	if os.Getenv("PCCONTROLLER_CONSOLE_TEST_CHILD") != "1" {
		t.Skip("subprocess helper only")
	}
	before := consoleStreamMask()
	if err := prepareNativeConsoleStreams(); err != nil {
		t.Fatal(err)
	}
	// Exercise exactly the not-yet-created pipe race from the original launcher.
	time.Sleep(150 * time.Millisecond)
	report, _ := json.Marshal(consoleHelperReport{Before: before, After: consoleStreamMask(), State: consolewindow.Inspect()})
	if err := os.WriteFile(os.Getenv("PCCONTROLLER_CONSOLE_TEST_REPORT"), report, 0600); err != nil {
		t.Fatal(err)
	}
	stop, err := startNativeConsoleBridge()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(os.Getenv("PCCONTROLLER_CONSOLE_TEST_STOP")); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("test parent did not stop its console child")
}

// Test-owned console only: no input injection, board, serial, host or LAN calls.
func TestNativeConsoleOwnedWindowToggle(t *testing.T) {
	if os.Getenv("PCCONTROLLER_DESKTOP_WINDOW_TESTS") != "1" {
		t.Skip("opt-in native console integration")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reportPath, stopPath := filepath.Join(t.TempDir(), "report.json"), filepath.Join(t.TempDir(), "stop")
	launches := 0
	console := &nativeConsole{}
	console.launch = func(pipe string) (*os.Process, error) {
		launches++
		command, err := nativeConsoleCommand(executable, []string{"-test.run=^TestNativeConsoleBridgeProcessHelper$"},
			append(os.Environ(), nativeConsolePipeEnv+"="+pipe, "PCCONTROLLER_CONSOLE_TEST_CHILD=1",
				"PCCONTROLLER_CONSOLE_TEST_REPORT="+reportPath, "PCCONTROLLER_CONSOLE_TEST_STOP="+stopPath))
		if err != nil {
			return nil, err
		}
		return command.start()
	}
	t.Cleanup(func() {
		_ = os.WriteFile(stopPath, []byte("stop"), 0600)
		if console.done != nil {
			select {
			case <-console.done:
			case <-time.After(12 * time.Second):
				t.Error("test-owned console did not exit")
			}
		}
	})
	if err := console.toggle(); err != nil {
		t.Fatal(err)
	}
	if !console.visible.Load() {
		t.Fatal("new dedicated console was not visible")
	}
	var report consoleHelperReport
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.After != 7 || !report.State.SoleOwner || !report.State.Attached || !report.State.Visible {
		t.Fatalf("console handles/ownership = %+v", report)
	}
	if err := console.toggle(); err != nil {
		t.Fatal(err)
	}
	if console.visible.Load() {
		t.Fatal("console did not hide")
	}
	if err := console.toggle(); err != nil {
		t.Fatal(err)
	}
	if !console.visible.Load() || launches != 1 {
		t.Fatal("show spawned a duplicate console instead of reusing it")
	}
	t.Logf("interactive stream mask before=%d after=%d; show/hide/show reused one sole-owned console", report.Before, report.After)
}

// Explicit package smoke against an already-running IPC host. Never start a
// runtime or send hardware commands. Close only the console created by this test.
func TestNativeConsolePackagedTUI(t *testing.T) {
	executable := os.Getenv("PCCONTROLLER_CONSOLE_PACKAGE_SMOKE")
	if executable == "" {
		t.Skip("set PCCONTROLLER_CONSOLE_PACKAGE_SMOKE to a packaged host")
	}
	configPath, address := os.Getenv("PCCONTROLLER_CONSOLE_SMOKE_CONFIG"), os.Getenv("PCCONTROLLER_CONSOLE_SMOKE_ADDR")
	if configPath == "" || address == "" {
		t.Fatal("explicit smoke config and existing IPC address are required")
	}
	console := &nativeConsole{}
	var process *os.Process
	console.launch = func(pipe string) (*os.Process, error) {
		launch, err := nativeConsoleCommand(executable, nativeConsoleArguments(configPath, address, ""), nativeConsoleEnvironment(os.Environ(), "", pipe))
		if err != nil {
			return nil, err
		}
		process, err = launch.start()
		return process, err
	}
	var ownedWindow uintptr
	var ownedPID uint32
	t.Cleanup(func() {
		if ownedWindow != 0 {
			windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW").Call(ownedWindow, 0x0010, 0, 0)
			select {
			case <-console.done:
			case <-time.After(7 * time.Second):
				t.Error("test-owned TUI did not close")
			}
		}
	})
	if err := console.toggle(); err != nil {
		t.Fatal(err)
	}
	// Windows may attribute ConsoleWindowClass to its console client rather
	// than conhost. Admit only this freshly created host and its exact child.
	ownedProcesses := map[uint32]bool{uint32(process.Pid): true}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ParentProcessID == uint32(process.Pid) && strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(executable)) {
			ownedProcesses[entry.ProcessID] = true
		}
	}
	windows.CloseHandle(snapshot)
	user32 := windows.NewLazySystemDLL("user32.dll")
	callback := syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
		var pid uint32
		user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		visible, _, _ := user32.NewProc("IsWindowVisible").Call(hwnd)
		if ownedProcesses[pid] && visible != 0 {
			ownedWindow = hwnd
			ownedPID = pid
			return 0
		}
		return 1
	})
	user32.NewProc("EnumWindows").Call(callback, 0)
	if ownedWindow == 0 {
		t.Fatal("test-owned console HWND not found")
	}
	// Allow the actual remote TUI bootstrap/render to finish, not just its pipe.
	select {
	case <-console.done:
		t.Fatalf("packaged TUI exited: %v", console.exitErr)
	case <-time.After(3 * time.Second):
	}
	if tool, screenshot := os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_TOOL"), os.Getenv("PCCONTROLLER_CONSOLE_SMOKE_SCREENSHOT"); tool != "" && screenshot != "" {
		if output, err := exec.Command(tool, strconv.Itoa(int(ownedPID)), screenshot, "PCController", "print", "0").CombinedOutput(); err != nil {
			t.Fatalf("capture: %v: %s", err, output)
		}
	}
	if err := console.toggle(); err != nil {
		t.Fatal(err)
	}
	if console.visible.Load() {
		t.Fatal("packaged TUI did not hide")
	}
	if err := console.toggle(); err != nil {
		t.Fatal(err)
	}
	if !console.visible.Load() {
		t.Fatal("packaged TUI did not show again")
	}
	t.Logf("packaged full TUI alive after bootstrap; sole-owned host PID %d; show/hide/show verified", process.Pid)
}
