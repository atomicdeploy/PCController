//go:build windows

package traymenu

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var testPointInRegion = windows.NewLazySystemDLL("gdi32.dll").
	NewProc("PtInRegion")

var testDwmGetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").
	NewProc("DwmGetWindowAttribute")

func TestPopupCompositionCommitsBackdropBeforeCanonicalRegions(t *testing.T) {
	session := popupSession{
		hwnd:    win.HWND(11),
		overlay: win.HWND(22),
		options: Options{
			Backdrop: BackdropAcrylic,
		},
		dark:         true,
		transparency: true,
		dimensions: dimensions{
			cornerRadius: scale(popupCornerRadiusDIP, 144),
		},
	}
	var events []string
	applyBackdrop := func(
		hwnd win.HWND,
		requested Backdrop,
		dark, transparency bool,
	) Backdrop {
		if hwnd != session.hwnd || requested != BackdropAcrylic ||
			!dark || !transparency {
			t.Fatalf(
				"backdrop arguments = hwnd=%d requested=%q dark=%v transparency=%v",
				hwnd,
				requested,
				dark,
				transparency,
			)
		}
		events = append(events, "backdrop")
		return BackdropAcrylic
	}
	applyRegion := func(
		hwnd win.HWND,
		width, height, radius int32,
	) error {
		switch hwnd {
		case session.hwnd:
			events = append(events, "parent-region")
		case session.overlay:
			events = append(events, "overlay-region")
		default:
			t.Fatalf("unexpected region HWND %d", hwnd)
		}
		if width != 447 || height != 573 ||
			radius != scale(popupCornerRadiusDIP, 144) {
			t.Fatalf(
				"region geometry = %dx%d radius %d",
				width,
				height,
				radius,
			)
		}
		return nil
	}
	if err := session.commitPopupComposition(
		447,
		573,
		applyBackdrop,
		applyRegion,
	); err != nil {
		t.Fatalf("commitPopupComposition: %v", err)
	}
	if got := strings.Join(events, ","); got !=
		"backdrop,parent-region,overlay-region" {
		t.Fatalf("composition order = %q", got)
	}
	if session.result.BackdropUsed != BackdropAcrylic {
		t.Fatalf(
			"used backdrop = %q",
			session.result.BackdropUsed,
		)
	}
}

func TestPopupCompositionReusesUnchangedCachedHost(t *testing.T) {
	session := popupSession{
		window:  &popupWindow{},
		hwnd:    win.HWND(11),
		overlay: win.HWND(22),
		options: Options{
			Backdrop: BackdropAcrylic,
		},
		dark:         true,
		transparency: true,
		dimensions: dimensions{
			cornerRadius: 12,
		},
	}
	backdropCalls, regionCalls := 0, 0
	applyBackdrop := func(
		win.HWND,
		Backdrop,
		bool,
		bool,
	) Backdrop {
		backdropCalls++
		return BackdropAcrylic
	}
	applyRegion := func(win.HWND, int32, int32, int32) error {
		regionCalls++
		return nil
	}

	if err := session.commitPopupComposition(
		320,
		480,
		applyBackdrop,
		applyRegion,
	); err != nil {
		t.Fatalf("initial composition: %v", err)
	}
	if !session.backdropChanged || backdropCalls != 1 || regionCalls != 2 {
		t.Fatalf(
			"initial calls = backdrop %d region %d changed=%v",
			backdropCalls,
			regionCalls,
			session.backdropChanged,
		)
	}

	if err := session.commitPopupComposition(
		320,
		480,
		applyBackdrop,
		applyRegion,
	); err != nil {
		t.Fatalf("cached composition: %v", err)
	}
	if session.backdropChanged || backdropCalls != 1 || regionCalls != 2 {
		t.Fatalf(
			"unchanged cached host was rebuilt: backdrop %d region %d changed=%v",
			backdropCalls,
			regionCalls,
			session.backdropChanged,
		)
	}

	if err := session.commitPopupComposition(
		360,
		480,
		applyBackdrop,
		applyRegion,
	); err != nil {
		t.Fatalf("resized composition: %v", err)
	}
	if backdropCalls != 1 || regionCalls != 4 {
		t.Fatalf(
			"resize calls = backdrop %d region %d, want 1 and 4",
			backdropCalls,
			regionCalls,
		)
	}

	session.window.invalidateComposition()
	if err := session.commitPopupComposition(
		360,
		480,
		applyBackdrop,
		applyRegion,
	); err != nil {
		t.Fatalf("invalidated composition: %v", err)
	}
	if backdropCalls != 2 || regionCalls != 6 {
		t.Fatalf(
			"invalidated calls = backdrop %d region %d, want 2 and 6",
			backdropCalls,
			regionCalls,
		)
	}
}

