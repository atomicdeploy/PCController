//go:build windows

package consolewindow

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const stdOutputHandle = ^uintptr(10)

type coord struct {
	X int16
	Y int16
}

type smallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type screenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

type fontInfoEx struct {
	Size       uint32
	Font       uint32
	FontSize   coord
	FontFamily uint32
	FontWeight uint32
	FaceName   [32]uint16
}

var (
	consoleKernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	consoleUser32                   = windows.NewLazySystemDLL("user32.dll")
	procGetStdHandle                = consoleKernel32.NewProc("GetStdHandle")
	procGetConsoleMode              = consoleKernel32.NewProc("GetConsoleMode")
	procGetConsoleWindow            = consoleKernel32.NewProc("GetConsoleWindow")
	procGetConsoleScreenBufferInfo  = consoleKernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetLargestConsoleWindowSize = consoleKernel32.NewProc("GetLargestConsoleWindowSize")
	procSetConsoleScreenBufferSize  = consoleKernel32.NewProc("SetConsoleScreenBufferSize")
	procSetConsoleWindowInfo        = consoleKernel32.NewProc("SetConsoleWindowInfo")
	procSetConsoleCursorPosition    = consoleKernel32.NewProc("SetConsoleCursorPosition")
	procGetCurrentConsoleFontEx     = consoleKernel32.NewProc("GetCurrentConsoleFontEx")
	procSetCurrentConsoleFontEx     = consoleKernel32.NewProc("SetCurrentConsoleFontEx")
	procIsWindowVisible             = consoleUser32.NewProc("IsWindowVisible")
)

type windowsConsoleAPI interface {
	font(windows.Handle) (fontInfoEx, error)
	setFont(windows.Handle, fontInfoEx) error
	bufferInfo(windows.Handle) (screenBufferInfo, error)
	largestWindow(windows.Handle) (coord, error)
	setBuffer(windows.Handle, coord) error
	setWindow(windows.Handle, smallRect) error
	setCursor(windows.Handle, coord) error
}

type nativeConsoleAPI struct{}

func applyPlatform(settings Settings) (Result, error) {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return Result{Reason: "no local classic-console window is attached (redirected output or pseudoconsole)"}, nil
	}
	if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
		return Result{Reason: "the attached console window is not locally visible (pseudoconsole or hidden host)"}, nil
	}
	handleValue, _, callErr := procGetStdHandle.Call(stdOutputHandle)
	handle := windows.Handle(handleValue)
	if handle == 0 || handle == windows.InvalidHandle {
		return Result{}, windowsConsoleError("resolve standard output console", callErr)
	}
	var mode uint32
	if result, _, modeErr := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode))); result == 0 {
		if errors.Is(modeErr, windows.ERROR_INVALID_HANDLE) {
			return Result{Reason: "standard output is redirected and has no local console"}, nil
		}
		return Result{}, windowsConsoleError("query standard output console", modeErr)
	}
	if err := applyWindowsConsole(nativeConsoleAPI{}, handle, settings); err != nil {
		return Result{}, err
	}
	return Result{Applied: true}, nil
}

