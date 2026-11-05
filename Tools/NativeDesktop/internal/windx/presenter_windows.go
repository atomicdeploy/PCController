//go:build windows

package windx

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	d3dDriverTypeHardware        = uint32(1)
	d3d11CreateDeviceBGRASupport = uint32(0x20)
	d3d11SDKVersion              = uint32(7)

	d3d11UsageDefault = uint32(0)

	dxgiFormatB8G8R8A8UNorm  = uint32(87)
	dxgiUsageRenderTarget    = uint32(0x20)
	dxgiScalingStretch       = uint32(0)
	dxgiSwapEffectFlipSeq    = uint32(3)
	dxgiAlphaPremultiplied   = uint32(1)
	dxgiCompositionBuffers   = uint32(2)
	presentationSyncInterval = uintptr(1)

	// IUnknown
	comQueryInterfaceSlot = 0
	comReleaseSlot        = 2

	// IDXGIObject and descendants, from dxgi.h / dxgi1_2.h.
	dxgiObjectGetParentSlot                = 6
	dxgiDeviceGetAdapterSlot               = 7
	dxgiAdapterGetDescSlot                 = 8
	dxgiFactory2CreateCompositionChainSlot = 24
	dxgiSwapChainPresentSlot               = 8
	dxgiSwapChainGetBufferSlot             = 9
	dxgiSwapChainResizeBuffersSlot         = 13

	// ID3D11Device, from d3d11.h.
	d3d11DeviceCreateTexture2DSlot  = 5
	d3d11DeviceGetRemovedReasonSlot = 39

	// ID3D11DeviceContext inherits seven ID3D11DeviceChild/IUnknown methods.
	d3d11ContextCopyResourceSlot      = 47
	d3d11ContextUpdateSubresourceSlot = 48

	// IDCompositionDevice, IDCompositionTarget, and IDCompositionVisual, from
	// dcomp.h. Overloaded C++ methods occupy distinct vtable entries.
	dcompDeviceCommitSlot       = 3
	dcompDeviceCreateTargetSlot = 6
	dcompDeviceCreateVisualSlot = 7
	dcompTargetSetRootSlot      = 3
	dcompVisualSetContentSlot   = 15
)

var (
	d3d11DLL          = windows.NewLazySystemDLL("d3d11.dll")
	d3d11CreateDevice = d3d11DLL.NewProc("D3D11CreateDevice")
	dcompDLL          = windows.NewLazySystemDLL("dcomp.dll")
	dcompCreateDevice = dcompDLL.NewProc("DCompositionCreateDevice")
	windxUser32       = windows.NewLazySystemDLL("user32.dll")
	windxIsWindow     = windxUser32.NewProc("IsWindow")
)

var (
	iidIDXGIDevice = windows.GUID{
		Data1: 0x54ec77fa,
		Data2: 0x1377,
		Data3: 0x44e6,
		Data4: [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c},
	}
	iidIDXGIFactory2 = windows.GUID{
		Data1: 0x50c83a1c,
		Data2: 0xe072,
		Data3: 0x4c48,
		Data4: [8]byte{0x87, 0xb0, 0x36, 0x30, 0xfa, 0x36, 0xa6, 0xd0},
	}
	iidID3D11Texture2D = windows.GUID{
		Data1: 0x6f15aaf2,
		Data2: 0xd208,
		Data3: 0x4e89,
		Data4: [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c},
	}
	iidIDCompositionDevice = windows.GUID{
		Data1: 0xc37ea93a,
		Data2: 0xe7aa,
		Data3: 0x450d,
		Data4: [8]byte{0xb1, 0x6f, 0x97, 0x46, 0xcb, 0x04, 0x07, 0xf3},
	}
)

type dxgiSampleDesc struct {
	Count   uint32
	Quality uint32
}

