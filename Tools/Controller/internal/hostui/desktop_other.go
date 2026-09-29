//go:build !windows && !linux

package hostui

func ensurePlatformDesktopIntegration(
	DesktopIntegrationOptions,
) (DesktopIntegrationStatus, error) {
	return DesktopIntegrationStatus{
		Supported: false, LastError: ErrUnsupported.Error(),
	}, ErrUnsupported
}

func removePlatformDesktopIntegration(
	DesktopIntegrationOptions,
) (DesktopIntegrationCleanupStatus, error) {
	return DesktopIntegrationCleanupStatus{
		Supported: false, LastError: ErrUnsupported.Error(),
	}, ErrUnsupported
}
