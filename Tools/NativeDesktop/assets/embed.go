package assets

import "embed"

// Files is the one canonical runtime asset bundle shared by the native UI,
// local Web host, static Web exporter, and sound manager. Source masters and
// PE-only resources remain on disk without being embedded a second time.
//
//go:embed device-transition.apng device-on.svg device-off.svg
//go:embed icons/app.png icons/device-on.png icons/device-off.png icons/device-unknown.png icons/menu-*.png
//go:embed icons/device-on.ico icons/device-off.ico icons/device-unknown.ico icons/device-connecting.ico icons/device-error.ico
//go:embed icons/menu-settings.ico icons/menu-diagnostics.ico icons/menu-reconnect.ico
//go:embed fonts/Vazirmatn-Regular.ttf
//go:embed sfx/*.wav
var Files embed.FS
