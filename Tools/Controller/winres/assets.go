// Package winres owns the built-in desktop artwork used by standalone hosts.
package winres

import "embed"

// ToastImages includes the default logo and every semantic notification icon.
//
//go:embed icon.png toast-event-*.png
var ToastImages embed.FS
