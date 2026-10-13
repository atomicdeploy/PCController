// Package startupmode classifies presentation only, without configuration,
// discovery, transport initialization or output mutation.
package startupmode

import "strings"

func Command(args []string) string {
	remaining := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		value := args[index]
		global := false
		for _, flag := range []string{"--config", "--app-name", "--tagline", "--app-tagline"} {
			if value == flag {
				if index+1 >= len(args) {
					return "invalid"
				}
				index++
				if strings.TrimSpace(args[index]) == "" {
					return "invalid"
				}
				global = true
				break
			}
			if strings.HasPrefix(value, flag+"=") {
				if strings.TrimSpace(strings.TrimPrefix(value, flag+"=")) == "" {
					return "invalid"
				}
				global = true
				break
			}
		}
		if !global {
			remaining = append(remaining, value)
		}
	}
	if len(remaining) == 0 {
		return "tui"
	}
	return strings.ToLower(remaining[0])
}

func TUI(args []string) bool { return Command(args) == "tui" }

// BackgroundWeb disables the terminal explicitly. Browser/tray preferences
// do not disable the TUI: even a --no-open --no-tray host has a terminal UI.
func BackgroundWeb(args []string) bool {
	if Command(args) != "web" {
		return false
	}
	var noTUI bool
	for index := 0; index < len(args); index++ {
		flag := args[index]
		if flag == "--config" || flag == "--app-name" || flag == "--tagline" || flag == "--app-tagline" {
			index++
			continue
		}
		switch flag {
		case "--help", "-h":
			return false
		case "--no-tui", "--no-tui=true":
			noTUI = true
		case "--no-tui=false":
			noTUI = false
		}
	}
	return noTUI
}
