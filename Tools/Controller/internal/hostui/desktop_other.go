//go:build !windows

package hostui

func ensurePlatformDesktopIntegration(
	DesktopIntegrationOptions,
) (DesktopIntegrationStatus, error) {
	return DesktopIntegrationStatus{
		Supported: false, LastError: ErrUnsupported.Error(),
	}, ErrUnsupported
}
