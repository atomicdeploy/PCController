//go:build windows

package winfx

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

func TestKeyboardCaptureRejectsNilHandler(t *testing.T) {
	t.Parallel()

	capture, err := StartKeyboardCapture(KeyboardCaptureOptions{}, nil)
	if capture != nil || !errors.Is(err, ErrNilKeyboardCaptureHandler) {
		t.Fatalf("StartKeyboardCapture(nil) = %#v, %v", capture, err)
	}
}

func TestKeyboardCaptureLifecycleIsExclusiveAndIdempotent(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	capture, err := StartKeyboardCapture(
		KeyboardCaptureOptions{},
		func(KeyboardCaptureEvent) {},
	)
	if err != nil {
		t.Fatalf("StartKeyboardCapture: %v", err)
	}
	if !capture.Active() {
		t.Fatal("new capture is not active")
	}
	if got := capture.OwnerThreadID(); got == 0 ||
		got != windows.GetCurrentThreadId() {
		t.Fatalf("OwnerThreadID = %d, current = %d", got, windows.GetCurrentThreadId())
	}

	other, err := StartKeyboardCapture(
		KeyboardCaptureOptions{},
		func(KeyboardCaptureEvent) {},
	)
	if other != nil || !errors.Is(err, ErrKeyboardCaptureActive) {
		t.Fatalf("second StartKeyboardCapture = %#v, %v", other, err)
	}

	if err := capture.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if capture.Active() {
		t.Fatal("closed capture is active")
	}
	if err := capture.Close(); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}

	replacement, err := StartKeyboardCapture(
		KeyboardCaptureOptions{},
		func(KeyboardCaptureEvent) {},
	)
	if err != nil {
		t.Fatalf("replacement StartKeyboardCapture: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatalf("replacement Close: %v", err)
	}
}

