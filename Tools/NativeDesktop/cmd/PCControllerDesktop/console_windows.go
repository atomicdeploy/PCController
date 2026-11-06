//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const attachParentProcess = ^uintptr(0)

var (
	consoleKernel32              = windows.NewLazySystemDLL("kernel32.dll")
	consoleAttachConsole         = consoleKernel32.NewProc("AttachConsole")
	consoleAllocConsole          = consoleKernel32.NewProc("AllocConsole")
	consoleFreeConsole           = consoleKernel32.NewProc("FreeConsole")
	consoleGetConsoleProcessList = consoleKernel32.NewProc(
		"GetConsoleProcessList",
	)
)

// prepareTerminalConsole keeps a CUI build on its inherited console so command
// shells wait synchronously for CLI/TUI completion. The attachment attempt is
// only a compatibility fallback for detached or older windowsgui builds.
// Redirected standard handles remain untouched.
func prepareTerminalConsole(allocateWhenDetached bool) {
	if _, err := (nativeGUIConsoleAPI{}).processIDs(); err != nil {
		// This fallback matters for a detached launch and for older GUI-
		// subsystem builds. A normal CUI launch already has its inherited
		// console and never enters this branch.
		attached, _, attachErr := consoleAttachConsole.Call(attachParentProcess)
		if attached == 0 &&
			!errors.Is(attachErr, windows.ERROR_ACCESS_DENIED) &&
			allocateWhenDetached {
			// If there is no parent console (for example a detached TUI
			// launch), create one so the interactive mode has a screen buffer.
			_, _, _ = consoleAllocConsole.Call()
		}
	}

	if !standardFileValid(os.Stdin) {
		if file := consoleStandardFile(
			windows.STD_INPUT_HANDLE,
			"CONIN$",
			os.O_RDONLY,
		); file != nil {
			os.Stdin = file
		}
	}
	if !standardFileValid(os.Stdout) {
		if file := consoleStandardFile(
			windows.STD_OUTPUT_HANDLE,
			"CONOUT$",
			os.O_WRONLY,
		); file != nil {
			os.Stdout = file
		}
	}
	if !standardFileValid(os.Stderr) {
		if file := consoleStandardFile(
			windows.STD_ERROR_HANDLE,
			"CONOUT$",
			os.O_WRONLY,
		); file != nil {
			os.Stderr = file
		}
	}
}

// guiConsoleAPI isolates the native ownership and window operations so the
// release decision can be exercised without touching the test runner's
// console.
type guiConsoleAPI interface {
	currentProcessID() uint32
	processIDs() ([]uint32, error)
	free() error
}

type nativeGUIConsoleAPI struct{}

// prepareGUIConsole removes only the private console Windows creates when a
// CUI executable is launched outside an existing console. If cmd, PowerShell,
// Windows Terminal, or another process shares the console, it is inherited
// user state and is never hidden or detached.
func prepareGUIConsole() {
	prepareGUIConsoleWith(nativeGUIConsoleAPI{})
}

func prepareGUIConsoleWith(api guiConsoleAPI) {
	processes, err := api.processIDs()
	if err != nil ||
		!shouldReleaseGUIConsole(api.currentProcessID(), processes) {
		return
	}
	// Do not hide the console window before detaching. Another process could
	// attach after the ownership check; FreeConsole affects only this process,
	// whereas ShowWindow could accidentally hide the newly shared console.
	// When this process really is the last client, conhost closes naturally.
	_ = api.free()
}

// shouldReleaseGUIConsole is intentionally strict: after ignoring impossible
// zero entries, exactly one process must own the console and it must be this
// process. Any ambiguity preserves the console.
func shouldReleaseGUIConsole(
	currentProcessID uint32,
	processes []uint32,
) bool {
	if currentProcessID == 0 {
		return false
	}
	owners := 0
	soleOwner := uint32(0)
	for _, processID := range processes {
		if processID == 0 {
			continue
		}
		owners++
		soleOwner = processID
		if owners > 1 {
			return false
		}
	}
	return owners == 1 && soleOwner == currentProcessID
}

func (nativeGUIConsoleAPI) currentProcessID() uint32 {
	return uint32(os.Getpid())
}

func (nativeGUIConsoleAPI) processIDs() ([]uint32, error) {
	processes := make([]uint32, 8)
	for {
		count, _, callErr := consoleGetConsoleProcessList.Call(
			uintptr(unsafe.Pointer(&processes[0])),
			uintptr(len(processes)),
		)
		if count == 0 {
			return nil, fmt.Errorf(
				"query console processes: %w",
				callErr,
			)
		}
		if count <= uintptr(len(processes)) {
			return processes[:count], nil
		}
		if count > 1<<16 {
			return nil, fmt.Errorf(
				"query console processes: unreasonable owner count %d",
				count,
			)
		}
		processes = make([]uint32, int(count))
	}
}

func (nativeGUIConsoleAPI) free() error {
	released, _, callErr := consoleFreeConsole.Call()
	if released == 0 {
		return fmt.Errorf("free private GUI console: %w", callErr)
	}
	return nil
}

func standardFileValid(file *os.File) bool {
	if file == nil {
		return false
	}
	handle := windows.Handle(file.Fd())
	if handle == 0 || handle == windows.InvalidHandle {
		return false
	}
	fileType, err := windows.GetFileType(handle)
	return err == nil && fileType != windows.FILE_TYPE_UNKNOWN
}

func consoleStandardFile(stdHandle uint32, name string, flags int) *os.File {
	handle, err := windows.GetStdHandle(stdHandle)
	if err == nil && handle != 0 && handle != windows.InvalidHandle {
		if fileType, fileErr := windows.GetFileType(handle); fileErr == nil &&
			fileType != windows.FILE_TYPE_UNKNOWN {
			return os.NewFile(uintptr(handle), name)
		}
	}

	file, err := os.OpenFile(name, flags, 0)
	if err != nil {
		return nil
	}
	if err := windows.SetStdHandle(stdHandle, windows.Handle(file.Fd())); err != nil {
		_ = file.Close()
		return nil
	}
	return file
}
