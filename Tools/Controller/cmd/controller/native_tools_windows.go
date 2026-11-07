//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/desktop/desktop"
)

// Operations are a closed presentation catalog, not a second RPC dispatcher.
// They always cross the same authenticated core and policy gates as Web/TUI.
type nativeToolOperation struct {
	id, label, section, method string
	fields                     []desktop.Field
	board, confirm             bool
	seconds                    int
	params                     func(map[string]string) (any, error)
	command                    func(map[string]string) ([]string, error)
}

func nativeToolOperations(key string) (string, []nativeToolOperation) {
	number := nativeNumber
	constant := func(words ...string) func(map[string]string) ([]string, error) {
		return func(map[string]string) ([]string, error) { return words, nil }
	}
	parameters := func(keys ...string) func(map[string]string) (any, error) {
		return func(values map[string]string) (any, error) {
			result := map[string]any{}
			for _, key := range keys {
				result[key] = values[key]
			}
			return result, nil
		}
	}
	switch key {
	case "host-automations":
		return "Host event automations", []nativeToolOperation{
			{id: "list", label: "Read automation rules", section: "Rules", command: constant("automation", "list")},
			{id: "run", label: "Run named automation", section: "Run", confirm: true, fields: []desktop.Field{field("name", "Automation name", "")}, command: func(v map[string]string) ([]string, error) {
				if strings.TrimSpace(v["name"]) == "" {
					return nil, errors.New("select an automation name")
				}
				return []string{"automation", "run", v["name"]}, nil
			}},
		}
	case "rf-transmit":
		return "RF transmitter", []nativeToolOperation{{id: "transmit", label: "Transmit waveform", section: "Transmit", method: "controller.rf.transmit", board: true, confirm: true, fields: []desktop.Field{number("code", "Code", 1, 1, 4294967295), number("bits", "Bit count", 24, 1, 32), number("protocol", "Protocol", 1, 1, 12), number("pulse_us", "Pulse (µs, 0 = protocol default)", 0, 0, 65535), number("repeats", "Repeats", 1, 1, 20)}, params: func(v map[string]string) (any, error) {
			result := map[string]uint64{}
			for _, spec := range []struct {
				key      string
				min, max uint64
			}{{"code", 1, 4294967295}, {"bits", 1, 32}, {"protocol", 1, 12}, {"pulse_us", 0, 65535}, {"repeats", 1, 20}} {
				n, err := strconv.ParseUint(v[spec.key], 10, 64)
				if err != nil || n < spec.min || n > spec.max {
					return nil, fmt.Errorf("%s must be %d–%d", spec.key, spec.min, spec.max)
				}
				result[spec.key] = n
			}
			return result, nil
		}}}
	case "safety":
		return "Safety & recovery", []nativeToolOperation{
			{id: "state", label: "Read safety state", method: "controller.estop.get", section: "Emergency stop"},
			{id: "stop", label: "Latch emergency stop", method: "controller.estop.set", section: "Emergency stop", params: func(map[string]string) (any, error) {
				return map[string]any{"active": true, "reason": "Native desktop emergency stop"}, nil
			}},
			{id: "release", label: "Release emergency stop", method: "controller.estop.set", section: "Emergency stop", confirm: true, params: func(map[string]string) (any, error) { return map[string]any{"active": false}, nil }},
			{id: "reset", label: "Reset application firmware", section: "Recovery", board: true, confirm: true, command: constant("reset", "app")},
			{id: "dtr", label: "Pulse DTR and reconnect", section: "Recovery", board: true, confirm: true, seconds: 30, command: constant("reset", "lines")},
		}
	case "i2c":
		return "I²C workbench", []nativeToolOperation{
			{id: "scan", label: "Scan bus", section: "Bus", board: true, command: constant("i2c", "scan")},
			{id: "release", label: "Release host lease", section: "Bus", board: true, command: constant("i2c", "release")},
			{id: "lcd", label: "LCD ownership", section: "Bus", board: true, command: constant("i2c", "lcd", "status")},
			{id: "rescan", label: "Rescan LCD", section: "Bus", board: true, command: constant("i2c", "lcd", "rescan")},
			{id: "transfer", label: "Transfer", section: "Transfer", board: true, confirm: true,
				fields: []desktop.Field{field("address", "7-bit address (decimal or 0x)", "0x27"), number("lease", "Lease (seconds)", 2, 0, 10), number("read_count", "Read bytes", 0, 0, 32), field("write_bytes", "Write bytes (decimal or 0x)", "")},
				command: func(v map[string]string) ([]string, error) {
					words := []string{"i2c", "transfer", v["address"], v["lease"], v["read_count"]}
					return append(words, strings.Fields(strings.ReplaceAll(v["write_bytes"], ",", " "))...), nil
				}},
		}
	case "network":
		return "Peers & discovery", []nativeToolOperation{
			{id: "scan", label: "Discover peers", section: "Discovery", method: "controller.discovery.scan", seconds: 35, fields: []desktop.Field{field("protocols", "Protocols (empty = all)", ""), number("timeout_ms", "Scan duration (ms)", 1500, 100, 30000)}, params: func(v map[string]string) (any, error) {
				n, e := strconv.Atoi(v["timeout_ms"])
				return map[string]any{"timeout_ms": n, "protocols": strings.Fields(strings.ReplaceAll(v["protocols"], ",", " "))}, e
			}},
			{id: "bridges", label: "Connected bridge peers", section: "Bridges", method: "controller.bridge.list"},
			{id: "instances", label: "Application instances", section: "Bridges", method: "controller.app.instances"},
			{id: "udp", label: "UDP transport status", section: "Binary UDP", method: "controller.udp.status"},
			{id: "udp-events", label: "Received UDP events", section: "Binary UDP", method: "controller.udp.events.get"},
		}
	case "history":
		query := func(timeline bool) func(map[string]string) (any, error) {
			return func(v map[string]string) (any, error) {
				minutes, err := strconv.Atoi(v["minutes"])
				if err != nil || minutes < 1 || minutes > 10080 {
					return nil, errors.New("history range must be 1–10080 minutes")
				}
				result := map[string]any{"since": time.Now().Add(-time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339Nano)}
				if timeline {
					limit, e := strconv.Atoi(v["limit"])
					if e != nil {
						return nil, e
					}
					result["limit"] = limit
				}
				return result, nil
			}
		}
		return "Telemetry history", []nativeToolOperation{
			{id: "status", label: "Read measurement history", section: "History", method: "controller.history.status", fields: []desktop.Field{number("minutes", "Previous minutes", 15, 1, 10080)}, params: query(false)},
			{id: "timeline", label: "Read timeline", section: "History", method: "controller.history.timeline", fields: []desktop.Field{number("limit", "Maximum events", 500, 1, 10000)}, params: query(true)},
		}
	case "host-tools":
		return "Host operating system", []nativeToolOperation{
			{id: "status", label: "System status", section: "Facts", method: "controller.os.status"},
			{id: "catalog", label: "Fact profiles", section: "Facts", method: "controller.os.facts.catalog"},
			{id: "facts", label: "Read facts", section: "Facts", method: "controller.os.facts", fields: []desktop.Field{field("profile", "Profile", "")}, params: parameters("profile")},
			{id: "key", label: "Send virtual key", section: "Virtual keys", method: "controller.os.key", confirm: true, fields: []desktop.Field{field("key", "Key", "MEDIA_PLAY_PAUSE"), number("hold_ms", "Hold (ms)", 50, 1, 5000)}, params: func(v map[string]string) (any, error) {
				n, e := strconv.Atoi(v["hold_ms"])
				return map[string]any{"key": v["key"], "hold_ms": n}, e
			}},
			{id: "brightness", label: "Read display brightness", section: "Display", command: constant("os", "brightness", "get")},
			{id: "brightness-set", label: "Set display brightness", section: "Display", confirm: true, fields: []desktop.Field{number("percent", "Brightness (%)", 50, 0, 100)}, command: func(v map[string]string) ([]string, error) {
				return []string{"os", "brightness", "set", v["percent"]}, nil
			}},
			{id: "power", label: "Request power action", section: "Power", method: "controller.os.power", confirm: true, fields: []desktop.Field{choice("action", "Power action", "lock", "lock", "sleep", "hibernate", "shutdown", "restart"), field("confirmation", "Policy confirmation", "")}, params: parameters("action", "confirmation")},
		}
	case "local-device":
		return "Local device", []nativeToolOperation{
			{id: "status", label: "Refresh device status", section: "Device", method: "controller.device.status"},
			{id: "inspect", label: "Inspect resource", section: "Device", method: "controller.device.inspect", fields: []desktop.Field{field("resource", "Resource", "capabilities")}, params: parameters("resource")},
			{id: "action", label: "Invoke advertised action", section: "Actions", method: "controller.device.action", confirm: true, fields: []desktop.Field{field("action", "Action ID", ""), field("text", "Text", ""), number("count", "Count", 1, 1, 255)}, params: func(v map[string]string) (any, error) {
				n, e := strconv.Atoi(v["count"])
				return map[string]any{"action": v["action"], "text": v["text"], "count": n}, e
			}},
		}
	case "firmware-tools":
		return "Firmware & toolchain", []nativeToolOperation{
			{id: "profile", label: "Toolchain profile", section: "Toolchain", command: constant("toolchain", "profile")},
			{id: "features", label: "Configured feature profile", section: "Toolchain", command: constant("toolchain", "features")},
			{id: "plan", label: "Preview system provisioning", section: "Toolchain", command: constant("toolchain", "bootstrap", "--dry-run"), seconds: 60},
			{id: "provision", label: "Provision system toolchain", section: "Toolchain", command: constant("toolchain", "bootstrap"), seconds: 300, confirm: true},
			{id: "sync", label: "Check core/library upgrades", section: "Toolchain", command: constant("toolchain", "sync"), seconds: 300, confirm: true},
			{id: "compile", label: "Build firmware (no upload)", section: "Build", method: "controller.firmware.build", seconds: 300, fields: []desktop.Field{field("features", "Feature names (empty = saved profile)", ""), nativeBool("no_features", "Disable optional features", false)}, params: func(v map[string]string) (any, error) {
				return controller.FirmwareBuildRequest{FirmwareFeatures: strings.Fields(strings.ReplaceAll(v["features"], ",", " ")), NoFirmwareFeatures: v["no_features"] == "true"}, nil
			}},
			{id: "usbasp", label: "USBasp driver status", section: "Drivers", command: constant("driver", "usbasp", "status")},
			{id: "usbasp-install", label: "Install USBasp driver", section: "Drivers", command: constant("driver", "usbasp", "ensure"), seconds: 120, confirm: true},
		}
	case "status-lighting":
		colorFields := []desktop.Field{number("red", "Red", 128, 0, 255), number("green", "Green", 64, 0, 255), number("blue", "Blue", 255, 0, 255), number("brightness", "Brightness", 128, 0, 255)}
		return "Status lighting", []nativeToolOperation{
			{id: "color", label: "Preview color", section: "Color", method: "controller.status_led.set", board: true, fields: colorFields, params: func(v map[string]string) (any, error) {
				out := map[string]int{}
				for _, k := range []string{"red", "green", "blue", "brightness"} {
					n, e := strconv.Atoi(v[k])
					if e != nil {
						return nil, e
					}
					out[k] = n
				}
				return out, nil
			}},
			{id: "release", label: "Restore status policy", section: "Color", board: true, method: "controller.status_led.release"},
			{id: "profiles", label: "Read board profiles", section: "Board profiles", board: true, command: constant("rgb", "profile", "list")},
			{id: "profile-save", label: "Save board profile", section: "Board profiles", board: true, confirm: true, fields: []desktop.Field{field("condition", "Condition", "idle"), field("profile_color", "Color (#RRGGBB)", "#8040ff"), number("profile_brightness", "Profile brightness", 128, 0, 255)}, command: func(v map[string]string) ([]string, error) {
				return []string{"rgb", "profile", "set", v["condition"], "color", v["profile_color"], v["profile_brightness"]}, nil
			}},
		}
	}
	return "", nil
}