type dxgiSwapChainDesc1 struct {
	Width       uint32
	Height      uint32
	Format      uint32
	Stereo      int32
	SampleDesc  dxgiSampleDesc
	BufferUsage uint32
	BufferCount uint32
	Scaling     uint32
	SwapEffect  uint32
	AlphaMode   uint32
	Flags       uint32
}

type dxgiAdapterDesc struct {
	Description           [128]uint16
	VendorID              uint32
	DeviceID              uint32
	SubSysID              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	AdapterLUID           struct {
		LowPart  uint32
		HighPart int32
	}
}

type d3d11Texture2DDesc struct {
	Width          uint32
	Height         uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleDesc     dxgiSampleDesc
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

type directXStack struct {
	device      uintptr
	context     uintptr
	dxgiDevice  uintptr
	adapter     uintptr
	factory2    uintptr
	dcompDevice uintptr
}

func (stack *directXStack) close() {
	if stack == nil {
		return
	}
	for _, object := range []*uintptr{
		&stack.dcompDevice,
		&stack.factory2,
		&stack.adapter,
		&stack.dxgiDevice,
		&stack.context,
		&stack.device,
	} {
		releaseCOM(*object)
		*object = 0
	}
}

// Presenter owns a hardware D3D11 device, a premultiplied flip-model
// composition swap chain, and the DirectComposition visual attached to one
// HWND. Methods are serialized because the D3D11 immediate context is shared.
//
// The caller owns the HWND style and lifetime. A composition-only child
// normally uses WS_EX_NOREDIRECTIONBITMAP; this package intentionally does not
// mutate window styles that also control DWM backdrop and input behavior.
type Presenter struct {
	mu sync.Mutex

	hwnd       uintptr
	width      int
	height     int
	capability Capability
	status     Status
	opacity    byte
	hasFrame   bool

	stack         *directXStack
	swapChain     uintptr
	uploadTexture uintptr
	target        uintptr
	visual        uintptr

	// source retains the last unmodified premultiplied BGRA frame so opacity
	// can change without asking the UI to repaint. staging is the exact byte
	// range snapped by UpdateSubresource.
	source  []byte
	staging []byte
}

// Probe verifies the complete hardware path without creating a window or
// retaining native resources.
func Probe() (Capability, error) {
	stack, capability, err := createDirectXStack()
	if stack != nil {
		stack.close()
	}
	return capability, err
}

// NewPresenter creates a persistent hardware presenter for hwnd. Width and
// height are physical pixels and must be non-zero.
func NewPresenter(
	hwnd uintptr,
	width, height int,
) (*Presenter, error) {
	if hwnd == 0 || !isNativeWindow(hwnd) {
		return nil, ErrInvalidWindow
	}
	if _, err := frameByteCount(width, height); err != nil {
		return nil, err
	}

	stack, capability, err := createDirectXStack()
	if err != nil {
		return nil, err
	}
	presenter := &Presenter{
		hwnd:       hwnd,
		width:      width,
		height:     height,
		capability: capability,
		status: Status{
			Backend: BackendDirectX,
			Width:   width,
			Height:  height,
			Opacity: 1,
		},
		opacity: 255,
		stack:   stack,
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = presenter.Close()
		}
	}()

	presenter.swapChain, err = createCompositionSwapChain(
		stack.factory2,
		stack.device,
		width,
		height,
	)
	if err != nil {
		return nil, err
	}
	presenter.uploadTexture, err = createUploadTexture(
		stack.device,
		width,
		height,
	)
	if err != nil {
		return nil, err
	}
	presenter.source, err = resizeFrameBuffer(nil, width, height)
	if err != nil {
		return nil, err
	}
	presenter.staging, err = resizeFrameBuffer(nil, width, height)
	if err != nil {
		return nil, err
	}

	// Seed every swap-chain byte before attaching it to a visible visual.
	// Composition therefore never observes driver-owned uninitialized pixels.
	presenter.hasFrame = true
	if err := presenter.uploadAndPresentLocked(0); err != nil {
		return nil, err
	}
	if err := presenter.createVisualTree(); err != nil {
		return nil, err
	}
	if err := presenter.commitLocked("IDCompositionDevice::Commit"); err != nil {
		return nil, err
	}

	cleanup = false
	return presenter, nil
}

