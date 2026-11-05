// Package windx provides the small, pure-Go DirectX surface used by the
// native Windows renderers. It deliberately exposes no D3D or COM pointers:
// callers submit complete premultiplied RGBA frames and can always fall back
// to the legacy renderer if the capability probe or a presentation fails.
package windx

import (
	"errors"
	"fmt"
)

// Backend is a renderer selection stored by higher-level configuration.
type Backend string

const (
	BackendAuto    Backend = "auto"
	BackendDirectX Backend = "directx"
	BackendLegacy  Backend = "legacy"
)

func (backend Backend) String() string {
	return string(backend)
}

// Valid reports whether backend is a supported renderer selection.
func (backend Backend) Valid() bool {
	switch backend {
	case BackendAuto, BackendDirectX, BackendLegacy:
		return true
	default:
		return false
	}
}

// FeatureLevel is the D3D_FEATURE_LEVEL selected by D3D11CreateDevice.
type FeatureLevel uint32

const (
	FeatureLevel9_1  FeatureLevel = 0x9100
	FeatureLevel9_2  FeatureLevel = 0x9200
	FeatureLevel9_3  FeatureLevel = 0x9300
	FeatureLevel10_0 FeatureLevel = 0xa000
	FeatureLevel10_1 FeatureLevel = 0xa100
	FeatureLevel11_0 FeatureLevel = 0xb000
	FeatureLevel11_1 FeatureLevel = 0xb100
	FeatureLevel12_0 FeatureLevel = 0xc000
	FeatureLevel12_1 FeatureLevel = 0xc100
)

func (level FeatureLevel) String() string {
	switch level {
	case FeatureLevel9_1:
		return "9.1"
	case FeatureLevel9_2:
		return "9.2"
	case FeatureLevel9_3:
		return "9.3"
	case FeatureLevel10_0:
		return "10.0"
	case FeatureLevel10_1:
		return "10.1"
	case FeatureLevel11_0:
		return "11.0"
	case FeatureLevel11_1:
		return "11.1"
	case FeatureLevel12_0:
		return "12.0"
	case FeatureLevel12_1:
		return "12.1"
	case 0:
		return "unknown"
	default:
		return fmt.Sprintf("0x%04x", uint32(level))
	}
}

// Capability describes the adapter and complete presentation stack found by
// Probe. Available is true only when hardware D3D11, DXGI 1.2, and
// DirectComposition can all be initialized.
type Capability struct {
	Backend              Backend      `json:"backend"`
	Available            bool         `json:"available"`
	Hardware             bool         `json:"hardware"`
	DirectComposition    bool         `json:"direct_composition"`
	Adapter              string       `json:"adapter,omitempty"`
	FeatureLevel         FeatureLevel `json:"feature_level,omitempty"`
	VendorID             uint32       `json:"vendor_id,omitempty"`
	DeviceID             uint32       `json:"device_id,omitempty"`
	DedicatedVideoMemory uint64       `json:"dedicated_video_memory,omitempty"`
}

// Status is a point-in-time snapshot of a Presenter. HRESULT values are kept
// as unsigned hexadecimal-friendly values because DXGI/D3D HRESULTs are not
// ordinary Win32 error codes.
type Status struct {
	Backend             Backend `json:"backend"`
	Width               int     `json:"width"`
	Height              int     `json:"height"`
	Opacity             float64 `json:"opacity"`
	Presents            uint64  `json:"presents"`
	Closed              bool    `json:"closed"`
	DeviceLost          bool    `json:"device_lost"`
	LastHRESULT         uint32  `json:"last_hresult,omitempty"`
	DeviceRemovedReason uint32  `json:"device_removed_reason,omitempty"`
	LastFailedOperation string  `json:"last_failed_operation,omitempty"`
}

var (
	ErrUnavailable   = errors.New("windx: DirectX presentation is unavailable")
	ErrInvalidWindow = errors.New("windx: invalid window")
	ErrInvalidSize   = errors.New("windx: invalid surface size")
	ErrFrameSize     = errors.New("windx: frame dimensions do not match the presenter")
	ErrClosed        = errors.New("windx: presenter is closed")
	ErrDeviceLost    = errors.New("windx: graphics device was lost")
)

const (
	hresultInvalidArgument         = uint32(0x80070057)
	hresultDXGIDeviceRemoved       = uint32(0x887a0005)
	hresultDXGIDeviceHung          = uint32(0x887a0006)
	hresultDXGIDeviceReset         = uint32(0x887a0007)
	hresultDXGIDriverInternalError = uint32(0x887a0020)
)

// HRESULTError preserves the exact failing API and HRESULT. DeviceReason is
// populated from ID3D11Device::GetDeviceRemovedReason when it supplies more
// useful information than the original operation.
type HRESULTError struct {
	Operation    string
	Code         uint32
	DeviceReason uint32
}

func (failure *HRESULTError) Error() string {
	if failure == nil {
		return "<nil>"
	}
	if failure.DeviceReason != 0 && failure.DeviceReason != failure.Code {
		return fmt.Sprintf(
			"%s failed with HRESULT 0x%08X (device reason 0x%08X)",
			failure.Operation,
			failure.Code,
			failure.DeviceReason,
		)
	}
	return fmt.Sprintf(
		"%s failed with HRESULT 0x%08X",
		failure.Operation,
		failure.Code,
	)
}

func (failure *HRESULTError) Unwrap() error {
	if failure != nil &&
		(isDeviceLossCode(failure.Code) ||
			isDeviceLossCode(failure.DeviceReason)) {
		return ErrDeviceLost
	}
	return nil
}

// IsDeviceLost reports whether err represents a removed, reset, hung, or
// internally failed DXGI device.
func IsDeviceLost(err error) bool {
	return errors.Is(err, ErrDeviceLost)
}

func isDeviceLossCode(code uint32) bool {
	switch code {
	case hresultDXGIDeviceRemoved,
		hresultDXGIDeviceHung,
		hresultDXGIDeviceReset,
		hresultDXGIDriverInternalError:
		return true
	default:
		return false
	}
}
