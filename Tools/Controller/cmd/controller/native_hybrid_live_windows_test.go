//go:build windows && !no_native_gui

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"pccontroller.local/controller/internal/hostui"
)

// Isolated helper attaches only to the explicitly test-owned console. Never
// detach the runner's/user's terminal to inject an integration-test key.
func TestNativeHybridKeyHelper(t *testing.T) {
	target := os.Getenv("PCCONTROLLER_HYBRID_KEY_TARGET")
	if target == "" {
		t.Skip("isolated console input helper")
	}
	pid, err := strconv.ParseUint(target, 10, 32)
	if err != nil || pid == 0 {
		t.Fatal("invalid test console PID")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	if ok, _, err := kernel.NewProc("AttachConsole").Call(uintptr(pid)); ok == 0 {
		t.Fatal(err)
	}
	defer kernel.NewProc("FreeConsole").Call()
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	type keyRecord struct {
		Type, Padding                       uint16
		Down                                int32
		Repeat, VirtualKey, Scan, Character uint16
		Control                             uint32
	}
	key, char, control := uint16(0x47), uint16(7), uint32(8) // Ctrl+G
	if os.Getenv("PCCONTROLLER_HYBRID_KEY") == "quit" {
		key, char, control = 0x51, 'q', 0
	}
	records := [2]keyRecord{{Type: 1, Down: 1, Repeat: 1, VirtualKey: key, Character: char, Control: control}, {Type: 1, Repeat: 1, VirtualKey: key, Character: char, Control: control}}
	var written uint32
	if ok, _, err := kernel.NewProc("WriteConsoleInputW").Call(input.Fd(), uintptr(unsafe.Pointer(&records[0])), 2, uintptr(unsafe.Pointer(&written))); ok == 0 || written != 2 {
		t.Fatalf("write test key: %v (%d)", err, written)
	}
}

func sendHybridTestKey(t *testing.T, pid uint32, key string) {
	t.Helper()
	if err := hybridTestKey(pid, key); err != nil {
		t.Fatal(err)
	}
}

func hybridTestKey(pid uint32, key string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, exe, "-test.run=^TestNativeHybridKeyHelper$")
	command.Env = append(os.Environ(), "PCCONTROLLER_HYBRID_KEY_TARGET="+strconv.Itoa(int(pid)), "PCCONTROLLER_HYBRID_KEY="+key)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008, HideWindow: true} // DETACHED_PROCESS
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("test-owned console key: %w: %s", err, output)
	}
	return nil
}

func TestNativeHybridPackagedBothSurfaces(t *testing.T) {
	executable, address := os.Getenv("PCCONTROLLER_HYBRID_PACKAGE_SMOKE"), os.Getenv("PCCONTROLLER_HYBRID_SMOKE_ADDR")
	if executable == "" {
		t.Skip("opt-in packaged combined-mode fixture")
	}
	if address == "" {
		t.Fatal("explicit existing idle IPC host required")
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	launch, err := nativeConsoleCommand(executable, []string{"--config", configPath, "gui", "--tui", "--hidden", "--ipc-addr", address, "--no-auto"}, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	host, err := launch.start()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { host.Wait(); close(done) }()
	var child uint32
	var root uintptr
	user := windows.NewLazySystemDLL("user32.dll")
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) && root == 0 {
		snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
		if err != nil {
			t.Fatal(err)
		}
		entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
		for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
			if entry.ParentProcessID == uint32(host.Pid) && strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(executable)) {
				child = entry.ProcessID
			}
		}
		windows.CloseHandle(snapshot)
		if child != 0 {
			user.NewProc("EnumWindows").Call(syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
				var pid uint32
				user.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
				var title, class [256]uint16
				user.NewProc("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&title[0])), 256)
				user.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&class[0])), 256)
				if pid == child && windows.UTF16ToString(title[:]) == "PCController" && windows.UTF16ToString(class[:]) != "ConsoleWindowClass" {
					root = hwnd
					return 0
				}
				return 1
			}), 0)
		}
		select {
		case <-done:
			t.Fatal("combined process exited during startup")
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Cleanup(func() {
		if child != 0 {
			if err := hybridTestKey(child, "quit"); err != nil {
				t.Log(err)
			}
		}
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			// These PIDs were created by this test, never a user's running host.
			if process, err := os.FindProcess(int(child)); err == nil && child != 0 {
				process.Kill()
			}
			host.Kill()
			t.Error("combined fixture did not exit gracefully")
		}
	})
	if root == 0 {
		t.Fatal("combined GUI HWND not created")
	}
	client := newRemoteTUIIPC(context.Background(), address, "")
	defer client.Close()
	ready := false
	for time.Now().Before(deadline) && !ready {
		var instances []hostui.AppInstance
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := client.call(ctx, "controller.app.instances", nil, &instances)
		cancel()
		if err == nil {
			for _, instance := range instances {
				ready = ready || (instance.Surface == "tui" && instance.Page != "" && instance.Self != nil && instance.Self.ProcessID == int(child))
			}
		}
		if !ready {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !ready {
		t.Fatal("full TUI did not register in the same process as the GUI")
	}
	visible := func() bool { value, _, _ := user.NewProc("IsWindowVisible").Call(root); return value != 0 }
	if visible() {
		t.Fatal("--hidden GUI started visible")
	}
	for _, want := range []bool{true, false, true} {
		sendHybridTestKey(t, child, "toggle")
		until := time.Now().Add(3 * time.Second)
		for visible() != want && time.Now().Before(until) {
			time.Sleep(25 * time.Millisecond)
		}
		if visible() != want {
			t.Fatalf("Ctrl+G visibility = %v, want %v", visible(), want)
		}
	}
	user.NewProc("PostMessageW").Call(root, 0x0010, 0, 0) // Close only this test GUI.
	until := time.Now().Add(3 * time.Second)
	for visible() && time.Now().Before(until) {
		time.Sleep(25 * time.Millisecond)
	}
	if visible() {
		t.Fatal("GUI close did not hide")
	}
	sendHybridTestKey(t, child, "toggle")
	until = time.Now().Add(3 * time.Second)
	for !visible() && time.Now().Before(until) {
		time.Sleep(25 * time.Millisecond)
	}
	if !visible() {
		t.Fatal("same GUI could not reopen after close")
	}
	t.Logf("GUI HWND %#x and full TUI share PID %d; Ctrl+G show/hide/show and close/reopen passed", root, child)
}