// Present uploads one complete premultiplied RGBA frame. The source can be
// changed immediately after the method returns; UpdateSubresource has already
// snapped the retained BGRA staging bytes.
func (presenter *Presenter) Present(frame *image.RGBA) error {
	if presenter == nil {
		return ErrClosed
	}
	presenter.mu.Lock()
	defer presenter.mu.Unlock()
	if presenter.status.Closed {
		return ErrClosed
	}
	if err := convertPremultipliedRGBA(
		frame,
		presenter.source,
		presenter.width,
		presenter.height,
	); err != nil {
		return err
	}
	applyPremultipliedOpacity(
		presenter.source,
		presenter.staging,
		presenter.opacity,
	)
	presenter.hasFrame = true
	return presenter.uploadAndPresentLocked(presentationSyncInterval)
}

// Resize recreates size-dependent GPU resources. It commits one transparent
// initialized frame before returning so a subsequent render can never expose
// undefined swap-chain memory.
func (presenter *Presenter) Resize(width, height int) error {
	if presenter == nil {
		return ErrClosed
	}
	if _, err := frameByteCount(width, height); err != nil {
		return err
	}
	presenter.mu.Lock()
	defer presenter.mu.Unlock()
	if presenter.status.Closed {
		return ErrClosed
	}
	if presenter.width == width && presenter.height == height {
		return nil
	}

	nextTexture, err := createUploadTexture(
		presenter.stack.device,
		width,
		height,
	)
	if err != nil {
		presenter.captureHRESULTFailure(err)
		return err
	}
	result := comCall(
		presenter.swapChain,
		dxgiSwapChainResizeBuffersSlot,
		uintptr(dxgiCompositionBuffers),
		uintptr(uint32(width)),
		uintptr(uint32(height)),
		uintptr(dxgiFormatB8G8R8A8UNorm),
		0,
	)
	if hresultFailed(uint32(result)) {
		releaseCOM(nextTexture)
		return presenter.failureLocked(
			"IDXGISwapChain::ResizeBuffers",
			uint32(result),
		)
	}

	releaseCOM(presenter.uploadTexture)
	presenter.uploadTexture = nextTexture
	presenter.width = width
	presenter.height = height
	presenter.source, _ = resizeFrameBuffer(
		presenter.source,
		width,
		height,
	)
	presenter.staging, _ = resizeFrameBuffer(
		presenter.staging,
		width,
		height,
	)
	presenter.hasFrame = true
	presenter.status.Width = width
	presenter.status.Height = height
	if err := presenter.uploadAndPresentLocked(0); err != nil {
		return err
	}
	return presenter.commitLocked(
		"IDCompositionDevice::Commit(resize)",
	)
}

// SetOpacity applies a whole-surface opacity while preserving premultiplied
// color. It deliberately performs the multiplication in retained memory
// rather than invoking a floating-point COM vtable method: Go's architecture-
// neutral Windows syscall trampoline transports uintptr arguments, while
// DirectComposition's float ABI differs across amd64, 386, and arm64.
func (presenter *Presenter) SetOpacity(opacity float64) error {
	if presenter == nil {
		return ErrClosed
	}
	value, err := opacityByte(opacity)
	if err != nil {
		return err
	}
	presenter.mu.Lock()
	defer presenter.mu.Unlock()
	if presenter.status.Closed {
		return ErrClosed
	}
	if presenter.opacity == value {
		return nil
	}
	presenter.opacity = value
	presenter.status.Opacity = float64(value) / 255
	if !presenter.hasFrame {
		return nil
	}
	applyPremultipliedOpacity(
		presenter.source,
		presenter.staging,
		presenter.opacity,
	)
	return presenter.uploadAndPresentLocked(0)
}