func applyWindowsConsole(api windowsConsoleAPI, handle windows.Handle, settings Settings) (returnErr error) {
	originalFont, err := api.font(handle)
	if err != nil {
		return fmt.Errorf("query console font: %w", err)
	}
	originalInfo, err := api.bufferInfo(handle)
	if err != nil {
		return fmt.Errorf("query console dimensions: %w", err)
	}
	font := originalFont
	font.FontSize.X = 0
	fontHeight, err := checkedInt16(settings.FontSize, "font size")
	if err != nil {
		return err
	}
	font.FontSize.Y = fontHeight
	font.FaceName = [32]uint16{}
	copy(font.FaceName[:], utf16.Encode([]rune(settings.FontFace)))
	if err := api.setFont(handle, font); err != nil {
		return fmt.Errorf("set console font to %q at %d px: %w", settings.FontFace, settings.FontSize, err)
	}
	defer func() {
		if returnErr == nil {
			return
		}
		if rollbackErr := restoreWindowsConsole(api, handle, originalFont, originalInfo); rollbackErr != nil {
			returnErr = fmt.Errorf("%w; console rollback also failed: %v", returnErr, rollbackErr)
		}
	}()

	info := originalInfo
	largest, err := api.largestWindow(handle)
	if err != nil {
		return fmt.Errorf("query maximum console dimensions: %w", err)
	}
	if settings.Columns > int(largest.X) || settings.Rows > int(largest.Y) {
		return fmt.Errorf(
			"requested console %dx%d exceeds the current display/font limit %dx%d",
			settings.Columns, settings.Rows, largest.X, largest.Y,
		)
	}
	columns, err := checkedInt16(settings.Columns, "columns")
	if err != nil {
		return err
	}
	rows, err := checkedInt16(settings.Rows, "rows")
	if err != nil {
		return err
	}
	target := coord{X: columns, Y: rows}
	currentWindow := coord{
		X: info.Window.Right - info.Window.Left + 1,
		Y: info.Window.Bottom - info.Window.Top + 1,
	}

	// Grow the backing buffer before enlarging either visible dimension.
	grown := coord{X: info.Size.X, Y: info.Size.Y}
	if target.X > grown.X {
		grown.X = target.X
	}
	if target.Y > grown.Y {
		grown.Y = target.Y
	}
	if grown != info.Size {
		if err := api.setBuffer(handle, grown); err != nil {
			return fmt.Errorf("grow console buffer: %w", err)
		}
		info.Size = grown
	}

	// Shrink the visible window before shrinking its backing buffer.
	if target.X < currentWindow.X || target.Y < currentWindow.Y {
		interim := coord{X: currentWindow.X, Y: currentWindow.Y}
		if target.X < interim.X {
			interim.X = target.X
		}
		if target.Y < interim.Y {
			interim.Y = target.Y
		}
		if err := api.setWindow(handle, rectFor(interim)); err != nil {
			return fmt.Errorf("shrink console window: %w", err)
		}
	}

	cursor := info.CursorPosition
	if cursor.X >= target.X {
		cursor.X = target.X - 1
	}
	if cursor.Y >= target.Y {
		cursor.Y = target.Y - 1
	}
	if cursor != info.CursorPosition {
		if err := api.setCursor(handle, cursor); err != nil {
			return fmt.Errorf("move console cursor inside resized buffer: %w", err)
		}
	}
	if info.Size != target {
		if err := api.setBuffer(handle, target); err != nil {
			return fmt.Errorf("set console buffer to %dx%d: %w", target.X, target.Y, err)
		}
	}
	if err := api.setWindow(handle, rectFor(target)); err != nil {
		return fmt.Errorf("set console window to %dx%d: %w", target.X, target.Y, err)
	}
	runtime.KeepAlive(api)
	return nil
}

func restoreWindowsConsole(
	api windowsConsoleAPI,
	handle windows.Handle,
	font fontInfoEx,
	info screenBufferInfo,
) error {
	var rollbackErrors []error
	current, currentErr := api.bufferInfo(handle)
	if currentErr != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("query changed console: %w", currentErr))
	} else {
		grown := current.Size
		if info.Size.X > grown.X {
			grown.X = info.Size.X
		}
		if info.Size.Y > grown.Y {
			grown.Y = info.Size.Y
		}
		if grown != current.Size {
			if err := api.setBuffer(handle, grown); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("regrow buffer: %w", err))
			}
		}
	}
	if err := api.setWindow(handle, smallRect{}); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("shrink window: %w", err))
	}
	if err := api.setCursor(handle, coord{}); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("reset cursor: %w", err))
	}
	if err := api.setFont(handle, font); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore font: %w", err))
	}
	if err := api.setBuffer(handle, info.Size); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore buffer: %w", err))
	}
	if err := api.setWindow(handle, info.Window); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore window: %w", err))
	}
	if err := api.setCursor(handle, info.CursorPosition); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore cursor: %w", err))
	}
	return errors.Join(rollbackErrors...)
}

