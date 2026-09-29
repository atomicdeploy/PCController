module pccontroller.local/controller

go 1.27.1

require (
	github.com/Microsoft/go-winio v0.6.2
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/charmbracelet/x/ansi v0.11.8
	github.com/coder/websocket v1.8.15
	github.com/fsnotify/fsnotify v1.10.1
	github.com/go-ole/go-ole v1.3.0
	github.com/grandcat/zeroconf v1.0.0
	github.com/muesli/termenv v0.16.0
	github.com/pelletier/go-toml/v2 v2.4.3
	go.bug.st/serial v1.8.0
	golang.org/x/net v0.59.0
	golang.org/x/sys v0.48.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	github.com/miekg/dns v1.1.73 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v1.2.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

// The upstream Windows backend does not cancel stalled/pending overlapped I/O
// before CloseHandle or close the device before joining completion. Pin the
// reviewed fork until bugst/go-serial#105 is resolved.
replace go.bug.st/serial => github.com/DRSDavidSoft/go-serial v0.0.0-20260928083401-fa09c8b9a680
