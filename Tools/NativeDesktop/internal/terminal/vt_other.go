//go:build !windows

package terminal

import (
	"io"
	"os"
	"strings"
)

// Unix-like terminals already interpret ANSI/VT output. Keep the same API as
// Windows so command startup is platform-neutral.
func EnableVT100(writers ...io.Writer) (bool, error) {
	for _, writer := range writers {
		if IsTerminal(writer) {
			return true, nil
		}
	}
	return false, nil
}

func platformEmojiSupported(_ *os.File) bool {
	locale := strings.ToLower(
		os.Getenv("LC_ALL") + " " +
			os.Getenv("LC_CTYPE") + " " +
			os.Getenv("LANG"),
	)
	return strings.Contains(locale, "utf-8") ||
		strings.Contains(locale, "utf8")
}