func TestKeyboardCaptureReceivesInjectedSendInput(t *testing.T) {
	if os.Getenv("RAYANLAMP_INTERACTIVE_TESTS") != "1" {
		t.Skip("set RAYANLAMP_INTERACTIVE_TESTS=1 to inject an F24 test chord")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	events := make(chan KeyboardCaptureEvent, 4)
	capture, err := StartKeyboardCapture(
		KeyboardCaptureOptions{},
		func(event KeyboardCaptureEvent) {
			events <- event
		},
	)
	if err != nil {
		t.Fatalf("StartKeyboardCapture: %v", err)
	}
	defer func() {
		if err := capture.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	keybdEvent := windows.NewLazySystemDLL("user32.dll").NewProc("keybd_event")
	if err := keybdEvent.Find(); err != nil {
		t.Skipf("resolve keybd_event: %v", err)
	}
	sent := make(chan struct{}, 1)
	go func() {
		keybdEvent.Call(win.VK_F24, 0, 0, 0)
		keybdEvent.Call(win.VK_F24, 0, win.KEYEVENTF_KEYUP, 0)
		sent <- struct{}{}
	}()

	deadline := time.Now().Add(2 * time.Second)
	var captured []KeyboardCaptureEvent
	inputDone := false
	for (len(captured) < 2 || !inputDone) && time.Now().Before(deadline) {
		var message win.MSG
		for win.PeekMessage(&message, 0, 0, 0, win.PM_REMOVE) {
			win.TranslateMessage(&message)
			win.DispatchMessage(&message)
		}
		for {
			select {
			case event := <-events:
				captured = append(captured, event)
			case <-sent:
				inputDone = true
			default:
				goto drained
			}
		}
	drained:
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if !inputDone {
		t.Fatal("keybd_event did not return")
	}
	if len(captured) != 2 {
		t.Skipf("interactive desktop did not deliver injected events to the hook")
	}
	if !captured[0].KeyDown || captured[0].KeyUp ||
		!captured[1].KeyUp || captured[1].KeyDown {
		t.Fatalf("captured direction sequence = %+v", captured)
	}
	for _, event := range captured {
		if event.VirtualKey != win.VK_F24 ||
			event.RawVirtualKey != win.VK_F24 ||
			!event.Injected {
			t.Fatalf("captured injected event = %+v", event)
		}
	}
}

func TestKeyboardCaptureDispatchIncludesInjectedByDefault(t *testing.T) {
	var got KeyboardCaptureEvent
	capture := &KeyboardCapture{
		handler:   func(event KeyboardCaptureEvent) { got = event },
		held:      make(map[uint32]bool),
		accepting: true,
	}
	input := lowLevelKeyboardInput{
		VirtualKey: 'K',
		ScanCode:   0x25,
		Flags:      keyboardFlagInjected,
	}
	if suppressed := capture.dispatch(keyboardMessageKeyDown, &input); !suppressed {
		t.Fatal("default capture passed injected key through")
	}
	if !got.Injected || got.VirtualKey != 'K' || !got.KeyDown {
		t.Fatalf("injected event = %+v", got)
	}
}

func TestKeyboardCaptureCanPassInjectedInputThrough(t *testing.T) {
	called := false
	capture := &KeyboardCapture{
		handler:   func(KeyboardCaptureEvent) { called = true },
		options:   KeyboardCaptureOptions{IgnoreInjected: true},
		held:      make(map[uint32]bool),
		accepting: true,
	}
	input := lowLevelKeyboardInput{
		VirtualKey: 'K',
		Flags:      keyboardFlagInjected,
	}
	if suppressed := capture.dispatch(keyboardMessageKeyDown, &input); suppressed {
		t.Fatal("IgnoreInjected capture suppressed injected key")
	}
	if called {
		t.Fatal("IgnoreInjected capture delivered injected key")
	}
}

func TestKeyboardCapturePassesEveryKeyThroughAfterAcceptanceStops(t *testing.T) {
	called := false
	capture := &KeyboardCapture{
		handler: func(KeyboardCaptureEvent) { called = true },
		held:    make(map[uint32]bool),
		// This is the fail-open state retained after an unhook error.
		accepting: false,
	}
	input := lowLevelKeyboardInput{VirtualKey: 'K'}
	if suppressed := capture.dispatch(
		keyboardMessageKeyDown,
		&input,
	); suppressed {
		t.Fatal("inactive retained capture suppressed a physical key")
	}
	if called {
		t.Fatal("inactive retained capture delivered a keyboard event")
	}
}

func TestKeyboardCaptureRecoversHandlerPanicAndStillSuppresses(t *testing.T) {
	capture := &KeyboardCapture{
		handler: func(KeyboardCaptureEvent) {
			panic("test panic")
		},
		held:      make(map[uint32]bool),
		accepting: true,
	}
	input := lowLevelKeyboardInput{VirtualKey: 'P'}
	if suppressed := capture.dispatch(keyboardMessageKeyDown, &input); !suppressed {
		t.Fatal("panicking handler did not suppress captured key")
	}
}

func TestKeyboardCaptureMarksRepeatedKeyDown(t *testing.T) {
	var events []KeyboardCaptureEvent
	capture := &KeyboardCapture{
		handler:   func(event KeyboardCaptureEvent) { events = append(events, event) },
		held:      make(map[uint32]bool),
		accepting: true,
	}
	input := lowLevelKeyboardInput{VirtualKey: 'R'}
	capture.dispatch(keyboardMessageKeyDown, &input)
	capture.dispatch(keyboardMessageKeyDown, &input)
	capture.dispatch(keyboardMessageKeyUp, &input)
	capture.dispatch(keyboardMessageKeyDown, &input)
	if len(events) != 4 ||
		events[0].Repeat ||
		!events[1].Repeat ||
		events[2].Repeat ||
		events[3].Repeat {
		t.Fatalf("repeat sequence = %+v", events)
	}
}