func (presenter *Presenter) Capability() Capability {
	if presenter == nil {
		return Capability{Backend: BackendLegacy}
	}
	presenter.mu.Lock()
	defer presenter.mu.Unlock()
	return presenter.capability
}

func (presenter *Presenter) Status() Status {
	if presenter == nil {
		return Status{
			Backend: BackendLegacy,
			Closed:  true,
		}
	}
	presenter.mu.Lock()
	defer presenter.mu.Unlock()
	return presenter.status
}

// Close detaches the visual tree and releases every COM reference. It is safe
// to call repeatedly.
func (presenter *Presenter) Close() error {
	if presenter == nil {
		return nil
	}
	presenter.mu.Lock()
	defer presenter.mu.Unlock()
	if presenter.status.Closed {
		return nil
	}
	presenter.status.Closed = true

	var failures []error
	if presenter.visual != 0 {
		result := comCall(
			presenter.visual,
			dcompVisualSetContentSlot,
			0,
		)
		if hresultFailed(uint32(result)) {
			failures = append(failures, presenter.failureLocked(
				"IDCompositionVisual::SetContent(nil)",
				uint32(result),
			))
		}
	}
	if presenter.target != 0 {
		result := comCall(
			presenter.target,
			dcompTargetSetRootSlot,
			0,
		)
		if hresultFailed(uint32(result)) {
			failures = append(failures, presenter.failureLocked(
				"IDCompositionTarget::SetRoot(nil)",
				uint32(result),
			))
		}
	}
	if presenter.stack != nil && presenter.stack.dcompDevice != 0 {
		if err := presenter.commitLocked(
			"IDCompositionDevice::Commit(close)",
		); err != nil {
			failures = append(failures, err)
		}
	}

	for _, object := range []*uintptr{
		&presenter.visual,
		&presenter.target,
		&presenter.uploadTexture,
		&presenter.swapChain,
	} {
		releaseCOM(*object)
		*object = 0
	}
	if presenter.stack != nil {
		presenter.stack.close()
		presenter.stack = nil
	}
	presenter.source = nil
	presenter.staging = nil
	presenter.hasFrame = false
	presenter.hwnd = 0
	return errors.Join(failures...)
}

func (presenter *Presenter) createVisualTree() error {
	var target uintptr
	result := comCall(
		presenter.stack.dcompDevice,
		dcompDeviceCreateTargetSlot,
		presenter.hwnd,
		1,
		uintptr(unsafe.Pointer(&target)),
	)
	runtime.KeepAlive(&target)
	if hresultFailed(uint32(result)) {
		return presenter.failureLocked(
			"IDCompositionDevice::CreateTargetForHwnd",
			uint32(result),
		)
	}
	presenter.target = target

	var visual uintptr
	result = comCall(
		presenter.stack.dcompDevice,
		dcompDeviceCreateVisualSlot,
		uintptr(unsafe.Pointer(&visual)),
	)
	runtime.KeepAlive(&visual)
	if hresultFailed(uint32(result)) {
		return presenter.failureLocked(
			"IDCompositionDevice::CreateVisual",
			uint32(result),
		)
	}
	presenter.visual = visual

	result = comCall(
		presenter.visual,
		dcompVisualSetContentSlot,
		presenter.swapChain,
	)
	if hresultFailed(uint32(result)) {
		return presenter.failureLocked(
			"IDCompositionVisual::SetContent",
			uint32(result),
		)
	}
	result = comCall(
		presenter.target,
		dcompTargetSetRootSlot,
		presenter.visual,
	)
	if hresultFailed(uint32(result)) {
		return presenter.failureLocked(
			"IDCompositionTarget::SetRoot",
			uint32(result),
		)
	}
	return nil
}

