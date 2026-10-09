package hostui

type ServiceStatus struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	Endpoint  string `json:"endpoint,omitempty"`
	Detail    string `json:"detail,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

type IntegrationStatus struct {
	Hotkeys       HotkeyStatus       `json:"hotkeys"`
	Keyboard      KeyboardStatus     `json:"keyboard_control"`
	Notifications NotificationStatus `json:"notifications"`
	Messaging     ServiceStatus      `json:"messaging"`
	Discovery     ServiceStatus      `json:"discovery"`
	Webhooks      ServiceStatus      `json:"webhooks"`
	SocketIO      ServiceStatus      `json:"socket_io"`
}
