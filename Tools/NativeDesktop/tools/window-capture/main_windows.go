//go:build windows

// Developer-only screenshot utility; never linked into RayanLamp.
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type rgbQuad struct {
	Blue, Green, Red, Reserved byte
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]rgbQuad
}

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	gdi32                    = syscall.NewLazyDLL("gdi32.dll")
	enumWindows              = user32.NewProc("EnumWindows")
	getWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	isWindowVisible          = user32.NewProc("IsWindowVisible")
	getWindowRect            = user32.NewProc("GetWindowRect")
	getForegroundWindow      = user32.NewProc("GetForegroundWindow")
	getWindowText            = user32.NewProc("GetWindowTextW")
	getWindowTextLength      = user32.NewProc("GetWindowTextLengthW")
	getWindowDC              = user32.NewProc("GetWindowDC")
	getDC                    = user32.NewProc("GetDC")
	releaseDC                = user32.NewProc("ReleaseDC")
	printWindow              = user32.NewProc("PrintWindow")
	bitBlt                   = gdi32.NewProc("BitBlt")
	createCompatibleDC       = gdi32.NewProc("CreateCompatibleDC")
	createCompatibleBitmap   = gdi32.NewProc("CreateCompatibleBitmap")
	selectObject             = gdi32.NewProc("SelectObject")
	getDIBits                = gdi32.NewProc("GetDIBits")
	deleteObject             = gdi32.NewProc("DeleteObject")
	deleteDC                 = gdi32.NewProc("DeleteDC")
	setProcessDPIAware       = user32.NewProc("SetProcessDPIAware")
	setProcessDPIContext     = user32.NewProc("SetProcessDpiAwarenessContext")
)

func main() {
	enableDPIAwareness()
	if len(os.Args) < 3 || len(os.Args) > 6 {
		fmt.Fprintln(os.Stderr, "usage: window-capture <pid> <output.png> [title substring] [print|screen] [margin]")
		os.Exit(2)
	}
	id, err := strconv.ParseUint(os.Args[1], 10, 32)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	title := ""
	if len(os.Args) >= 4 {
		title = os.Args[3]
	}
	screen := len(os.Args) == 5 && strings.EqualFold(os.Args[4], "screen")
	if len(os.Args) >= 5 {
		screen = strings.EqualFold(os.Args[4], "screen")
	}
	margin := 0
	if len(os.Args) == 6 {
		margin, err = strconv.Atoi(os.Args[5])
		if err != nil || margin < 0 || margin > 256 {
			fmt.Fprintln(os.Stderr, "margin must be an integer from 0 through 256")
			os.Exit(2)
		}
	}
	hwnd := findWindow(uint32(id), title)
	if hwnd == 0 {
		fmt.Fprintln(os.Stderr, "no visible top-level window")
		os.Exit(1)
	}
	if err := capture(hwnd, os.Args[2], screen, margin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	foreground, _, _ := getForegroundWindow.Call()
	fmt.Printf("captured hwnd=%#x title=%q foreground=%t to %s\n", hwnd, windowTitle(hwnd), foreground == hwnd, os.Args[2])
}

func findWindow(pid uint32, titleSubstring string) uintptr {
	var chosen uintptr
	wanted := strings.ToLower(strings.TrimSpace(titleSubstring))
	exact := strings.HasPrefix(wanted, "=")
	wanted = strings.TrimPrefix(wanted, "=")
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var owner uint32
		getWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		visible, _, _ := isWindowVisible.Call(hwnd)
		name := strings.ToLower(windowTitle(hwnd))
		matches := wanted == "" || strings.Contains(name, wanted)
		if exact {
			matches = name == wanted
		}
		if owner == pid && visible != 0 && matches {
			var bounds rect
			ok, _, _ := getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
			if ok != 0 && bounds.Right > bounds.Left && bounds.Bottom > bounds.Top {
				chosen = hwnd
				return 0
			}
		}
		return 1
	})
	enumWindows.Call(callback, 0)
	return chosen
}