func (p nativePages) loadNativeTools(key string, state control.Snapshot) (desktop.Page, error) {
	title, operations := nativeToolOperations(key)
	if title == "" {
		return desktop.Page{}, fmt.Errorf("unknown native activity %q", key)
	}
	page := desktop.Page{Title: title}
	seen := map[string]bool{}
	for _, op := range operations {
		for _, field := range op.fields {
			if !seen[field.Key] {
				field.Section = op.section
				page.Fields = append(page.Fields, field)
				seen[field.Key] = true
			}
		}
		if len(op.fields) == 0 && !seen["section:"+op.section] {
			// A section containing only actions still needs a selectable native tab.
			page.Fields = append(page.Fields, desktop.Field{Key: "section:" + op.section, Label: "", ReadOnly: true, Section: op.section})
		}
		seen["section:"+op.section] = true
		enabled := !p.preview && (!op.board || state.Connected && !state.Programming.Active && !state.EmergencyStop.Active)
		page.Actions = append(page.Actions, desktop.PageAction{ID: op.id, Label: op.label, Section: op.section, Enabled: enabled, Confirm: op.confirm, TimeoutSeconds: op.seconds})
	}
	return page, nil
}

func (p nativePages) submitNativeTools(ctx context.Context, key, action string, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot execute controller operations")
	}
	_, operations := nativeToolOperations(key)
	for _, op := range operations {
		if op.id != action {
			continue
		}
		var result any
		if op.command != nil {
			words, err := op.command(values)
			if err != nil {
				return desktop.Page{}, err
			}
			output, err := p.command(ctx, shell.Join(words))
			if err != nil {
				return desktop.Page{}, err
			}
			result = output
		} else {
			var params any = map[string]any{}
			if op.params != nil {
				var err error
				params, err = op.params(values)
				if err != nil {
					return desktop.Page{}, err
				}
			}
			if err := p.call(ctx, op.method, params, &result); err != nil {
				return desktop.Page{}, err
			}
		}
		var state control.Snapshot
		if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
			return desktop.Page{}, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
		}
		page, err := p.loadNativeTools(key, state)
		for i := range page.Fields {
			if value, ok := values[page.Fields[i].Key]; ok {
				page.Fields[i].Value = value
			}
		}
		if text, ok := result.(string); ok {
			page.Output = text
		} else {
			encoded, _ := json.MarshalIndent(result, "", "  ")
			page.Output = string(encoded)
		}
		return page, err
	}
	return desktop.Page{}, errors.New("unknown native operation")
}
