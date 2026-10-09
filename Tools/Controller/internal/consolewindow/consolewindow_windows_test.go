//go:build windows

package consolewindow

import (
	"fmt"
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

type fakeWindowsConsole struct {
	info    screenBufferInfo
	largest coord
	fontNow fontInfoEx
	calls   []string
	fail    string
	failed  bool
}

func (fake *fakeWindowsConsole) failure(operation string) error {
	if fake.fail == operation && !fake.failed {
		fake.failed = true
		return fmt.Errorf("injected %s failure", operation)
	}
	return nil
}

func (fake *fakeWindowsConsole) font(windows.Handle) (fontInfoEx, error) {
	return fake.fontNow, nil
}

func (fake *fakeWindowsConsole) setFont(_ windows.Handle, value fontInfoEx) error {
	fake.calls = append(fake.calls, "font")
	if err := fake.failure("font"); err != nil {
		return err
	}
	fake.fontNow = value
	return nil
}

func (fake *fakeWindowsConsole) bufferInfo(windows.Handle) (screenBufferInfo, error) {
	return fake.info, nil
}

func (fake *fakeWindowsConsole) largestWindow(windows.Handle) (coord, error) {
	return fake.largest, nil
}

func (fake *fakeWindowsConsole) setBuffer(_ windows.Handle, value coord) error {
	operation := fmt.Sprintf("buffer:%dx%d", value.X, value.Y)
	fake.calls = append(fake.calls, operation)
	if err := fake.failure(operation); err != nil {
		return err
	}
	return nil
}

func (fake *fakeWindowsConsole) setWindow(_ windows.Handle, value smallRect) error {
	operation := fmt.Sprintf("window:%dx%d", value.Right+1, value.Bottom+1)
	fake.calls = append(fake.calls, operation)
	if err := fake.failure(operation); err != nil {
		return err
	}
	return nil
}

func (fake *fakeWindowsConsole) setCursor(_ windows.Handle, value coord) error {
	operation := fmt.Sprintf("cursor:%d,%d", value.X, value.Y)
	fake.calls = append(fake.calls, operation)
	if err := fake.failure(operation); err != nil {
		return err
	}
	return nil
}

func TestApplyWindowsConsoleGrowsBufferBeforeWindow(t *testing.T) {
	fake := &fakeWindowsConsole{
		info:    screenBufferInfo{Size: coord{80, 25}, Window: smallRect{Right: 79, Bottom: 24}},
		largest: coord{200, 80},
	}
	err := applyWindowsConsole(fake, 1, Settings{Enabled: true, Columns: 132, Rows: 40, FontFace: "Consolas", FontSize: 18})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"font", "buffer:132x40", "window:132x40"}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls=%#v want=%#v", fake.calls, want)
	}
}

func TestApplyWindowsConsoleShrinksWindowAndCursorBeforeBuffer(t *testing.T) {
	fake := &fakeWindowsConsole{
		info: screenBufferInfo{
			Size: coord{160, 60}, CursorPosition: coord{150, 55},
			Window: smallRect{Right: 159, Bottom: 59},
		},
		largest: coord{200, 80},
	}
	err := applyWindowsConsole(fake, 1, Settings{Enabled: true, Columns: 100, Rows: 30, FontFace: "Consolas", FontSize: 16})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"font", "window:100x30", "cursor:99,29", "buffer:100x30", "window:100x30"}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls=%#v want=%#v", fake.calls, want)
	}
}

func TestApplyWindowsConsoleRejectsDisplayLimit(t *testing.T) {
	originalFont := fontInfoEx{FontSize: coord{Y: 14}}
	fake := &fakeWindowsConsole{
		info:    screenBufferInfo{Size: coord{80, 25}, Window: smallRect{Right: 79, Bottom: 24}},
		largest: coord{120, 35},
		fontNow: originalFont,
	}
	err := applyWindowsConsole(fake, 1, Settings{Enabled: true, Columns: 132, Rows: 40, FontFace: "Consolas", FontSize: 18})
	if err == nil {
		t.Fatal("display limit was ignored")
	}
	if fake.fontNow != originalFont {
		t.Fatal("font was not restored after display-limit rejection")
	}
}

func TestApplyWindowsConsoleRollsBackAfterResizeFailure(t *testing.T) {
	originalFont := fontInfoEx{FontSize: coord{Y: 14}}
	fake := &fakeWindowsConsole{
		info:    screenBufferInfo{Size: coord{80, 25}, CursorPosition: coord{12, 8}, Window: smallRect{Right: 79, Bottom: 24}},
		largest: coord{200, 80},
		fontNow: originalFont,
		fail:    "buffer:132x40",
	}
	err := applyWindowsConsole(fake, 1, Settings{Enabled: true, Columns: 132, Rows: 40, FontFace: "Consolas", FontSize: 18})
	if err == nil {
		t.Fatal("injected resize failure was ignored")
	}
	if fake.fontNow != originalFont {
		t.Fatal("font was not restored after resize failure")
	}
	wantTail := []string{"window:1x1", "cursor:0,0", "font", "buffer:80x25", "window:80x25", "cursor:12,8"}
	if len(fake.calls) < len(wantTail) || !reflect.DeepEqual(fake.calls[len(fake.calls)-len(wantTail):], wantTail) {
		t.Fatalf("rollback calls=%#v want tail=%#v", fake.calls, wantTail)
	}
}

func TestCheckedInt16RejectsWin32CoordinateOverflow(t *testing.T) {
	for _, value := range []int{-32769, 32768} {
		if _, err := checkedInt16(value, "test"); err == nil {
			t.Fatalf("checkedInt16(%d) accepted overflow", value)
		}
	}
	for _, value := range []int{-32768, 32767} {
		if converted, err := checkedInt16(value, "test"); err != nil || int(converted) != value {
			t.Fatalf("checkedInt16(%d)=%d err=%v", value, converted, err)
		}
	}
}
