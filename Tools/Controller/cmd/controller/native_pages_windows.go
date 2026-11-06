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
	controllerports "pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/desktop/desktop"
	"strconv"
	"strings"
	"sync"
)

type nativePages struct {
	client  *remoteTUIIPC
	preview bool
	events  *nativeEventBuffer
	rpc     func(context.Context, string, any, any) error
	rest    func(context.Context, string, string, []byte) ([]byte, error)
	rfCache *nativeRFCache
}

func (p nativePages) call(ctx context.Context, method string, params, result any) error {
	if p.rpc != nil {
		return p.rpc(ctx, method, params, result)
	}
	if p.client == nil {
		return errors.New("controller connection unavailable")
	}
	if method == "controller.snapshot" {
		if target, ok := result.(*control.Snapshot); ok {
			state, err := p.client.Snapshot(ctx)
			if err == nil {
				*target = state
			}
			return err
		}
	}
	return p.client.call(ctx, method, params, result)
}

func (p nativePages) command(ctx context.Context, command string) (string, error) {
	var result struct {
		Output string `json:"output"`
	}
	err := p.call(ctx, "controller.command.execute", map[string]string{"command": command}, &result)
	return result.Output, err
}

// The desktop consumes the existing owner event stream; opening a page never
// starts another serial reader or unbounded history allocation.
type nativeEventBuffer struct {
	mu       sync.Mutex
	rows     [][]string
	versions map[string]uint64
}

func (b *nativeEventBuffer) append(event control.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.rows) == 200 {
		copy(b.rows, b.rows[1:])
		b.rows = b.rows[:199]
	}
	b.rows = append(b.rows, []string{strconv.FormatUint(event.ID, 10), event.Time.Local().Format("15:04:05.000"), event.Kind, event.Text})
	if b.versions == nil {
		b.versions = map[string]uint64{}
	}
	b.versions["events"]++
	if strings.HasPrefix(event.Kind, "rf.") {
		b.versions["rf"]++
		if event.Kind != "rf.receive" {
			b.versions["rf-table"]++
		}
	}
	if strings.HasPrefix(event.Kind, "update.") || strings.HasPrefix(event.Kind, "artifact.") {
		b.versions["firmware"]++
		b.versions["updates"]++
	}
}