func (presenter *Presenter) uploadAndPresentLocked(
	syncInterval uintptr,
) error {
	if presenter.stack == nil || presenter.stack.context == 0 ||
		presenter.uploadTexture == 0 || presenter.swapChain == 0 ||
		len(presenter.staging) == 0 {
		return ErrClosed
	}

	comCall(
		presenter.stack.context,
		d3d11ContextUpdateSubresourceSlot,
		presenter.uploadTexture,
		0,
		0,
		uintptr(unsafe.Pointer(&presenter.staging[0])),
		uintptr(uint32(presenter.width*4)),
		uintptr(uint32(presenter.width*presenter.height*4)),
	)

	var backBuffer uintptr
	result := comCall(
		presenter.swapChain,
		dxgiSwapChainGetBufferSlot,
		0,
		uintptr(unsafe.Pointer(&iidID3D11Texture2D)),
		uintptr(unsafe.Pointer(&backBuffer)),
	)
	runtime.KeepAlive(&iidID3D11Texture2D)
	runtime.KeepAlive(&backBuffer)
	if hresultFailed(uint32(result)) {
		return presenter.failureLocked(
			"IDXGISwapChain::GetBuffer",
			uint32(result),
		)
	}
	comCall(
		presenter.stack.context,
		d3d11ContextCopyResourceSlot,
		backBuffer,
		presenter.uploadTexture,
	)
	releaseCOM(backBuffer)

	result = comCall(
		presenter.swapChain,
		dxgiSwapChainPresentSlot,
		syncInterval,
		0,
	)
	runtime.KeepAlive(presenter.staging)
	presenter.status.LastHRESULT = uint32(result)
	if hresultFailed(uint32(result)) {
		return presenter.failureLocked(
			"IDXGISwapChain::Present",
			uint32(result),
		)
	}
	presenter.status.Presents++
	presenter.status.LastFailedOperation = ""
	return nil
}

func (presenter *Presenter) commitLocked(operation string) error {
	result := comCall(
		presenter.stack.dcompDevice,
		dcompDeviceCommitSlot,
	)
	presenter.status.LastHRESULT = uint32(result)
	if hresultFailed(uint32(result)) {
		return presenter.failureLocked(operation, uint32(result))
	}
	return nil
}

func (presenter *Presenter) failureLocked(
	operation string,
	code uint32,
) error {
	reason := uint32(0)
	if presenter.stack != nil && presenter.stack.device != 0 {
		reason = uint32(comCall(
			presenter.stack.device,
			d3d11DeviceGetRemovedReasonSlot,
		))
	}
	presenter.status.LastHRESULT = code
	presenter.status.DeviceRemovedReason = reason
	presenter.status.LastFailedOperation = operation
	presenter.status.DeviceLost =
		isDeviceLossCode(code) || isDeviceLossCode(reason)
	return &HRESULTError{
		Operation:    operation,
		Code:         code,
		DeviceReason: reason,
	}
}

func (presenter *Presenter) captureHRESULTFailure(err error) {
	var failure *HRESULTError
	if errors.As(err, &failure) {
		_ = presenter.failureLocked(
			failure.Operation,
			failure.Code,
		)
	}
}

