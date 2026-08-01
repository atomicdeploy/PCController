package hostui

type DesktopIntegrationOptions struct {
	AppID       string
	DisplayName string
}

type DesktopIntegrationStatus struct {
	Supported     bool   `json:"supported"`
	ProtocolReady bool   `json:"protocol_ready"`
	ShortcutReady bool   `json:"shortcut_ready"`
	Executable    string `json:"executable,omitempty"`
	Shortcut      string `json:"shortcut,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

func EnsureDesktopIntegration(options DesktopIntegrationOptions) (DesktopIntegrationStatus, error) {
	return ensurePlatformDesktopIntegration(options)
}