func (b *nativeEventBuffer) version(key string) uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.versions[key]
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
		err = p.call(ctx, "controller.snapshot", map[string]any{}, &state)
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
				var available []controllerports.Info
				err := p.call(ctx, "controller.ports", map[string]any{}, &available)
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
			if err := p.call(ctx, "controller.artifact.manifest", map[string]any{}, &manifest); err != nil {
				return page, err
			}
			if err := p.call(ctx, "controller.artifact.list", map[string]any{}, &inventory); err != nil {
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
			if err := p.call(ctx, "controller.ui.config", map[string]any{}, &cfg); err != nil {
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
		page.Columns = []string{"ID", "Name / code", "Bits / protocol", "Assignment", "Receiving", "Since opened"}
		catalog, err := p.loadNativeRFCatalog(ctx, state)
		if err != nil {
			return page, err
		}
		p.nativeRFReceiptRows(&page, catalog, state)
		for _, entry := range catalog.Entries {
			name := entry.Name
			if name == "" {
				name = entry.CodeDisplay
			}
			page.Rows = append(page.Rows, []string{strconv.Itoa(int(entry.ID)), name, fmt.Sprintf("%d / %d", entry.Bits, entry.Protocol), entry.BoardAssignment.Label, "○", "0"})
		}
		page.Fields = []desktop.Field{
			choice("timed", "Timed learning", strconv.FormatBool(catalog.Preferences.Timed), "false", "true"),
			field("timeout_seconds", "Timeout (seconds)", strconv.Itoa(catalog.Preferences.TimeoutSeconds)),
			choice("continuous", "Continue after learning", strconv.FormatBool(catalog.Preferences.Continuous), "false", "true"),
		}
		page.Actions = []desktop.PageAction{pageAction("save", "Save preferences", !p.preview)}
		page.Fields = append(page.Fields, choice("board_action", "Board action", "none", "none", "key", "menu", "relay", "side", "pwm"), field("board_target", "Board target", ""), choice("board_behavior", "Board behavior", "press", "press", "toggle", "momentary", "up", "down", "stop"))
		page.Actions = append(page.Actions, nativeSelectionAction("add-observed", "Save observed code", enabled), nativeSelectionAction("map-board", "Map on board", enabled), desktop.PageAction{ID: "remove", Label: "Remove learned code", Enabled: enabled, SelectionRequired: true, Confirm: true}, pageAction("refresh-codes", "Read board codes", enabled))
		if !p.preview {
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
				page.Actions = append(page.Actions, nativeSelectionAction("assign", "Assign selected", enabled))
			}
		}
		if catalog.Learning.Active {
			page.Actions = append(page.Actions, pageAction("cancel", "Stop learning", enabled))
		} else {
			page.Actions = append(page.Actions, pageAction("learn", "Start learning", enabled && state.Hello.Capabilities&native.CapabilityRF != 0))
		}
	case "programs":
		return p.loadEffectsPage(state), nil
	case "buzzer":
		page.Title = "Buzzer & melodies"
		var melodies []appconfig.Melody
		if !p.preview {
			if err := p.call(ctx, "controller.melodies.list", map[string]any{}, &melodies); err != nil {
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
			page.Fields = append(page.Fields, choice("melody", "Melody", names[0], names...), nativeBool("loop", "Loop until stopped", false), field("repeats", "Repeats", "1"))
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
			Running bool                     `json:"running"`
			Stats   control.StripStreamStats `json:"stats"`
		}
		order := "GRB"
		if !p.preview {
			status, err := p.command(ctx, "strip status")
			if err != nil {
				return page, err
			}
			if err := json.Unmarshal([]byte(status), &stream); err != nil {
				return page, fmt.Errorf("read strip state: %w", err)
			}
			var ui struct {
				StripColorOrder string `json:"strip_color_order"`
			}
			if err := p.call(ctx, "controller.ui.config", map[string]any{}, &ui); err != nil {
				return page, err
			}
			if ui.StripColorOrder != "" {
				order = ui.StripColorOrder
			}
		}
		page.Fields = []desktop.Field{choice("order", "Pixel order", order, "GRB", "RGB", "BRG", "BGR", "RBG", "GBR"), field("pixels", "Pixel count", strconv.Itoa(native.StripMaximumPixels)), field("fps", "Frame rate", "60"), field("red", "Red (0–255)", "255"), field("green", "Green (0–255)", "160"), field("blue", "Blue (0–255)", "40"), nativeNumber("brightness", "Brightness", 255, 0, 255)}
		page.Actions = []desktop.PageAction{pageAction("order", "Apply pixel order", !p.preview), pageAction("fill", "Fill color", enabled), pageAction("configure", "Configure pixel count", enabled), pageAction("clear", "Clear strip", enabled)}
		if stream.Running {
			page.Output = fmt.Sprintf("%d pixels · %d FPS target · %d acknowledged frames · %d failed frames", stream.Stats.Pixels, stream.Stats.RequestedFPS, stream.Stats.FramesAcked, stream.Stats.FramesFailed)
		}
		if stream.Running {
			page.Actions = append(page.Actions, pageAction("stop", "Stop stream", state.Connected))
		} else {
			page.Actions = append(page.Actions, pageAction("rainbow", "Start rainbow", enabled))
		}
	case "display":
		page.Title = "Front-panel display"
		page.Fields = []desktop.Field{choice("target", "Display", "segments", "segments", "lcd", "both"), field("text", "Text", ""), nativeNumber("duration_ms", "Visible duration (ms)", 5000, 80, 65535), nativeNumber("speed_ms", "Marquee step (ms)", 220, 80, 5000), choice("repeat", "Repeat", "once", "once", "loop", "interval"), nativeNumber("interval_ms", "Repeat interval (ms)", 30000, 1000, 255000), nativeBool("scroll", "Force marquee", false)}
		page.Actions = []desktop.PageAction{pageAction("show", "Show text", enabled), pageAction("clear", "Clear display", enabled)}
	default:
		return p.loadWorkspacePage(ctx, key, state)
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
			if strings.HasPrefix(f.Key, "board_") {
				f.Section = "Board mapping"
			}
			if f.Key == "binding_name" || f.Key == "gesture" || f.Key == "control_action" {
				f.Section = "Selected code assignment"
			}
		case "buzzer":
			f.Section = "Tone test"
			if f.Key == "melody" || f.Key == "repeats" || f.Key == "loop" {
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
	for i := range page.Actions {
		a := &page.Actions[i]
		switch key {
		case "rf":
			switch a.ID {
			case "save", "learn", "cancel":
				a.Section = "Learning preferences"
			case "assign":
				a.Section = "Selected code assignment"
			default:
				a.Section = "Board mapping"
			}
		case "buzzer":
			switch a.ID {
			case "tone":
				a.Section = "Tone test"
			case "melody":
				a.Section = "Melody playback"
			}
		case "strip":
			switch a.ID {
			case "order", "configure", "rainbow":
				a.Section = "Pixel configuration"
			case "fill":
				a.Section = "Solid color"
			}
		}
	}
}

func (p nativePages) submit(ctx context.Context, key, action string, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot change controller state")
	}
	call := func(method string, params any) error {
		var result any
		return p.call(ctx, method, params, &result)
	}
	execute := func(command string) error { _, err := p.command(ctx, command); return err }
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
			if err = p.call(ctx, "controller.artifact.list", map[string]string{"kind": "firmware"}, &inventory); err != nil {
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
		if action == "map-board" || action == "remove" || action == "add-observed" || action == "refresh-codes" {
			return p.submitNativeRFExtra(ctx, action, values)
		}
		switch action {
		case "assign":
			id, parseErr := strconv.Atoi(values["selected"])
			observed := strings.HasPrefix(values["selected"], "observed:")
			if !observed && (parseErr != nil || id < 0 || id > 19) {
				return desktop.Page{}, errors.New("select a learned code first")
			}
			name := strings.TrimSpace(values["binding_name"])
			if name == "" {
				return desktop.Page{}, errors.New("enter a new assignment name")
			}
			var entries []controllerapi.RFEntryView
			if observed {
				var catalog nativeRFCatalog
				if err = p.call(ctx, "controller.rf.catalog", map[string]any{}, &catalog); err != nil {
					return desktop.Page{}, err
				}
				for _, receipt := range catalog.Receptions {
					if "observed:"+nativeRFIdentity(receipt.Code, receipt.Bits, receipt.Protocol) == values["selected"] {
						entries = append(entries, controllerapi.RFEntryView{ID: 0, Code: receipt.Code, Bits: receipt.Bits, Protocol: receipt.Protocol})
						id = 0
						break
					}
				}
			} else if err = p.call(ctx, "controller.rf.list", map[string]any{}, &entries); err != nil {
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
			if err = p.call(ctx, "controller.rf.catalog", map[string]any{}, &catalog); err != nil {
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
		return p.submitEffectsPage(ctx, action, values)
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
			if values["loop"] == "true" {
				repeats, err = 0, nil
			}
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
		case "configure":
			var pixels int
			pixels, err = number("pixels", 1, native.StripMaximumPixels)
			if err == nil {
				err = execute(fmt.Sprintf("strip config %d", pixels))
			}
		case "clear":
			err = execute("strip clear")
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
				var brightness int
				brightness, err = number("brightness", 0, 255)
				if err == nil {
					err = execute(fmt.Sprintf("strip fill %d %d %d %d", rgb[0], rgb[1], rgb[2], brightness))
				}
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
		if action != "show" && action != "clear" {
			return desktop.Page{}, errors.New("unknown display action")
		}
		duration, parseErr := number("duration_ms", 80, 65535)
		if parseErr != nil {
			return desktop.Page{}, parseErr
		}
		speed, parseErr := number("speed_ms", 80, 5000)
		if parseErr != nil {
			return desktop.Page{}, parseErr
		}
		interval, parseErr := number("interval_ms", 1000, 255000)
		if parseErr != nil {
			return desktop.Page{}, parseErr
		}
		text := values["text"]
		if action == "clear" {
			text = ""
		}
		err = call("controller.display.send", control.DisplayRequest{Target: values["target"], Text: text, DurationMS: duration, SpeedMS: speed, Repeat: control.DisplayRepeat(values["repeat"]), IntervalMS: interval, Scroll: values["scroll"] == "true"})
	default:
		return p.submitWorkspacePage(ctx, key, action, values)
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
