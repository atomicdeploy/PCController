package tui

import (
	"sort"
	"strings"

	"pccontroller.local/controller/internal/shell"
)

var nestedCompletions = map[string][]string{
	"config": {"get", "set"},
	"config get": {
		"ui.app_title",
		"ui.tagline",
		"ui.appearance.theme",
		"ui.appearance.locale",
		"ui.appearance.direction",
		"ui.appearance.reduce_motion",
		"ui.appearance.compact_numbers",
		"ui.tui_console.enabled",
		"ui.tui_console.columns",
		"ui.tui_console.rows",
		"ui.tui_console.font_face",
		"ui.tui_console.font_size",
		"ui.appearance.audio_muted",
		"ui.appearance.audio_volume",
	},
	"config set": {
		"ui.app_title",
		"ui.tagline",
		"ui.appearance.theme",
		"ui.appearance.locale",
		"ui.appearance.direction",
		"ui.appearance.reduce_motion",
		"ui.appearance.compact_numbers",
		"ui.appearance.audio_muted",
		"ui.appearance.audio_volume",
		"ui.tui_console.enabled",
		"ui.tui_console.columns",
		"ui.tui_console.rows",
		"ui.tui_console.font_face",
		"ui.tui_console.font_size",
	},
	"silent":            {"status", "on", "off", "board", "host", "both"},
	"silent board":      {"status", "on", "off"},
	"silent host":       {"status", "on", "off"},
	"silent both":       {"status", "on", "off"},
	"buzzer":            {"status", "path"},
	"buzzer path":       {"board", "host", "both", "none"},
	"menu":              {"list", "prev", "next", "dec", "inc", "page"},
	"menu page":         {"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13"},
	"relay":             {"1", "2", "3", "4", "5", "6", "7", "8", "side", "off", "test"},
	"relay 1":           {"on", "off", "toggle"},
	"relay 2":           {"on", "off", "toggle"},
	"relay 3":           {"on", "off", "toggle"},
	"relay 4":           {"on", "off", "toggle"},
	"relay 5":           {"on", "off", "toggle"},
	"relay 6":           {"on", "off", "toggle"},
	"relay 7":           {"on", "off", "toggle"},
	"relay 8":           {"on", "off", "toggle"},
	"relay side":        {"left", "right"},
	"relay side left":   {"stop", "up", "down"},
	"relay side right":  {"stop", "up", "down"},
	"pwm":               {"get", "off", "set"},
	"settings":          {"decimals", "color", "set"},
	"rf":                {"send", "learn", "cancel", "list", "remove", "map"},
	"rf learn":          {"indefinite", "timer", "single", "one-shot"},
	"rf learn timer":    {"15s", "30s", "60s"},
	"rf learn single":   {"15s", "30s", "60s"},
	"rf learn one-shot": {"15s", "30s", "60s"},
	"rf remove":         {"all"},
	"rgb":               {"color", "effect", "profile"},
	"rgb effect":        {"list", "play", "wait", "breathe", "flash", "cycle", "transition", "stop", "status"},
	"rgb profile":       {"list", "get", "set"},
	"melody":            {"list", "create", "play", "wait", "stop", "status"},
	"macro":             {"list", "play", "status", "cancel"},
	"automation":        {"list", "run"},
	"reset":             {"lines", "app", "bootloader"},
	"boot":              {"probe", "info", "metadata", "backup", "read", "write", "verify", "start"},
	"program":           {"flash", "probe", "metadata", "backup", "read", "verify", "compile", "urclock"},
	"program flash":     {"firmware.hex"},
	"toolchain":         {"bootstrap", "sync", "profile", "compile", "core-info", "install-bootloader"},
	"board":             {"initialize", "name"},
	"board initialize":  {"--name=", "--uart=auto", "--uart=none", "--bootloader-only", "--skip-toolchain"},
	"board name":        {"get", "set ", "clear"},
	"keyboard":          {"status", "list", "enable", "disable", "stop"},
	"display":           {"segments", "lcd", "both"},
	"i2c":               {"scan"},
}

func completionCandidates(engine *shell.Engine, line string) []string {
	leading := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	trimmed := strings.TrimLeft(line, " \t")
	endsWithSpace := len(trimmed) > 0 && (trimmed[len(trimmed)-1] == ' ' || trimmed[len(trimmed)-1] == '\t')
	words := strings.Fields(strings.ToLower(trimmed))
	if len(words) == 0 {
		return prefixLines(leading, engine.Complete(""))
	}
	if len(words) == 1 && !endsWithSpace {
		return prefixLines(leading, engine.Complete(words[0]))
	}

	prefixWords := words
	fragment := ""
	if !endsWithSpace {
		prefixWords = words[:len(words)-1]
		fragment = words[len(words)-1]
	}
	path := strings.Join(prefixWords, " ")
	choices := nestedCompletions[path]
	var result []string
	for _, choice := range choices {
		if strings.HasPrefix(choice, fragment) {
			parts := append(append([]string(nil), prefixWords...), choice)
			result = append(result, leading+strings.Join(parts, " "))
		}
	}
	sort.Strings(result)
	return result
}

func prefixLines(prefix string, values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = prefix + value
	}
	return result
}

func commonCompletionPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) && prefix != "" {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}