func createDirectXStack() (
	stack *directXStack,
	capability Capability,
	err error,
) {
	capability.Backend = BackendDirectX
	if err := findDirectXAPIs(); err != nil {
		return nil, capability, err
	}
	stack = new(directXStack)
	defer func() {
		if err != nil {
			stack.close()
			stack = nil
		}
	}()

	stack.device, stack.context, capability.FeatureLevel, err =
		createHardwareD3D11Device()
	if err != nil {
		return stack, capability, fmt.Errorf(
			"%w: %w",
			ErrUnavailable,
			err,
		)
	}
	capability.Hardware = true

	stack.dxgiDevice, err = queryInterface(
		stack.device,
		&iidIDXGIDevice,
	)
	if err != nil {
		return stack, capability, fmt.Errorf(
			"%w: obtain IDXGIDevice: %w",
			ErrUnavailable,
			err,
		)
	}

	result := comCall(
		stack.dxgiDevice,
		dxgiDeviceGetAdapterSlot,
		uintptr(unsafe.Pointer(&stack.adapter)),
	)
	runtime.KeepAlive(&stack.adapter)
	if hresultFailed(uint32(result)) {
		return stack, capability, fmt.Errorf(
			"%w: %w",
			ErrUnavailable,
			newHRESULTError(
				"IDXGIDevice::GetAdapter",
				uint32(result),
			),
		)
	}

	var description dxgiAdapterDesc
	result = comCall(
		stack.adapter,
		dxgiAdapterGetDescSlot,
		uintptr(unsafe.Pointer(&description)),
	)
	runtime.KeepAlive(&description)
	if hresultFailed(uint32(result)) {
		return stack, capability, fmt.Errorf(
			"%w: %w",
			ErrUnavailable,
			newHRESULTError(
				"IDXGIAdapter::GetDesc",
				uint32(result),
			),
		)
	}
	capability.Adapter = windows.UTF16ToString(
		description.Description[:],
	)
	capability.VendorID = description.VendorID
	capability.DeviceID = description.DeviceID
	capability.DedicatedVideoMemory = uint64(
		description.DedicatedVideoMemory,
	)

	result = comCall(
		stack.adapter,
		dxgiObjectGetParentSlot,
		uintptr(unsafe.Pointer(&iidIDXGIFactory2)),
		uintptr(unsafe.Pointer(&stack.factory2)),
	)
	runtime.KeepAlive(&iidIDXGIFactory2)
	runtime.KeepAlive(&stack.factory2)
	if hresultFailed(uint32(result)) {
		return stack, capability, fmt.Errorf(
			"%w: %w",
			ErrUnavailable,
			newHRESULTError(
				"IDXGIAdapter::GetParent(IDXGIFactory2)",
				uint32(result),
			),
		)
	}

	result, _, _ = dcompCreateDevice.Call(
		stack.dxgiDevice,
		uintptr(unsafe.Pointer(&iidIDCompositionDevice)),
		uintptr(unsafe.Pointer(&stack.dcompDevice)),
	)
	runtime.KeepAlive(&iidIDCompositionDevice)
	runtime.KeepAlive(&stack.dcompDevice)
	if hresultFailed(uint32(result)) {
		return stack, capability, fmt.Errorf(
			"%w: %w",
			ErrUnavailable,
			newHRESULTError(
				"DCompositionCreateDevice",
				uint32(result),
			),
		)
	}

	capability.DirectComposition = true
	capability.Available = true
	return stack, capability, nil
}

