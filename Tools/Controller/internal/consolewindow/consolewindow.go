// Package consolewindow manages the visible local console used by the TUI.
// It never attempts to resize or restyle an SSH/RDP client terminal.
package consolewindow

import (
	"errors"
	"os"
	"strings"
	"unicode/utf16"
)

const (
	MinimumColumns  = 56
	MaximumColumns  = 300
	MinimumRows     = 18
	MaximumRows     = 120
	MinimumFontSize = 5
	MaximumFontSize = 72
)

// Settings is the effective, fully resolved local-console presentation.
type Settings struct {
	Enabled  bool
	Columns  int
	Rows     int
	FontFace string
	FontSize int
}

// Result distinguishes an applied local-console change from a deliberate,
// harmless skip on a terminal whose presentation is owned elsewhere.
type Result struct {
	Applied bool
	Reason  string
}

// Validate rejects values that cannot be represented safely by the Win32
// classic-console APIs or by the TUI's minimum usable layout.
func Validate(settings Settings) error {
	if settings.Columns < MinimumColumns || settings.Columns > MaximumColumns {
		return errors.New("console columns must be 56..300")
	}
	if settings.Rows < MinimumRows || settings.Rows > MaximumRows {
		return errors.New("console rows must be 18..120")
	}
	face := strings.TrimSpace(settings.FontFace)
	if face == "" || len(utf16.Encode([]rune(face))) > 31 {
		return errors.New("console font face must be 1..31 UTF-16 code units")
	}
	for _, character := range face {
		if character < 0x20 || character == 0x7f {
			return errors.New("console font face must contain printable characters only")
		}
	}
	if settings.FontSize < MinimumFontSize || settings.FontSize > MaximumFontSize {
		return errors.New("console font size must be 5..72")
	}
	return nil
}

// Apply updates a local, attached platform console. Unsupported platforms,
// redirected/pseudoconsole sessions, and remote terminals return an explicit
// skipped result so callers can choose between a warning and a strict error.
func Apply(settings Settings) (Result, error) {
	if !settings.Enabled {
		return Result{Reason: "local console management is disabled"}, nil
	}
	settings = normalizeSettings(settings)
	if err := Validate(settings); err != nil {
		return Result{}, err
	}
	if reason := remoteSessionReason(os.Getenv); reason != "" {
		return Result{Reason: reason}, nil
	}
	return applyPlatform(settings)
}

func normalizeSettings(settings Settings) Settings {
	settings.FontFace = strings.TrimSpace(settings.FontFace)
	return settings
}

func remoteSessionReason(getenv func(string) string) string {
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if strings.TrimSpace(getenv(name)) != "" {
			return "remote SSH terminal owns its window size and font"
		}
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(getenv("SESSIONNAME"))), "RDP-") {
		return "remote desktop session owns its console presentation"
	}
	return ""
}
