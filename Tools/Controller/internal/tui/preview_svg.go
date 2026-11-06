package tui

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

var previewSGR = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// PreviewSVG renders the actual terminal frame, including its ANSI palette,
// into a self-contained documentation asset. It never starts the host runtime.
func PreviewSVG(frame string) string {
	lines := strings.Split(strings.TrimRight(frame, "\r\n"), "\n")
	width := 1
	for _, line := range lines {
		width = max(width, ansi.StringWidth(line))
	}
	var out strings.Builder
	// Keep terminal spacing explicit, including in SVG2 browsers.
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="PCController terminal interface preview"><rect width="100%%" height="100%%" rx="12" fill="#121116"/><g font-family="Cascadia Mono,Consolas,DejaVu Sans Mono,monospace" font-size="14" style="white-space:pre" xml:space="preserve">`, width*9+32, len(lines)*20+32, width*9+32, len(lines)*20+32)
	fg, bg, bold, inverse := "#f5f2f7", "", false, false
	for row, line := range lines {
		x, offset := 16, 0
		write := func(text string) {
			text = ansi.Strip(text)
			if text == "" {
				return
			}
			pixels := ansi.StringWidth(text) * 9
			ink, fill := fg, bg
			if inverse {
				ink, fill = fill, ink
				if ink == "" {
					ink = "#121116"
				}
			}
			if fill != "" {
				fmt.Fprintf(&out, `<rect x="%d" y="%d" width="%d" height="20" fill="%s"/>`, x, 16+row*20, pixels, fill)
			}
			weight := "normal"
			if bold {
				weight = "bold"
			}
			// Browsers exclude trailing whitespace from textLength, even with
			// white-space:pre. Advance the complete terminal span ourselves and
			// fit only its visible content; NBSP preserves interior cell padding.
			visible := strings.Trim(text, " ")
			if visible != "" {
				leading := len(text) - len(strings.TrimLeft(text, " "))
				fmt.Fprintf(&out, `<text x="%d" y="%d" fill="%s" font-weight="%s" textLength="%d" lengthAdjust="spacingAndGlyphs">%s</text>`, x+leading*9, 31+row*20, ink, weight, ansi.StringWidth(visible)*9, html.EscapeString(strings.ReplaceAll(visible, " ", "\u00a0")))
			}
			x += pixels
		}
		for _, match := range previewSGR.FindAllStringSubmatchIndex(line, -1) {
			write(line[offset:match[0]])
			parts := strings.Split(line[match[2]:match[3]], ";")
			for i := 0; i < len(parts); i++ {
				code, _ := strconv.Atoi(parts[i])
				switch {
				case code == 0:
					fg, bg, bold, inverse = "#f5f2f7", "", false, false
				case code == 1:
					bold = true
				case code == 22:
					bold = false
				case code == 7:
					inverse = true
				case code == 27:
					inverse = false
				case code == 39:
					fg = "#f5f2f7"
				case code == 49:
					bg = ""
				case code >= 30 && code <= 37:
					fg = previewANSIColor(code - 30)
				case code >= 90 && code <= 97:
					fg = previewANSIColor(code - 90 + 8)
				case code >= 40 && code <= 47:
					bg = previewANSIColor(code - 40)
				case code >= 100 && code <= 107:
					bg = previewANSIColor(code - 100 + 8)
				case (code == 38 || code == 48) && i+2 < len(parts):
					color := ""
					if parts[i+1] == "5" {
						n, _ := strconv.Atoi(parts[i+2])
						color = previewANSIColor(n)
						i += 2
					} else if parts[i+1] == "2" && i+4 < len(parts) {
						r, _ := strconv.Atoi(parts[i+2])
						g, _ := strconv.Atoi(parts[i+3])
						b, _ := strconv.Atoi(parts[i+4])
						color = fmt.Sprintf("#%02x%02x%02x", max(0, min(255, r)), max(0, min(255, g)), max(0, min(255, b)))
						i += 4
					}
					if color != "" {
						if code == 38 {
							fg = color
						} else {
							bg = color
						}
					}
				}
			}
			offset = match[1]
		}
		write(line[offset:])
	}
	out.WriteString("</g></svg>\n")
	return out.String()
}

func previewANSIColor(n int) string {
	base := []string{"#121116", "#ed7889", "#73c69a", "#e7ae63", "#8ea7ee", "#c69bea", "#72cbd1", "#e4dfe9", "#938c9b", "#ff91a0", "#9bdab6", "#f1ce86", "#afc4ff", "#e0b6fa", "#9ce1e5", "#ffffff"}
	if n < 0 || n > 255 {
		return base[7]
	}
	if n < 16 {
		return base[n]
	}
	if n >= 232 {
		v := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
	levels := []int{0, 95, 135, 175, 215, 255}
	n -= 16
	return fmt.Sprintf("#%02x%02x%02x", levels[n/36], levels[(n/6)%6], levels[n%6])
}
