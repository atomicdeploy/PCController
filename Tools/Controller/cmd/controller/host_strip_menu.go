package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

// These are staged host-session choices, not claimed board readback. Only
// Apply/Play/Stop/Off dispatch through the shared, ACK-checked command engine.
type hostStripMenuActions struct {
	mu                 sync.Mutex
	config             func() appconfig.Config
	snapshot           func() control.Snapshot
	state              func() control.OutputStreamState
	execute            func(context.Context, string) (string, error)
	color              string
	brightness, pixels int
	effect             string
}

func newHostStripMenuActions(store *appconfig.Store, runtime *control.Runtime, engine *shell.Engine) *hostStripMenuActions {
	return &hostStripMenuActions{config: store.Current, snapshot: runtime.Snapshot,
		state: runtime.EnsureOutputScheduler().State, execute: engine.Execute,
		color: "ffffff", brightness: 100, pixels: native.StripMaximumPixels}
}

func stripHostMenuOptions(effects []appconfig.StripEffect) []appconfig.HostMenuOption {
	options := make([]appconfig.HostMenuOption, 0, len(effects))
	for _, effect := range effects {
		// The physical LCD catalog is printable ASCII, even if the full UI name
		// is localized. Stable IDs, not shortened labels, select effects.
		label := strings.Map(func(r rune) rune {
			if r < 32 || r > 126 {
				return '?'
			}
			return r
		}, effect.Name)
		if label == "" {
			label = effect.ID
		}
		if len(label) > 16 {
			label = label[:16]
		}
		options = append(options, appconfig.HostMenuOption{Label: label, Value: effect.ID})
	}
	return options
}

func (a *hostStripMenuActions) selectedEffect() string {
	effects := a.config().StripEffects
	for _, effect := range effects {
		if effect.ID == a.effect {
			return a.effect
		}
	}
	a.effect = ""
	if len(effects) > 0 {
		a.effect = effects[0].ID
	}
	return a.effect
}

func (a *hostStripMenuActions) Read(action string) (string, bool, error) {
	if !strings.HasPrefix(action, "host.strip.") {
		return "", false, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case "host.strip.status":
		snapshot := a.snapshot()
		if !snapshot.Connected {
			return "Offline", true, nil
		}
		if snapshot.Hello.Capabilities&native.CapabilityAddressableLED == 0 {
			return "Unsupported", true, nil
		}
		state := a.state()
		if state.StripID != 0 {
			return "RUN " + state.StripName, true, nil
		}
		return "Stream idle", true, nil // Idle does not mean the LEDs are off.
	case "host.strip.color":
		return a.color, true, nil
	case "host.strip.brightness":
		return strconv.Itoa(a.brightness), true, nil
	case "host.strip.pixels":
		return strconv.Itoa(a.pixels), true, nil
	case "host.strip.effect":
		return a.selectedEffect(), true, nil
	default:
		return "", true, fmt.Errorf("unknown strip menu read %q", action)
	}
}

func (a *hostStripMenuActions) Write(action, value string) (string, bool, error) {
	if !strings.HasPrefix(action, "host.strip.") {
		return "", false, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case "host.strip.color":
		rgb, err := hex.DecodeString(value)
		if err != nil || len(rgb) != 3 {
			return "", true, errors.New("solid color must be six RGB hex digits")
		}
		a.color = value
	case "host.strip.brightness", "host.strip.pixels":
		min, max := 0, 100
		if action == "host.strip.pixels" {
			min, max = 1, native.StripMaximumPixels
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < min || n > max {
			return "", true, fmt.Errorf("value must be %d..%d", min, max)
		}
		if action == "host.strip.pixels" {
			a.pixels = n
		} else {
			a.brightness = n
		}
	case "host.strip.effect":
		found := false
		for _, effect := range a.config().StripEffects {
			if effect.ID == value {
				found = true
				break
			}
		}
		if !found {
			return "", true, errors.New("strip effect no longer exists")
		}
		a.effect = value
	default:
		return "", true, fmt.Errorf("unknown strip menu setting %q", action)
	}
	return "Selected", true, nil
}

func (a *hostStripMenuActions) Execute(ctx context.Context, action string) (string, bool, error) {
	if !strings.HasPrefix(action, "host.strip.") {
		return "", false, nil
	}
	// Stop is useful even after a disconnect; it cancels the host stream only.
	if action == "host.strip.stop" {
		output, err := a.execute(ctx, "strip stop")
		return output, true, err
	}
	snapshot := a.snapshot()
	if !snapshot.Connected {
		return "", true, errors.New("device is offline")
	}
	if snapshot.Hello.Capabilities&native.CapabilityAddressableLED == 0 {
		return "", true, errors.New("board does not advertise LED strip support")
	}
	a.mu.Lock()
	var command string
	switch action {
	case "host.strip.apply":
		rgb, _ := hex.DecodeString(a.color)
		for i := range rgb {
			rgb[i] = byte((int(rgb[i])*a.brightness + 50) / 100)
		}
		// Software scaling works on fixed-brightness AVR too. The shared frame
		// path applies configured wire color order and serializes stream handoff.
		command = "strip frame " + strings.Repeat(hex.EncodeToString(rgb), a.pixels)
	case "host.strip.play":
		if a.selectedEffect() != "" {
			command = shell.Join([]string{"effect", "play", "effect:" + a.effect, strconv.Itoa(a.pixels)})
		}
	case "host.strip.off":
		command = "strip clear"
	}
	a.mu.Unlock()
	if command == "" {
		return "", true, errors.New("no strip action or effect selected")
	}
	output, err := a.execute(ctx, command)
	return output, true, err
}