func rectFor(size coord) smallRect {
	return smallRect{Right: size.X - 1, Bottom: size.Y - 1}
}

func checkedInt16(value int, name string) (int16, error) {
	if value < -32768 || value > 32767 {
		return 0, fmt.Errorf("%s %d is outside the Win32 16-bit coordinate range", name, value)
	}
	return int16(value), nil
}

func (nativeConsoleAPI) font(handle windows.Handle) (fontInfoEx, error) {
	value := fontInfoEx{Size: uint32(unsafe.Sizeof(fontInfoEx{}))}
	result, _, callErr := procGetCurrentConsoleFontEx.Call(
		uintptr(handle), 0, uintptr(unsafe.Pointer(&value)),
	)
	if result == 0 {
		return fontInfoEx{}, windowsConsoleError("GetCurrentConsoleFontEx", callErr)
	}
	return value, nil
}

func (nativeConsoleAPI) setFont(handle windows.Handle, value fontInfoEx) error {
	value.Size = uint32(unsafe.Sizeof(value))
	result, _, callErr := procSetCurrentConsoleFontEx.Call(
		uintptr(handle), 0, uintptr(unsafe.Pointer(&value)),
	)
	if result == 0 {
		return windowsConsoleError("SetCurrentConsoleFontEx", callErr)
	}
	return nil
}

func (nativeConsoleAPI) bufferInfo(handle windows.Handle) (screenBufferInfo, error) {
	var value screenBufferInfo
	result, _, callErr := procGetConsoleScreenBufferInfo.Call(
		uintptr(handle), uintptr(unsafe.Pointer(&value)),
	)
	if result == 0 {
		return screenBufferInfo{}, windowsConsoleError("GetConsoleScreenBufferInfo", callErr)
	}
	return value, nil
}

func (nativeConsoleAPI) largestWindow(handle windows.Handle) (coord, error) {
	result, _, callErr := procGetLargestConsoleWindowSize.Call(uintptr(handle))
	value := coord{X: int16(result & 0xffff), Y: int16((result >> 16) & 0xffff)}
	if value.X <= 0 || value.Y <= 0 {
		return coord{}, windowsConsoleError("GetLargestConsoleWindowSize", callErr)
	}
	return value, nil
}

func (nativeConsoleAPI) setBuffer(handle windows.Handle, size coord) error {
	packed := uintptr(uint16(size.X)) | uintptr(uint16(size.Y))<<16
	result, _, callErr := procSetConsoleScreenBufferSize.Call(uintptr(handle), packed)
	if result == 0 {
		return windowsConsoleError("SetConsoleScreenBufferSize", callErr)
	}
	return nil
}

func (nativeConsoleAPI) setWindow(handle windows.Handle, rect smallRect) error {
	result, _, callErr := procSetConsoleWindowInfo.Call(
		uintptr(handle), 1, uintptr(unsafe.Pointer(&rect)),
	)
	if result == 0 {
		return windowsConsoleError("SetConsoleWindowInfo", callErr)
	}
	return nil
}

func (nativeConsoleAPI) setCursor(handle windows.Handle, position coord) error {
	packed := uintptr(uint16(position.X)) | uintptr(uint16(position.Y))<<16
	result, _, callErr := procSetConsoleCursorPosition.Call(uintptr(handle), packed)
	if result == 0 {
		return windowsConsoleError("SetConsoleCursorPosition", callErr)
	}
	return nil
}

func windowsConsoleError(operation string, err error) error {
	if err == nil || errors.Is(err, syscall.Errno(0)) {
		return errors.New(operation + " failed")
	}
	return fmt.Errorf("%s: %w", operation, err)
}
