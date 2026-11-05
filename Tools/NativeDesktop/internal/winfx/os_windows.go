//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	windows11FirstBuild       = 22000
	systemBackdropFirstBuild  = 22621
	acrylicAccentStableBuild  = 17134
	spiGetHighContrast        = 0x0042
	hcfHighContrastOn         = 0x00000001
	transparencyRegistryPath  = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	transparencyRegistryValue = "EnableTransparency"
	appsUseLightThemeValue    = "AppsUseLightTheme"
)

var (
	osDWMAPI                    = windows.NewLazySystemDLL("dwmapi.dll")
	osDwmIsCompositionEnabled   = osDWMAPI.NewProc("DwmIsCompositionEnabled")
	osUser32                    = windows.NewLazySystemDLL("user32.dll")
	osSystemParametersInfoW     = osUser32.NewProc("SystemParametersInfoW")
	osSetCompositionAttribute   = osUser32.NewProc("SetWindowCompositionAttribute")
	osDwmEnableBlurBehindWindow = osDWMAPI.NewProc("DwmEnableBlurBehindWindow")
	osDwmSetWindowAttribute     = osDWMAPI.NewProc("DwmSetWindowAttribute")
)

// VersionInfo is the real Windows NT version, independent of application
// manifest version-lie behavior.
type VersionInfo struct {
	Major            uint32 `json:"major"`
	Minor            uint32 `json:"minor"`
	Build            uint32 `json:"build"`
	ServicePackMajor uint16 `json:"service_pack_major,omitempty"`
	ServicePackMinor uint16 `json:"service_pack_minor,omitempty"`
	ProductType      byte   `json:"product_type,omitempty"`
}

func (v VersionInfo) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Build)
}

// IsWindows11 reports whether the NT build belongs to Windows 11. Windows 10
// and 11 both report NT major version 10, so ProductName registry text is not
// sufficient.
func (v VersionInfo) IsWindows11() bool {
	return v.Major >= 10 && v.Build >= windows11FirstBuild
}

// CurrentVersion returns the actual OS version using ntdll.RtlGetVersion.
func CurrentVersion() VersionInfo {
	info := windows.RtlGetVersion()
	return VersionInfo{
		Major:            info.MajorVersion,
		Minor:            info.MinorVersion,
		Build:            info.BuildNumber,
		ServicePackMajor: info.ServicePackMajor,
		ServicePackMinor: info.ServicePackMinor,
		ProductType:      info.ProductType,
	}
}

// OSInfo captures the runtime policy and API capabilities that affect visual
// effects. Capability booleans mean "worth attempting"; Apply still checks the
// actual API result and falls back safely.
type OSInfo struct {
	Version                   VersionInfo `json:"version"`
	Windows11                 bool        `json:"windows_11"`
	DWMComposition            bool        `json:"dwm_composition"`
	HighContrast              bool        `json:"high_contrast"`
	TransparencyEnabled       bool        `json:"transparency_enabled"`
	SupportsSystemBackdrop    bool        `json:"supports_system_backdrop"`
	SupportsAccentPolicy      bool        `json:"supports_accent_policy"`
	SupportsAcrylicAccent     bool        `json:"supports_acrylic_accent"`
	SupportsDWMBlurBehind     bool        `json:"supports_dwm_blur_behind"`
	SupportsRoundedCorners    bool        `json:"supports_rounded_corners"`
	SupportsImmersiveDarkMode bool        `json:"supports_immersive_dark_mode"`
}

type highContrastW struct {
	Size          uint32
	Flags         uint32
	DefaultScheme *uint16
}

