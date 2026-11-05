//go:build windows

package ui

import (
	"os"
	"strings"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

func TestProductIconUsesVerifiedApplicationResource(t *testing.T) {
	if productIconResourceID != 32512 {
		t.Fatalf(
			"product icon resource = %d, want verified IDI_APPLICATION 32512",
			productIconResourceID,
		)
	}

	const (
		module = win.HINSTANCE(0x1234)
		handle = win.HANDLE(0x5678)
	)
	moduleCalls := 0
	loadCalls := 0
	icon, err := loadOwnedProductIcon(
		20,
		24,
		func() win.HINSTANCE {
			moduleCalls++
			return module
		},
		func(
			gotModule win.HINSTANCE,
			name *uint16,
			imageType uint32,
			width int32,
			height int32,
			flags uint32,
		) win.HANDLE {
			loadCalls++
			if gotModule != module {
				t.Errorf("module = %#x, want %#x", gotModule, module)
			}
			if got := uintptr(unsafe.Pointer(name)); got != productIconResourceID {
				t.Errorf(
					"resource = %d, want %d",
					got,
					productIconResourceID,
				)
			}
			if imageType != win.IMAGE_ICON {
				t.Errorf("image type = %d, want IMAGE_ICON", imageType)
			}
			if width != 20 || height != 24 {
				t.Errorf("size = %dx%d, want 20x24", width, height)
			}
			if flags&win.LR_SHARED != 0 {
				t.Errorf("owned icon unexpectedly uses LR_SHARED: %#x", flags)
			}
			return handle
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if icon != win.HICON(handle) {
		t.Fatalf("icon = %#x, want %#x", icon, handle)
	}
	if moduleCalls != 1 || loadCalls != 1 {
		t.Fatalf(
			"loader calls = module:%d image:%d, want 1 and 1",
			moduleCalls,
			loadCalls,
		)
	}
}

func TestProductIconLoaderRejectsInvalidOrUnavailableInputs(t *testing.T) {
	loadCalls := 0
	load := func(
		win.HINSTANCE,
		*uint16,
		uint32,
		int32,
		int32,
		uint32,
	) win.HANDLE {
		loadCalls++
		return 0
	}
	if _, err := loadOwnedProductIcon(
		0,
		16,
		func() win.HINSTANCE { return 1 },
		load,
	); err == nil {
		t.Fatal("invalid icon dimensions were accepted")
	}
	if loadCalls != 0 {
		t.Fatal("invalid dimensions reached LoadImage")
	}
	if _, err := loadOwnedProductIcon(
		16,
		16,
		func() win.HINSTANCE { return 0 },
		load,
	); err == nil {
		t.Fatal("missing module was accepted")
	}
	if loadCalls != 0 {
		t.Fatal("missing module reached LoadImage")
	}
	if _, err := loadOwnedProductIcon(
		16,
		16,
		func() win.HINSTANCE { return 1 },
		load,
	); err == nil {
		t.Fatal("failed LoadImage was accepted")
	}
	if loadCalls != 1 {
		t.Fatalf("failed LoadImage calls = %d, want 1", loadCalls)
	}
}

func TestRuntimeProductIconDoesNotReadEmbeddedICOCopy(t *testing.T) {
	for _, name := range []string{
		"shell_windows.go",
		"taskbar_windows.go",
	} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(source), "RayanLamp.ico") {
			t.Fatalf("%s still reads the duplicate raw product ICO", name)
		}
	}

	shell, err := os.ReadFile("shell_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shell), "newProductIconImage()") {
		t.Fatal("main window no longer loads its DPI-aware PE product icon")
	}
	taskbar, err := os.ReadFile("taskbar_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(taskbar),
		"newProductIconHandle(width, height)",
	) {
		t.Fatal("taskbar thumbnail no longer loads the PE icon at exact DPI size")
	}
}
