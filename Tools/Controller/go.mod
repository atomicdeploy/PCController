module pccontroller.local/controller

// Imported, history-preserving PCControllerDesktop native presentation stack.
replace pccontroller.local/desktop => ../NativeDesktop

// Preserve donor fixes to Walk's DPI/RTL geometry and native controls.
replace github.com/lxn/walk => ../NativeDesktop/vendor/github.com/lxn/walk

go 1.27.3

require (
	github.com/Microsoft/go-winio v0.6.4
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/charmbracelet/x/term v0.2.2
	github.com/coder/websocket v1.8.15
	github.com/fsnotify/fsnotify v1.10.1
	github.com/go-ole/go-ole v1.3.0
	github.com/grandcat/zeroconf v1.0.0
	github.com/mattn/go-isatty v0.0.24
	github.com/muesli/termenv v0.16.0
	github.com/pelletier/go-toml/v2 v2.4.3
	go.bug.st/serial v1.8.0
	golang.org/x/net v0.62.0
	golang.org/x/sys v0.49.0
	gopkg.in/yaml.v3 v3.0.1
	pccontroller.local/desktop v0.0.0
)

require (
	git.sr.ht/~jackmordaunt/go-toast v1.1.2 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/kettek/apng v0.0.0-20250827064933-2bb5f5fcf253 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/lxn/walk v0.0.0-20210112085537-c389da54e794 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/miekg/dns v1.1.73 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/xo/terminfo v1.3.0 // indirect
	golang.org/x/image v0.0.0-20211028202545-6944b10bf410 // indirect
	golang.org/x/text v0.43.0 // indirect
	gopkg.in/Knetic/govaluate.v3 v3.0.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

// The upstream Windows backend does not cancel stalled/pending overlapped I/O
// before CloseHandle or close the device before joining completion. Pin the
// reviewed fork until bugst/go-serial#105 is resolved.
replace go.bug.st/serial => github.com/DRSDavidSoft/go-serial v0.0.0-20260928083401-fa09c8b9a680
