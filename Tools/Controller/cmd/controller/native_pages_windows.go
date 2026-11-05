//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
	"rayanlamp.local/app/desktop"
	"strconv"
	"strings"
	"sync"
)

type nativePages struct {
	client  *remoteTUIIPC
	preview bool
	events  *nativeEventBuffer
}

// The desktop consumes the existing owner event stream; opening a page never
// starts another serial reader or unbounded history allocation.
type nativeEventBuffer struct {
	mu   sync.Mutex
	rows [][]string
}

func (b *nativeEventBuffer) append(event control.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.rows) == 200 {
		copy(b.rows, b.rows[1:])
		b.rows = b.rows[:199]
	}
	b.rows = append(b.rows, []string{strconv.FormatUint(event.ID, 10), event.Time.Local().Format("15:04:05.000"), event.Kind, event.Text})
}
func (b *nativeEventBuffer) snapshot() [][]string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]string(nil), b.rows...)
}

func choice(key, label, value string, choices ...string) desktop.Field {
	return desktop.Field{Key: key, Label: label, Value: value, Choices: choices}
}
func field(key, label, value string) desktop.Field {
	return desktop.Field{Key: key, Label: label, Value: value}
}
func pageAction(id, label string, enabled bool) desktop.PageAction {
	return desktop.PageAction{ID: id, Label: label, Enabled: enabled}
}
func (p nativePages) load(ctx context.Context, key string) (desktop.Page, error) {
	state := control.Snapshot{}
	if !p.preview {
		var err error
		state, err = p.client.Snapshot(ctx)
		if err != nil {
			return desktop.Page{}, err
		}
	}
	enabled := state.Connected && !state.EmergencyStop.Active && !state.Programming.Active
	page := desktop.Page{}
	switch key {
	case "events":
		page.Title = "Live event history"
		page.Columns = []string{"ID", "Time", "Event", "Detail"}
		page.Rows = p.events.snapshot()
	case "connection":
		page.Title = "Board connection"
		status := "Disconnected"
		if state.Connected {
			status = "Connected"
		}
		page.Fields = []desktop.Field{{Key: "state", Label: "Status", Value: status, ReadOnly: true}}
		if state.ConnectionReason != "" {
			page.Fields = append(page.Fields, desktop.Field{Key: "reason", Label: "Detail", Value: state.ConnectionReason, ReadOnly: true})
		}
		if state.Connected {
			page.Fields = append(page.Fields, desktop.Field{Key: "port", Label: "Port", Value: state.Port.Name, ReadOnly: true})
			page.Actions = []desktop.PageAction{{ID: "disconnect", Label: "Disconnect", Enabled: !state.Programming.Active, Confirm: true}}
		} else {
			ports := []string{"Automatic"}
			if !p.preview {
				available, err := p.client.Ports(ctx)
				if err != nil {
					return page, err
				}
				for _, port := range available {
					ports = append(ports, port.Name)
				}
			}
			page.Fields = append(page.Fields, choice("port", "Port", ports[0], ports...))
			page.Actions = []desktop.PageAction{pageAction("connect", "Connect", !p.preview && !state.Programming.Active)}
		}
	case "firmware":
		page.Title = "Firmware & backups"
		page.Columns = []string{"SHA-256", "Name", "Kind", "Size"}
		var manifest artifacts.Manifest
		var inventory artifacts.List
		if !p.preview {
			if err := p.client.call(ctx, "controller.artifact.manifest", map[string]any{}, &manifest); err != nil {
				return page, err
			}
			if err := p.client.call(ctx, "controller.artifact.list", map[string]any{}, &inventory); err != nil {
				return page, err
			}
		}
		for _, artifact := range inventory.Artifacts {
			if string(artifact.Kind) == "firmware" {
				page.Rows = append(page.Rows, []string{artifact.SHA256, artifact.Name, string(artifact.Kind), fmt.Sprintf("%d bytes", artifact.Bytes)})
			}
		}
		if manifest.Board.BuildHash != "" {
			page.Fields = append(page.Fields, desktop.Field{Key: "build", Label: "Running board", Value: manifest.Board.BuildHash, ReadOnly: true})
		}
		if update := manifest.Update; update != nil {
			progress := update.State
			if update.Stage != "" {
				progress += " · " + update.Stage
			}
			if update.ProgressKnown {
				progress += fmt.Sprintf(" · %d%%", update.ProgressPercent)
			}
			page.Fields = append(page.Fields, desktop.Field{Key: "progress", Label: "Update", Value: progress, ReadOnly: true})
			if update.Detail != "" {
				page.Fields = append(page.Fields, desktop.Field{Key: "detail", Label: "Detail", Value: update.Detail, ReadOnly: true})
			}
		}
		page.Fields = append(page.Fields, choice("method", "Programming method", "urclock", "urclock", "usbasp"))
		page.Actions = []desktop.PageAction{{ID: "backup", Label: "Back up flash", Enabled: enabled && manifest.Enabled, Confirm: true}, {ID: "upload", Label: "Install selected firmware", Enabled: enabled && manifest.Enabled && len(page.Rows) > 0, Confirm: true}}
	case "settings":
		page.Title = "Preferences"
		var cfg struct {
			AppTitle            string `json:"app_title"`
			IntroMode           string `json:"intro_mode"`
			ConfigFile          string `json:"config_file"`
			ConfigOpenSupported bool   `json:"config_open_supported"`
		}
		cfg.AppTitle = "PCController"
		cfg.IntroMode = "auto"
		if !p.preview {
			if err := p.client.call(ctx, "controller.ui.config", map[string]any{}, &cfg); err != nil {
				return page, err
			}
		}
		page.Fields = []desktop.Field{field("app_title", "Application name", cfg.AppTitle), choice("intro_mode", "Welcome experience", cfg.IntroMode, "auto", "always", "skip")}
		page.Actions = []desktop.PageAction{pageAction("save", "Save", !p.preview)}
		if cfg.ConfigOpenSupported {
			page.Actions = append(page.Actions, pageAction("open-config", "Open configuration", true), pageAction("open-folder", "Open folder", true))
		}
	case "rf":
		page.Title = "RF reception & learning"
		page.Columns = []string{"ID", "Name / code", "Bits / protocol", "Assignment"}
		var catalog struct {
			Entries []struct {
				controllerapi.RFEntryView
				BoardAssignment struct {
					Label string `json:"label"`
				} `json:"board_assignment"`
			} `json:"entries"`
			Preferences appconfig.RFLearningPreferences `json:"preferences"`
			Learning    control.RFLearnState            `json:"learning"`
			Peripherals struct {
				Controls []appconfig.ControlDescriptor `json:"controls"`
			} `json:"peripherals"`
			BoardError string `json:"board_error"`
		}
		catalog.Preferences.TimeoutSeconds = 60
		if !p.preview {
			if err := p.client.call(ctx, "controller.rf.catalog", map[string]bool{"read_board": true}, &catalog); err != nil {
				return page, err
			}
		}
		for _, entry := range catalog.Entries {
			name := entry.Name
			if name == "" {
				name = entry.CodeDisplay
			}
			page.Rows = append(page.Rows, []string{strconv.Itoa(int(entry.ID)), name, fmt.Sprintf("%d / %d", entry.Bits, entry.Protocol), entry.BoardAssignment.Label})
		}
		page.Fields = []desktop.Field{
			choice("timed", "Timed learning", strconv.FormatBool(catalog.Preferences.Timed), "false", "true"),
			field("timeout_seconds", "Timeout (seconds)", strconv.Itoa(catalog.Preferences.TimeoutSeconds)),
			choice("continuous", "Continue after learning", strconv.FormatBool(catalog.Preferences.Continuous), "false", "true"),
		}
		page.Actions = []desktop.PageAction{pageAction("save", "Save preferences", !p.preview)}
		if len(catalog.Entries) > 0 {
			actions := []string{}
			for _, channel := range catalog.Peripherals.Controls {
				if channel.Hidden || channel.Locked {
					continue
				}
				for _, action := range channel.Actions {
					actions = append(actions, channel.Name+" · "+action.Name+" ["+action.ID+"]")
				}
			}
			if len(actions) > 0 {
				page.Fields = append(page.Fields, field("binding_name", "New assignment name", ""), choice("gesture", "Gesture", "tap", "tap", "hold", "repeat", "hold-end"), choice("control_action", "Channel action", actions[0], actions...))
				page.Actions = append(page.Actions, pageAction("assign", "Assign selected", enabled))
			}
		}
		if catalog.Learning.Active {
			page.Actions = append(page.Actions, pageAction("cancel", "Stop learning", enabled))
		} else {
			page.Actions = append(page.Actions, pageAction("learn", "Start learning", enabled && state.Hello.Capabilities&native.CapabilityRF != 0))
		}
	case "programs":
		page.Title = "Programs & effects"
		page.Columns = []string{"Reference", "Name", "Group", "Duration"}
		for _, effect := range state.Effects {
			page.Rows = append(page.Rows, []string{effect.Reference, effect.Name, effect.Category, fmt.Sprintf("%.2f s", float64(effect.DurationMS)/1000)})
		}
		mode := string(state.ProgramState.Mode)
		if mode == "" {
			mode = "Idle"
		}
		page.Fields = []desktop.Field{choice("mode", "Application mode", strings.ToLower(mode), "idle", "running")}
		page.Actions = []desktop.PageAction{pageAction("mode", "Apply mode", !p.preview), pageAction("play", "Play selected", enabled && len(page.Rows) > 0)}
	case "buzzer":
		page.Title = "Buzzer & melodies"
		var melodies []appconfig.Melody
		if !p.preview {
			if err := p.client.call(ctx, "controller.melodies.list", map[string]any{}, &melodies); err != nil {
				return page, err
			}
		}
		names := []string{}
		for _, melody := range melodies {
			names = append(names, melody.Name)
		}
		page.Fields = []desktop.Field{field("frequency", "Tone frequency (Hz)", "880"), field("duration", "Tone duration (ms)", "115")}
		page.Actions = []desktop.PageAction{pageAction("tone", "Play tone", enabled)}
		if len(names) > 0 {
			page.Fields = append(page.Fields, choice("melody", "Melody", names[0], names...), field("repeats", "Repeats", "1"))
			page.Actions = append(page.Actions, pageAction("melody", "Play melody", enabled))
		}
		busy, known := native.BuzzerBusy(state.Hello, state.Status)
		if known && busy {
			page.Actions = append(page.Actions, pageAction("stop", "Stop buzzer", state.Connected))
		}
	case "strip":
		page.Title = "LED strip"
		enabled = enabled && state.Hello.Capabilities&native.CapabilityAddressableLED != 0
		var stream struct {
			Running bool `json:"running"`
		}
		if !p.preview {
			status, err := p.client.Execute(ctx, "strip status")
			if err != nil {
				return page, err
			}
			if err := json.Unmarshal([]byte(status), &stream); err != nil {
				return page, fmt.Errorf("read strip state: %w", err)
			}
		}
		page.Fields = []desktop.Field{choice("order", "Pixel order", "GRB", "GRB", "RGB", "BRG", "BGR", "RBG", "GBR"), field("pixels", "Pixel count", strconv.Itoa(native.StripMaximumPixels)), field("fps", "Frame rate", "60"), field("red", "Red (0–255)", "255"), field("green", "Green (0–255)", "160"), field("blue", "Blue (0–255)", "40")}
		page.Actions = []desktop.PageAction{pageAction("order", "Apply pixel order", enabled), pageAction("fill", "Fill color", enabled)}
		if stream.Running {
			page.Actions = append(page.Actions, pageAction("stop", "Stop stream", state.Connected))
		} else {
			page.Actions = append(page.Actions, pageAction("rainbow", "Start rainbow", enabled))
		}
	case "display":
		page.Title = "Front-panel display"
		page.Fields = []desktop.Field{choice("target", "Display", "segments", "segments", "lcd", "both"), field("text", "Text", ""), field("duration", "Duration (seconds)", "5"), choice("repeat", "Repeat", "once", "once", "loop")}
		page.Actions = []desktop.PageAction{pageAction("show", "Show text", enabled)}
	default:
		return page, fmt.Errorf("unknown native page %q", key)
	}
	styleNativeFields(key, &page)
	return page, nil
}