func createHardwareD3D11Device() (
	device uintptr,
	context uintptr,
	selected FeatureLevel,
	err error,
) {
	// New feature levels are rejected with E_INVALIDARG by older runtimes.
	// Retrying the official descending lists keeps Windows 7 Platform Update,
	// Windows 8, and current Windows on their highest supported level.
	attempts := [][]FeatureLevel{
		{
			FeatureLevel12_1,
			FeatureLevel12_0,
			FeatureLevel11_1,
			FeatureLevel11_0,
			FeatureLevel10_1,
			FeatureLevel10_0,
			FeatureLevel9_3,
		},
		{
			FeatureLevel11_1,
			FeatureLevel11_0,
			FeatureLevel10_1,
			FeatureLevel10_0,
			FeatureLevel9_3,
		},
		{
			FeatureLevel11_0,
			FeatureLevel10_1,
			FeatureLevel10_0,
			FeatureLevel9_3,
		},
	}
	for attempt, levels := range attempts {
		var currentDevice, currentContext uintptr
		var currentLevel FeatureLevel
		result, _, _ := d3d11CreateDevice.Call(
			0,
			uintptr(d3dDriverTypeHardware),
			0,
			uintptr(d3d11CreateDeviceBGRASupport),
			uintptr(unsafe.Pointer(&levels[0])),
			uintptr(uint32(len(levels))),
			uintptr(d3d11SDKVersion),
			uintptr(unsafe.Pointer(&currentDevice)),
			uintptr(unsafe.Pointer(&currentLevel)),
			uintptr(unsafe.Pointer(&currentContext)),
		)
		runtime.KeepAlive(levels)
		runtime.KeepAlive(&currentDevice)
		runtime.KeepAlive(&currentLevel)
		runtime.KeepAlive(&currentContext)
		code := uint32(result)
		if !hresultFailed(code) {
			if currentDevice == 0 || currentContext == 0 {
				releaseCOM(currentContext)
				releaseCOM(currentDevice)
				return 0, 0, 0, fmt.Errorf(
					"D3D11CreateDevice returned an empty device",
				)
			}
			return currentDevice, currentContext, currentLevel, nil
		}
		releaseCOM(currentContext)
		releaseCOM(currentDevice)
		if code != hresultInvalidArgument ||
			attempt == len(attempts)-1 {
			return 0, 0, 0, newHRESULTError(
				"D3D11CreateDevice(hardware)",
				code,
			)
		}
	}
	return 0, 0, 0, ErrUnavailable
}

func createCompositionSwapChain(
	factory2 uintptr,
	device uintptr,
	width, height int,
) (uintptr, error) {
	description := dxgiSwapChainDesc1{
		Width:       uint32(width),
		Height:      uint32(height),
		Format:      dxgiFormatB8G8R8A8UNorm,
		SampleDesc:  dxgiSampleDesc{Count: 1},
		BufferUsage: dxgiUsageRenderTarget,
		BufferCount: dxgiCompositionBuffers,
		Scaling:     dxgiScalingStretch,
		SwapEffect:  dxgiSwapEffectFlipSeq,
		AlphaMode:   dxgiAlphaPremultiplied,
	}
	var swapChain uintptr
	result := comCall(
		factory2,
		dxgiFactory2CreateCompositionChainSlot,
		device,
		uintptr(unsafe.Pointer(&description)),
		0,
		uintptr(unsafe.Pointer(&swapChain)),
	)
	runtime.KeepAlive(&description)
	runtime.KeepAlive(&swapChain)
	if hresultFailed(uint32(result)) {
		return 0, newHRESULTError(
			"IDXGIFactory2::CreateSwapChainForComposition",
			uint32(result),
		)
	}
	if swapChain == 0 {
		return 0, fmt.Errorf(
			"IDXGIFactory2::CreateSwapChainForComposition returned nil",
		)
	}
	return swapChain, nil
}

func createUploadTexture(
	device uintptr,
	width, height int,
) (uintptr, error) {
	description := d3d11Texture2DDesc{
		Width:      uint32(width),
		Height:     uint32(height),
		MipLevels:  1,
		ArraySize:  1,
		Format:     dxgiFormatB8G8R8A8UNorm,
		SampleDesc: dxgiSampleDesc{Count: 1},
		Usage:      d3d11UsageDefault,
	}
	var texture uintptr
	result := comCall(
		device,
		d3d11DeviceCreateTexture2DSlot,
		uintptr(unsafe.Pointer(&description)),
		0,
		uintptr(unsafe.Pointer(&texture)),
	)
	runtime.KeepAlive(&description)
	runtime.KeepAlive(&texture)
	if hresultFailed(uint32(result)) {
		return 0, newHRESULTError(
			"ID3D11Device::CreateTexture2D(upload)",
			uint32(result),
		)
	}
	if texture == 0 {
		return 0, fmt.Errorf(
			"ID3D11Device::CreateTexture2D returned nil",
		)
	}
	return texture, nil
}

