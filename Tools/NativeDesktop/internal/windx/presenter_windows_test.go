//go:build windows

package windx

import (
	"image"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNativeStructureLayouts(t *testing.T) {
	t.Parallel()
	if size := unsafe.Sizeof(dxgiSwapChainDesc1{}); size != 48 {
		t.Fatalf("DXGI_SWAP_CHAIN_DESC1 size = %d, want 48", size)
	}
	if size := unsafe.Sizeof(d3d11Texture2DDesc{}); size != 44 {
		t.Fatalf("D3D11_TEXTURE2D_DESC size = %d, want 44", size)
	}
	wantAdapter := uintptr(292)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		wantAdapter = 304
	}
	if size := unsafe.Sizeof(dxgiAdapterDesc{}); size != wantAdapter {
		t.Fatalf(
			"DXGI_ADAPTER_DESC size = %d, want %d",
			size,
			wantAdapter,
		)
	}
}

func TestOfficialCOMVtableSlots(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]int{
		"IDXGIObject.GetParent":                       dxgiObjectGetParentSlot,
		"IDXGIDevice.GetAdapter":                      dxgiDeviceGetAdapterSlot,
		"IDXGIAdapter.GetDesc":                        dxgiAdapterGetDescSlot,
		"IDXGIFactory2.CreateSwapChainForComposition": dxgiFactory2CreateCompositionChainSlot,
		"IDXGISwapChain.Present":                      dxgiSwapChainPresentSlot,
		"IDXGISwapChain.GetBuffer":                    dxgiSwapChainGetBufferSlot,
		"IDXGISwapChain.ResizeBuffers":                dxgiSwapChainResizeBuffersSlot,
		"ID3D11Device.CreateTexture2D":                d3d11DeviceCreateTexture2DSlot,
		"ID3D11Device.GetDeviceRemovedReason":         d3d11DeviceGetRemovedReasonSlot,
		"ID3D11DeviceContext.CopyResource":            d3d11ContextCopyResourceSlot,
		"ID3D11DeviceContext.UpdateSubresource":       d3d11ContextUpdateSubresourceSlot,
		"IDCompositionDevice.Commit":                  dcompDeviceCommitSlot,
		"IDCompositionDevice.CreateTargetForHwnd":     dcompDeviceCreateTargetSlot,
		"IDCompositionDevice.CreateVisual":            dcompDeviceCreateVisualSlot,
		"IDCompositionTarget.SetRoot":                 dcompTargetSetRootSlot,
		"IDCompositionVisual.SetContent":              dcompVisualSetContentSlot,
	} {
		want := map[string]int{
			"IDXGIObject.GetParent":                       6,
			"IDXGIDevice.GetAdapter":                      7,
			"IDXGIAdapter.GetDesc":                        8,
			"IDXGIFactory2.CreateSwapChainForComposition": 24,
			"IDXGISwapChain.Present":                      8,
			"IDXGISwapChain.GetBuffer":                    9,
			"IDXGISwapChain.ResizeBuffers":                13,
			"ID3D11Device.CreateTexture2D":                5,
			"ID3D11Device.GetDeviceRemovedReason":         39,
			"ID3D11DeviceContext.CopyResource":            47,
			"ID3D11DeviceContext.UpdateSubresource":       48,
			"IDCompositionDevice.Commit":                  3,
			"IDCompositionDevice.CreateTargetForHwnd":     6,
			"IDCompositionDevice.CreateVisual":            7,
			"IDCompositionTarget.SetRoot":                 3,
			"IDCompositionVisual.SetContent":              15,
		}[name]
		if value != want {
			t.Fatalf("%s slot = %d, want %d", name, value, want)
		}
	}
}

func TestProbeHardwareDirectX(t *testing.T) {
	capability, err := Probe()
	if err != nil {
		t.Skipf("hardware DirectX stack unavailable: %v", err)
	}
	if !capability.Available ||
		!capability.Hardware ||
		!capability.DirectComposition {
		t.Fatalf("incomplete capability: %+v", capability)
	}
	if capability.Adapter == "" {
		t.Fatal("adapter name is empty")
	}
	if capability.FeatureLevel == 0 {
		t.Fatal("D3D feature level is empty")
	}
}

func TestPresenterIntegration(t *testing.T) {
	if os.Getenv("RAYANLAMP_DIRECTX_SMOKE") != "1" {
		t.Skip("set RAYANLAMP_DIRECTX_SMOKE=1 for the native presenter smoke test")
	}
	if _, err := Probe(); err != nil {
		t.Skipf("hardware DirectX stack unavailable: %v", err)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := createDirectXTestWindow(t, 64, 64)
	defer destroyDirectXTestWindow(hwnd)

	presenter, err := NewPresenter(hwnd, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer presenter.Close()

	frame := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for offset := 0; offset < len(frame.Pix); offset += 4 {
		frame.Pix[offset+0] = 64
		frame.Pix[offset+1] = 96
		frame.Pix[offset+2] = 128
		frame.Pix[offset+3] = 192
	}
	if err := presenter.Present(frame); err != nil {
		t.Fatal(err)
	}
	if err := presenter.SetOpacity(0.5); err != nil {
		t.Fatal(err)
	}
	if err := presenter.Resize(80, 48); err != nil {
		t.Fatal(err)
	}
	frame = image.NewRGBA(image.Rect(0, 0, 80, 48))
	if err := presenter.Present(frame); err != nil {
		t.Fatal(err)
	}
	status := presenter.Status()
	if status.DeviceLost || status.Width != 80 || status.Height != 48 ||
		status.Presents < 4 {
		t.Fatalf("unexpected presenter status: %+v", status)
	}
	if err := presenter.Close(); err != nil {
		t.Fatal(err)
	}
	if !presenter.Status().Closed {
		t.Fatal("presenter did not report closed")
	}
}

var (
	windxTestUser32        = windows.NewLazySystemDLL("user32.dll")
	windxTestCreateWindow  = windxTestUser32.NewProc("CreateWindowExW")
	windxTestDestroyWindow = windxTestUser32.NewProc("DestroyWindow")
)

func createDirectXTestWindow(
	t *testing.T,
	width, height int,
) uintptr {
	t.Helper()
	className, err := windows.UTF16PtrFromString("STATIC")
	if err != nil {
		t.Fatal(err)
	}
	title, err := windows.UTF16PtrFromString("")
	if err != nil {
		t.Fatal(err)
	}
	const (
		wsExNoRedirectionBitmap = uintptr(0x00200000)
		wsPopup                 = uintptr(0x80000000)
	)
	hwnd, _, callErr := windxTestCreateWindow.Call(
		wsExNoRedirectionBitmap,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup,
		0,
		0,
		uintptr(width),
		uintptr(height),
		0,
		0,
		0,
		0,
	)
	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		t.Fatalf("CreateWindowExW: %v", callErr)
	}
	return hwnd
}

func destroyDirectXTestWindow(hwnd uintptr) {
	if hwnd != 0 {
		_, _, _ = windxTestDestroyWindow.Call(hwnd)
	}
}