// The presentation seam carries constraints as data so all native editors use
// the same heights, keyboard/spin behavior, and logical section grouping.
func styleNativeFields(key string, page *desktop.Page) {
	for i := range page.Fields {
		f := &page.Fields[i]
		f.Section = page.Title
		switch key {
		case "rf":
			f.Section = "Learning preferences"
			if f.Key == "binding_name" || f.Key == "gesture" || f.Key == "control_action" {
				f.Section = "Selected code assignment"
			}
		case "buzzer":
			f.Section = "Tone test"
			if f.Key == "melody" || f.Key == "repeats" {
				f.Section = "Melody playback"
			}
		case "strip":
			f.Section = "Pixel configuration"
			if f.Key == "red" || f.Key == "green" || f.Key == "blue" {
				f.Section = "Solid color"
			}
		}
		switch f.Key {
		case "frequency":
			f.Numeric, f.Min, f.Max = true, 20, 20000
		case "duration":
			f.Numeric, f.Min, f.Max = true, 1, 3600
			if key == "buzzer" {
				f.Max = 65535
			}
		case "timeout_seconds":
			f.Numeric, f.Min, f.Max = true, 1, 120
		case "repeats":
			f.Numeric, f.Min, f.Max = true, 1, 255
		case "pixels":
			f.Numeric, f.Min, f.Max = true, 1, float64(native.StripMaximumPixels)
		case "fps":
			f.Numeric, f.Min, f.Max = true, 1, float64(appconfig.StripMaximumFPS)
		case "red", "green", "blue":
			f.Numeric, f.Min, f.Max = true, 0, 255
		}
	}
}

