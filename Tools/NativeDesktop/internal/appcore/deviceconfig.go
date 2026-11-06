package appcore

import "pccontroller.local/desktop/internal/deviceconfig"

// DeviceConfigSnapshot returns a read-only, explicitly allow-listed projection
// of the parsed diagnostics cache plus current transport capability flags.
// Raw diagnostic documents and credentials never enter the returned type.
func (c *Core) DeviceConfigSnapshot() deviceconfig.Snapshot {
	if c == nil {
		return deviceconfig.Snapshot{}
	}
	return deviceconfig.Project(
		c.DiagnosticsSnapshot(),
		c.TerminalStatus(),
		c.Snapshot().HTTPReachable,
	)
}