func findDirectXAPIs() error {
	for _, item := range []struct {
		name string
		proc *windows.LazyProc
	}{
		{"D3D11CreateDevice", d3d11CreateDevice},
		{"DCompositionCreateDevice", dcompCreateDevice},
		{"IsWindow", windxIsWindow},
	} {
		if err := item.proc.Find(); err != nil {
			return fmt.Errorf(
				"%w: %s: %w",
				ErrUnavailable,
				item.name,
				err,
			)
		}
	}
	return nil
}

func isNativeWindow(hwnd uintptr) bool {
	if windxIsWindow.Find() != nil {
		return false
	}
	result, _, _ := windxIsWindow.Call(hwnd)
	return result != 0
}

func queryInterface(
	object uintptr,
	iid *windows.GUID,
) (uintptr, error) {
	if object == 0 || iid == nil {
		return 0, fmt.Errorf("windx: invalid QueryInterface input")
	}
	var resultObject uintptr
	result := comCall(
		object,
		comQueryInterfaceSlot,
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&resultObject)),
	)
	runtime.KeepAlive(iid)
	runtime.KeepAlive(&resultObject)
	if hresultFailed(uint32(result)) {
		return 0, newHRESULTError(
			"IUnknown::QueryInterface",
			uint32(result),
		)
	}
	if resultObject == 0 {
		return 0, fmt.Errorf(
			"IUnknown::QueryInterface returned nil",
		)
	}
	return resultObject, nil
}

func comCall(
	object uintptr,
	slot int,
	arguments ...uintptr,
) uintptr {
	const maximumCOMVtableSlots = 128
	type comObjectHeader struct {
		vtable *[maximumCOMVtableSlots]uintptr
	}
	if object == 0 || slot < 0 || slot >= maximumCOMVtableSlots {
		return uintptr(hresultInvalidArgument)
	}
	// Reinterpret the pointer-sized COM handle through its local storage. This
	// avoids pointer arithmetic over a uintptr (which is unsafe under a moving
	// Go heap and rejected by go vet) while retaining the native interface
	// layout: the first object word is the vtable pointer.
	native := *(**comObjectHeader)(unsafe.Pointer(&object))
	if native == nil || native.vtable == nil {
		return uintptr(hresultInvalidArgument)
	}
	method := native.vtable[slot]
	// Avoid allocating an argument slice on the presentation hot path.
	switch len(arguments) {
	case 0:
		return syscallResult(method, object)
	case 1:
		return syscallResult(method, object, arguments[0])
	case 2:
		return syscallResult(
			method,
			object,
			arguments[0],
			arguments[1],
		)
	case 3:
		return syscallResult(
			method,
			object,
			arguments[0],
			arguments[1],
			arguments[2],
		)
	case 4:
		return syscallResult(
			method,
			object,
			arguments[0],
			arguments[1],
			arguments[2],
			arguments[3],
		)
	case 5:
		return syscallResult(
			method,
			object,
			arguments[0],
			arguments[1],
			arguments[2],
			arguments[3],
			arguments[4],
		)
	case 6:
		return syscallResult(
			method,
			object,
			arguments[0],
			arguments[1],
			arguments[2],
			arguments[3],
			arguments[4],
			arguments[5],
		)
	}
	callArguments := make([]uintptr, len(arguments)+1)
	callArguments[0] = object
	copy(callArguments[1:], arguments)
	return syscallResult(method, callArguments...)
}

func syscallResult(trampoline uintptr, arguments ...uintptr) uintptr {
	result, _, _ := syscall.SyscallN(trampoline, arguments...)
	return result
}

func releaseCOM(object uintptr) {
	if object != 0 {
		comCall(object, comReleaseSlot)
	}
}

func hresultFailed(code uint32) bool {
	return int32(code) < 0
}

func newHRESULTError(
	operation string,
	code uint32,
) *HRESULTError {
	return &HRESULTError{
		Operation: operation,
		Code:      code,
	}
}
