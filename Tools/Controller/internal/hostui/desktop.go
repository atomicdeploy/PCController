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

// DesktopIntegrationCleanupStatus reports only artifacts that were positively
// identified as belonging to this executable. A false Removed field can mean
// either that the artifact was already absent or that it was deliberately
// preserved because ownership could not be established; Skipped distinguishes
// the latter case.
type DesktopIntegrationCleanupStatus struct {
	Supported          bool     `json:"supported"`
	ProtocolRemoved    bool     `json:"protocol_removed"`
	AppIdentityRemoved bool     `json:"app_identity_removed"`
	ShortcutRemoved    bool     `json:"shortcut_removed"`
	Shortcut           string   `json:"shortcut,omitempty"`
	Skipped            []string `json:"skipped,omitempty"`
	LastError          string   `json:"last_error,omitempty"`
}

func EnsureDesktopIntegration(options DesktopIntegrationOptions) (DesktopIntegrationStatus, error) {
	return ensurePlatformDesktopIntegration(options)
}

// RemoveDesktopIntegration removes the current executable's per-user desktop
// integration. It is intentionally conservative and idempotent: registrations
// or shortcuts owned by another executable are left untouched.
func RemoveDesktopIntegration(
	options DesktopIntegrationOptions,
) (DesktopIntegrationCleanupStatus, error) {
	return removePlatformDesktopIntegration(options)
}