func (p nativePages) submit(ctx context.Context, key, action string, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot change controller state")
	}
	call := func(method string, params any) error {
		var result any
		return p.client.call(ctx, method, params, &result)
	}
	execute := func(command string) error { _, err := p.client.Execute(ctx, command); return err }
	number := func(key string, min, max int) (int, error) {
		value, err := strconv.Atoi(strings.TrimSpace(values[key]))
		if err != nil || value < min || value > max {
			return 0, fmt.Errorf("%s must be %d–%d", key, min, max)
		}
		return value, nil
	}
	var err error
	switch key {
	case "connection":
		switch action {
		case "connect":
			port := values["port"]
			if port == "Automatic" {
				port = ""
			}
			err = call("controller.connect", map[string]string{"port": port})
		case "disconnect":
			err = call("controller.close", map[string]any{})
		default:
			err = errors.New("unknown connection action")
		}
	case "firmware":
		method := values["method"]
		if method != "urclock" && method != "usbasp" {
			return desktop.Page{}, errors.New("select a programming method")
		}
		switch action {
		case "backup":
			err = call("controller.board.firmware.download", artifacts.CaptureRequest{Components: []string{"flash"}, Authorized: true, Method: method})
		case "upload":
			digest := values["selected"]
			var inventory artifacts.List
			if err = p.client.call(ctx, "controller.artifact.list", map[string]string{"kind": "firmware"}, &inventory); err != nil {
				return desktop.Page{}, err
			}
			found := false
			for _, artifact := range inventory.Artifacts {
				if artifact.SHA256 == digest {
					found = true
					break
				}
			}
			if !found {
				return desktop.Page{}, errors.New("select a stored firmware image first")
			}
			err = call("controller.update.firmware", artifacts.UpdateRequest{ArtifactSHA256: digest, Authorized: true, Method: method})
		default:
			err = errors.New("unknown firmware action")
		}
	case "settings":
		switch action {
		case "save":
			err = call("controller.ui.config.set", map[string]string{"app_title": values["app_title"], "intro_mode": values["intro_mode"]})
		case "open-config":
			err = call("controller.ui.config.open", map[string]string{"target": "file"})
		case "open-folder":
			err = call("controller.ui.config.open", map[string]string{"target": "folder"})
		default:
			err = errors.New("unknown preferences action")
		}
	case "rf":
		switch action {
		case "assign":
			id, parseErr := strconv.Atoi(values["selected"])
			if parseErr != nil || id < 0 || id > 19 {
				return desktop.Page{}, errors.New("select a learned code first")
			}
			name := strings.TrimSpace(values["binding_name"])
			if name == "" {
				return desktop.Page{}, errors.New("enter a new assignment name")
			}
			var entries []controllerapi.RFEntryView
			if err = p.client.call(ctx, "controller.rf.list", map[string]any{}, &entries); err != nil {
				return desktop.Page{}, err
			}
			var selected *controllerapi.RFEntryView
			for i := range entries {
				if int(entries[i].ID) == id {
					selected = &entries[i]
					break
				}
			}
			if selected == nil {
				return desktop.Page{}, errors.New("selected RF entry changed; refresh first")
			}
			if selected.ActionKind != native.RFActionNone {
				return desktop.Page{}, errors.New("this code already has a board assignment; resolve it before adding a host assignment")
			}
			label := values["control_action"]
			start := strings.LastIndex(label, " [")
			if start < 0 || !strings.HasSuffix(label, "]") {
				return desktop.Page{}, errors.New("select a catalog channel action")
			}
			actionID := label[start+2 : len(label)-1]
			var catalog struct {
				Bindings []appconfig.Automation `json:"bindings"`
			}
			if err = p.client.call(ctx, "controller.rf.catalog", map[string]any{}, &catalog); err != nil {
				return desktop.Page{}, err
			}
			for _, binding := range catalog.Bindings {
				if binding.Name == name {
					return desktop.Page{}, errors.New("an assignment already uses this name; choose a unique name")
				}
			}
			code := selected.Code
			binding := appconfig.Automation{Name: name, Enabled: true, Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", RFCode: &code, RFBits: selected.Bits, RFProtocol: selected.Protocol, Gesture: values["gesture"]}, Actions: []appconfig.AutomationAction{{Type: "control", ActionID: actionID}}}
			err = call("controller.rf.binding.put", map[string]any{"binding": binding})
		case "save":
			var seconds int
			seconds, err = number("timeout_seconds", 1, 120)
			if err == nil {
				err = call("controller.rf.preferences.set", appconfig.RFLearningPreferences{Timed: values["timed"] == "true", TimeoutSeconds: seconds, Continuous: values["continuous"] == "true"})
			}
		case "learn":
			err = call("controller.rf.learn.start", map[string]any{})
		case "cancel":
			err = call("controller.rf.learn.cancel", map[string]any{})
		default:
			err = errors.New("unknown RF action")
		}
	case "programs":
		switch action {
		case "mode":
			err = call("controller.program_state.set", map[string]string{"owner": "native-desktop", "mode": values["mode"]})
		case "play":
			if values["selected"] == "" {
				return desktop.Page{}, errors.New("select an effect first")
			}
			err = execute(shell.Join([]string{"effect", "play", values["selected"]}))
		default:
			err = errors.New("unknown program action")
		}
	case "buzzer":
		switch action {
		case "tone":
			var hz, ms int
			hz, err = number("frequency", 20, 20000)
			if err == nil {
				ms, err = number("duration", 1, 65535)
			}
			if err == nil {
				err = execute(fmt.Sprintf("buzzer %d %d", hz, ms))
			}
		case "melody":
			var repeats int
			repeats, err = number("repeats", 1, 255)
			if err == nil {
				err = execute(shell.Join([]string{"melody", "play", values["melody"], strconv.Itoa(repeats)}))
			}
		case "stop":
			err = execute("melody stop")
		default:
			err = errors.New("unknown buzzer action")
		}
	case "strip":
		switch action {
		case "order":
			err = execute(shell.Join([]string{"strip", "order", values["order"]}))
		case "stop":
			err = execute("strip stop")
		case "fill":
			rgb := [3]int{}
			for i, key := range []string{"red", "green", "blue"} {
				rgb[i], err = number(key, 0, 255)
				if err != nil {
					break
				}
			}
			if err == nil {
				err = execute(fmt.Sprintf("strip fill %d %d %d", rgb[0], rgb[1], rgb[2]))
			}
		case "rainbow":
			var pixels, fps int
			pixels, err = number("pixels", 1, native.StripMaximumPixels)
			if err == nil {
				fps, err = number("fps", 1, appconfig.StripMaximumFPS)
			}
			if err == nil {
				err = execute(fmt.Sprintf("strip rainbow %d %d", pixels, fps))
			}
		default:
			err = errors.New("unknown strip action")
		}
	case "display":
		var seconds int
		seconds, err = number("duration", 1, 3600)
		if err == nil {
			err = execute(shell.Join([]string{"display", values["target"], "--duration", strconv.Itoa(seconds) + "s", "--repeat", values["repeat"], "--", values["text"]}))
		}
	default:
		err = errors.New("unknown page")
	}
	if err != nil {
		return desktop.Page{}, err
	}
	updated, err := p.load(ctx, key)
	if err != nil {
		return desktop.Page{}, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	return updated, nil
}
