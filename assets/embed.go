package assets

import "embed"

// Files is the one canonical runtime asset bundle shared by the native UI,
// local Web host, static Web exporter, and sound manager. Source masters and
// PE-only resources remain on disk without being embedded a second time.
//
//go:embed lamp-transition.apng lamp-on.svg lamp-off.svg
//go:embed icons/app.png icons/lamp-on.png icons/lamp-off.png icons/lamp-unknown.png icons/menu-*.png
//go:embed icons/lamp-on.ico icons/lamp-off.ico icons/lamp-unknown.ico icons/lamp-connecting.ico icons/lamp-error.ico
//go:embed icons/menu-settings.ico icons/menu-diagnostics.ico icons/menu-reconnect.ico
//go:embed fonts/yekanbakh-vf.ttf
//go:embed sfx/*.wav
var Files embed.FS