// GetOSInfo probes Windows visual capabilities and the user's accessibility
// policy. It returns all successfully collected fields even when one probe
// fails; callers can expose the joined error in diagnostics.
func GetOSInfo() (OSInfo, error) {
	version := CurrentVersion()
	info := OSInfo{
		Version:                   version,
		Windows11:                 version.IsWindows11(),
		TransparencyEnabled:       true,
		SupportsRoundedCorners:    version.IsWindows11(),
		SupportsImmersiveDarkMode: version.Major >= 10 && version.Build >= 17763,
	}

	var probeErrors []error

	composition, err := dwmCompositionEnabled()
	if err != nil {
		probeErrors = append(probeErrors, fmt.Errorf("query DWM composition: %w", err))
		// DWM composition cannot be disabled on supported Windows 10/11
		// systems, so capability probing can remain optimistic.
		composition = version.Major >= 10
	}
	info.DWMComposition = composition

	highContrast, err := highContrastEnabled()
	if err != nil {
		probeErrors = append(probeErrors, fmt.Errorf("query high contrast: %w", err))
	}
	info.HighContrast = highContrast

	transparency, err := transparencyEnabled()
	if err != nil {
		probeErrors = append(probeErrors, fmt.Errorf("query transparency preference: %w", err))
	}
	info.TransparencyEnabled = transparency

	info.SupportsSystemBackdrop =
		version.Major >= 10 &&
			version.Build >= systemBackdropFirstBuild &&
			osDwmSetWindowAttribute.Find() == nil
	info.SupportsAccentPolicy =
		version.Major >= 10 &&
			osSetCompositionAttribute.Find() == nil
	info.SupportsAcrylicAccent =
		info.SupportsAccentPolicy &&
			version.Build >= acrylicAccentStableBuild
	info.SupportsDWMBlurBehind =
		osDwmEnableBlurBehindWindow.Find() == nil

	return info, errors.Join(probeErrors...)
}

func dwmCompositionEnabled() (bool, error) {
	if err := osDwmIsCompositionEnabled.Find(); err != nil {
		return false, err
	}
	var enabled int32
	result, _, _ := osDwmIsCompositionEnabled.Call(uintptr(unsafe.Pointer(&enabled)))
	if err := hresultError("DwmIsCompositionEnabled", result); err != nil {
		return false, err
	}
	return enabled != 0, nil
}

func highContrastEnabled() (bool, error) {
	if err := osSystemParametersInfoW.Find(); err != nil {
		return false, err
	}
	value := highContrastW{Size: uint32(unsafe.Sizeof(highContrastW{}))}
	ok, _, callErr := osSystemParametersInfoW.Call(
		spiGetHighContrast,
		uintptr(value.Size),
		uintptr(unsafe.Pointer(&value)),
		0,
	)
	if ok == 0 {
		return false, boolCallError("SystemParametersInfoW(SPI_GETHIGHCONTRAST)", callErr)
	}
	return value.Flags&hcfHighContrastOn != 0, nil
}

func transparencyEnabled() (bool, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		transparencyRegistryPath,
		registry.QUERY_VALUE,
	)
	if errors.Is(err, registry.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue(transparencyRegistryValue)
	if errors.Is(err, registry.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return value != 0, nil
}

// HighContrast reports the current SPI_GETHIGHCONTRAST accessibility state.
// It never modifies the user's system settings.
func HighContrast() (bool, error) {
	return highContrastEnabled()
}

// SystemDarkMode reports the user's application color-mode preference. A
// missing AppsUseLightTheme value means the historical light-mode default.
func SystemDarkMode() (bool, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		transparencyRegistryPath,
		registry.QUERY_VALUE,
	)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue(appsUseLightThemeValue)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == 0, nil
}

// TransparencyEnabled reports the user's Windows transparency preference.
func TransparencyEnabled() (bool, error) {
	return transparencyEnabled()
}

type hresultFailure struct {
	API  string
	Code int32
}

func (err hresultFailure) Error() string {
	return fmt.Sprintf("%s failed with HRESULT 0x%08X", err.API, uint32(err.Code))
}

func hresultError(api string, result uintptr) error {
	code := int32(uint32(result))
	if code < 0 {
		return hresultFailure{API: api, Code: code}
	}
	return nil
}

func boolCallError(api string, callErr error) error {
	if callErr == nil || errors.Is(callErr, windows.ERROR_SUCCESS) {
		return fmt.Errorf("%s returned FALSE", api)
	}
	return fmt.Errorf("%s: %w", api, callErr)
}