func TestWindowBackdropRestoresDWMCornerAfterEveryRefresh(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := windows.UTF16FromString("STATIC")
	title, _ := windows.UTF16FromString("PCControllerDesktop corner policy test")
	hwnd := win.CreateWindowEx(
		win.WS_EX_TOOLWINDOW,
		&className[0],
		&title[0],
		win.WS_POPUP,
		0,
		0,
		120,
		120,
		0,
		0,
		win.GetModuleHandle(nil),
		nil,
	)
	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		t.Fatal("create popup corner policy test window")
	}
	defer func() {
		clearWindowBackdrop(hwnd)
		win.DestroyWindow(hwnd)
	}()

	const dwmWindowCornerDoNotRound = int32(1)
	if err := setDWMInt(
		hwnd,
		dwmwaWindowCornerPref,
		dwmWindowCornerDoNotRound,
	); err != nil {
		t.Skipf("DWM corner preference unavailable: %v", err)
	}

	for _, backdrop := range []Backdrop{
		BackdropAcrylic,
		BackdropBlur,
		BackdropSolid,
	} {
		if err := setDWMInt(
			hwnd,
			dwmwaWindowCornerPref,
			dwmWindowCornerDoNotRound,
		); err != nil {
			t.Fatalf("reset corner before %s refresh: %v", backdrop, err)
		}
		_ = applyWindowBackdrop(hwnd, backdrop, true, true)
		got, err := testDWMInt32(
			hwnd,
			dwmwaWindowCornerPref,
		)
		if err != nil {
			t.Fatalf("query corner after %s refresh: %v", backdrop, err)
		}
		if got != dwmWindowCornerRound {
			t.Errorf(
				"corner after %s refresh = %d, want DWMWCP_ROUND (%d)",
				backdrop,
				got,
				dwmWindowCornerRound,
			)
		}
	}
}

func testDWMInt32(hwnd win.HWND, attribute uint32) (int32, error) {
	if err := testDwmGetWindowAttribute.Find(); err != nil {
		return 0, err
	}
	var value int32
	result, _, _ := testDwmGetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(attribute),
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
	runtime.KeepAlive(&value)
	if int32(result) < 0 {
		return 0, windows.Errno(uint32(result))
	}
	return value, nil
}

func TestNativeRoundedRegionMatchesLayeredForegroundMask(t *testing.T) {
	for _, test := range []struct {
		width, height, radius int32
	}{
		{width: 180, height: 260, radius: 10},
		{width: 225, height: 325, radius: 13},
		{width: 270, height: 390, radius: 15},
		{width: 360, height: 520, radius: 20},
	} {
		region, err := createRoundedRegion(
			test.width,
			test.height,
			test.radius,
		)
		if err != nil {
			t.Fatalf("createRoundedRegion: %v", err)
		}

		mismatches := 0
		for y := int32(0); y < test.height; y++ {
			for x := int32(0); x < test.width; x++ {
				native, _, _ := testPointInRegion.Call(
					region,
					uintptr(x),
					uintptr(y),
				)
				layered := insideRoundedRect(
					x,
					y,
					0,
					0,
					test.width,
					test.height,
					test.radius,
				)
				if (native != 0) != layered {
					mismatches++
				}
			}
		}
		win.DeleteObject(win.HGDIOBJ(region))
		if mismatches != 0 {
			t.Errorf(
				"%dx%d radius %d has %d parent/foreground corner mismatches",
				test.width,
				test.height,
				test.radius,
				mismatches,
			)
		}
	}
}

func TestRoundedForegroundCornersAreSymmetricAndContinuous(t *testing.T) {
	for _, dpi := range []int32{96, 120, 144, 192} {
		width, height, radius := scale(180, dpi), scale(260, dpi),
			scale(popupCornerRadiusDIP, dpi)
		session := popupSession{
			dimensions: dimensions{
				dpi:          dpi,
				width:        width,
				height:       height,
				cornerRadius: radius,
			},
			palette: paletteFor(true),
			result:  Result{BackdropUsed: BackdropAcrylic},
		}
		pixels := make([]byte, int(width*height*4))
		session.drawPixels(pixels)
		surfaceAlpha := materialSurfaceAlpha(BackdropAcrylic, true)
		for y := int32(0); y < height; y++ {
			for x := int32(0); x < width; x++ {
				alpha := alphaAt(pixels, width, x, y)
				inside := insideRoundedRect(
					x, y, 0, 0, width, height, radius,
				)
				if !inside && alpha != 0 {
					t.Fatalf(
						"dpi %d outside corner (%d,%d) alpha = %d",
						dpi,
						x,
						y,
						alpha,
					)
				}
				if inside && alpha < surfaceAlpha {
					t.Fatalf(
						"dpi %d inside corner (%d,%d) alpha = %d, want >= %d",
						dpi,
						x,
						y,
						alpha,
						surfaceAlpha,
					)
				}
				mirrors := [][2]int32{
					{width - 1 - x, y},
					{x, height - 1 - y},
					{width - 1 - x, height - 1 - y},
				}
				for _, mirror := range mirrors {
					if mirrored := alphaAt(
						pixels,
						width,
						mirror[0],
						mirror[1],
					); mirrored != alpha {
						t.Fatalf(
							"dpi %d alpha symmetry (%d,%d)=%d, (%d,%d)=%d",
							dpi,
							x,
							y,
							alpha,
							mirror[0],
							mirror[1],
							mirrored,
						)
					}
				}
			}
		}
	}
}

func TestTrayPopupProductionCodeDoesNotCaptureDesktopSnapshots(t *testing.T) {
	forbidden := []string{
		"BitBlt(",
		"PrintWindow",
		"GetDesktopWindow",
		"DwmRegisterThumbnail",
		"Windows.Graphics.Capture",
		"DuplicateOutput",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read traymenu package: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, token := range forbidden {
			if strings.Contains(string(source), token) {
				t.Errorf(
					"%s contains forbidden desktop-capture token %q",
					name,
					token,
				)
			}
		}
	}
}