func windowTitle(hwnd uintptr) string {
	length, _, _ := getWindowTextLength.Call(hwnd)
	text := make([]uint16, length+1)
	if len(text) > 0 {
		getWindowText.Call(hwnd, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
	}
	return syscall.UTF16ToString(text)
}

func capture(hwnd uintptr, output string, screen bool, margin int) error {
	var bounds rect
	ok, _, callErr := getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	if ok == 0 {
		return fmt.Errorf("GetWindowRect: %w", callErr)
	}
	if !screen {
		margin = 0
	}
	sourceLeft := bounds.Left - int32(margin)
	sourceTop := bounds.Top - int32(margin)
	width := int(bounds.Right-bounds.Left) + margin*2
	height := int(bounds.Bottom-bounds.Top) + margin*2
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid window dimensions %dx%d", width, height)
	}

	sourceDCProc := getWindowDC
	sourceHWND := hwnd
	if screen {
		sourceDCProc = getDC
		sourceHWND = 0
	}
	windowDC, _, callErr := sourceDCProc.Call(sourceHWND)
	if windowDC == 0 {
		return fmt.Errorf("GetWindowDC: %w", callErr)
	}
	defer releaseDC.Call(sourceHWND, windowDC)
	memoryDC, _, callErr := createCompatibleDC.Call(windowDC)
	if memoryDC == 0 {
		return fmt.Errorf("CreateCompatibleDC: %w", callErr)
	}
	defer deleteDC.Call(memoryDC)
	bitmap, _, callErr := createCompatibleBitmap.Call(windowDC, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return fmt.Errorf("CreateCompatibleBitmap: %w", callErr)
	}
	defer deleteObject.Call(bitmap)
	old, _, _ := selectObject.Call(memoryDC, bitmap)
	defer selectObject.Call(memoryDC, old)

	if screen {
		const sourceCopyCaptureLayered = 0x40CC0020
		copied, _, bitBltErr := bitBlt.Call(
			memoryDC,
			0, 0,
			uintptr(width), uintptr(height),
			windowDC,
			uintptr(sourceLeft), uintptr(sourceTop),
			sourceCopyCaptureLayered,
		)
		if copied == 0 {
			return fmt.Errorf("BitBlt: %w", bitBltErr)
		}
	} else {
		printed, _, _ := printWindow.Call(hwnd, memoryDC, 2)
		if printed == 0 {
			printed, _, callErr = printWindow.Call(hwnd, memoryDC, 0)
			if printed == 0 {
				return fmt.Errorf("PrintWindow: %w", callErr)
			}
		}
	}

	stride := ((width*32 + 31) / 32) * 4
	pixels := make([]byte, stride*height)
	info := bitmapInfo{
		Header: bitmapInfoHeader{
			Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			Width:       int32(width),
			Height:      -int32(height),
			Planes:      1,
			BitCount:    32,
			Compression: 0,
			SizeImage:   uint32(len(pixels)),
		},
	}
	lines, _, callErr := getDIBits.Call(
		memoryDC,
		bitmap,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&info)),
		0,
	)
	if lines == 0 {
		return fmt.Errorf("GetDIBits: %w", callErr)
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src := y*stride + x*4
			dst := y*img.Stride + x*4
			img.Pix[dst+0] = pixels[src+2]
			img.Pix[dst+1] = pixels[src+1]
			img.Pix[dst+2] = pixels[src+0]
			img.Pix[dst+3] = 0xff
		}
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}

func enableDPIAwareness() {
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 == (HANDLE)-4.
	if setProcessDPIContext.Find() == nil {
		ok, _, _ := setProcessDPIContext.Call(^uintptr(3))
		if ok != 0 {
			return
		}
	}
	_, _, _ = setProcessDPIAware.Call()
}
