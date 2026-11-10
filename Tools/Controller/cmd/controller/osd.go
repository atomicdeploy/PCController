package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ipcjson"
)

type osdActions []hostui.OSDAction
type osdButtons struct{ actions *osdActions }

func (b osdButtons) String() string { return "id=label (repeatable)" }
func (b osdButtons) Set(s string) error {
	id, label, ok := strings.Cut(s, "=")
	if !ok {
		return errors.New("OSD --button requires id=label")
	}
	*b.actions = append(*b.actions, hostui.OSDAction{ID: id, Label: label})
	return nil
}

func (a *osdActions) String() string { return "label=URI (repeatable)" }
func (a *osdActions) Set(s string) error {
	label, uri, ok := strings.Cut(s, "=")
	if !ok {
		return errors.New("OSD --action requires label=URI")
	}
	*a = append(*a, hostui.OSDAction{Label: label, URI: uri})
	return nil
}

func runOSD(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: osd show|status|dismiss [--addr host:port] [--standalone]")
	}
	verb := args[0]
	if verb != "show" && verb != "status" && verb != "dismiss" {
		return errors.New("OSD command must be show, status or dismiss")
	}
	f := flag.NewFlagSet("osd "+verb, flag.ContinueOnError)
	f.SetOutput(stderr)
	address := f.String("addr", "", "Existing host IPC address; default verifies the current per-user host")
	standalone := f.Bool("standalone", false, "Show without a host or board and wait until the OSD expires")
	wait := f.Bool("wait", false, "Wait for expiry, dismissal or a button; print the final JSON result")
	id := f.String("id", "", "Correlated OSD identifier")
	title := f.String("title", "", "Overlay heading; default uses host branding")
	text := f.String("text", "", "Overlay text")
	icon := f.String("icon", "app", "Semantic icon: app, info, success, warning, error, door, hot, relay, motion, radio")
	severity := f.String("severity", "info", "Semantic color: info, success, warning or error")
	duration := f.Duration("duration", 5*time.Second, "Display duration, including transitions (500ms..2m)")
	position := f.String("position", "", "top/bottom-left/center/right or center; default bottom-center")
	behavior := f.String("behavior", "", "replace (default) or stack")
	background := f.String("background", "", "current (default), transparent, acrylic or blur")
	theme := f.String("theme", "system", "system, dark or light")
	reduced := f.Bool("reduce-motion", false, "Disable transitions and automatic scrolling")
	var actions osdActions
	f.Var(&actions, "action", "Button label=URI; repeat for multiple buttons")
	f.Var(osdButtons{actions: &actions}, "button", "Script choice id=label, with no URI launch; repeat for multiple choices")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() > 0 {
		if verb != "show" || *text != "" {
			return errors.New("unexpected OSD arguments")
		}
		*text = strings.Join(f.Args(), " ")
	}
	ctx, cancel := signalContext()
	defer cancel()
	if *wait && verb != "show" {
		return errors.New("--wait supports osd show only")
	}
	request := hostui.OSDRequest{ID: *id, Title: *title, Text: *text, Icon: *icon, Severity: *severity, DurationMS: int(duration.Milliseconds()), Position: *position, Theme: *theme, Behavior: *behavior, Background: *background, ReduceMotion: *reduced, Actions: actions}
	if verb == "show" {
		if *duration < 500*time.Millisecond || *duration > 2*time.Minute {
			return errors.New("OSD duration must be 500ms..2m")
		}
		var err error
		request, err = hostui.ValidateOSD(request)
		if err != nil {
			return err
		}
		// Validate before contacting a host, but preserve unspecified preferences.
		request.Position, request.Behavior, request.Background = *position, *behavior, *background
	}
	if *standalone {
		if verb != "show" {
			return errors.New("--standalone supports osd show only")
		}
		defer hostui.CloseDesktopOSD()
		status, err := hostui.ShowOSD(ctx, request)
		if err != nil {
			return err
		}
		if !*wait {
			if err = json.NewEncoder(stdout).Encode(status); err != nil {
				return err
			}
		}
		// No board runtime, serial ownership, LAN discovery or main GUI is created.
		result, err := waitForOSD(ctx, status, func(context.Context) (hostui.OSDStatus, error) { return hostui.DesktopOSDResult(status.ID) })
		if err != nil {
			return err
		}
		if *wait {
			return writeOSDResult(stdout, result)
		}
		return nil
	}
	params := any(map[string]string{"id": *id})
	if verb == "show" {
		params = request
	}
	token := os.Getenv("PCCONTROLLER_IPC_TOKEN")
	if *address == "" {
		paths, err := defaultHostInstancePaths()
		if err != nil {
			return err
		}
		resolveCtx, resolveCancel := context.WithTimeout(ctx, time.Second)
		record, err := resolveHostInstance(resolveCtx, paths)
		resolveCancel()
		if err != nil {
			return fmt.Errorf("no verified local host; use --addr or --standalone: %w", err)
		}
		endpoint := recordPrimaryEndpoint(record)
		*address = endpoint.Listen
		if token == "" {
			token = endpoint.AuthToken
		}
	}
	call := func(callParent context.Context, method string, value any) (hostui.OSDStatus, error) {
		callCtx, stop := context.WithTimeout(callParent, 5*time.Second)
		defer stop()
		raw, _ := json.Marshal(value)
		response, err := ipcjson.Call(callCtx, *address, ipcjson.Request{Method: "controller.osd." + method, Params: raw, Auth: token})
		if err != nil {
			return hostui.OSDStatus{}, err
		}
		encoded, err := json.Marshal(response.Result)
		if err != nil {
			return hostui.OSDStatus{}, err
		}
		var status hostui.OSDStatus
		err = json.Unmarshal(encoded, &status)
		return status, err
	}
	status, err := call(ctx, verb, params)
	if err != nil {
		return err
	}
	if *wait {
		waitCtx, stop := context.WithTimeout(ctx, *duration+5*time.Second)
		defer stop()
		status, err = waitForOSD(waitCtx, status, func(c context.Context) (hostui.OSDStatus, error) {
			return call(c, "status", map[string]string{"id": status.ID})
		})
		if err != nil {
			return err
		}
	}
	return writeOSDResult(stdout, status)
}

func writeOSDResult(w io.Writer, status hostui.OSDStatus) error {
	if err := json.NewEncoder(w).Encode(status); err != nil {
		return err
	}
	if status.Dismissal == "failed" {
		return fmt.Errorf("OSD presentation failed: %s", status.LastError)
	}
	return nil
}

func waitForOSD(ctx context.Context, initial hostui.OSDStatus, read func(context.Context) (hostui.OSDStatus, error)) (hostui.OSDStatus, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return hostui.OSDStatus{}, ctx.Err()
		case <-ticker.C:
			result, err := read(ctx)
			if err != nil {
				return result, err
			}
			if result.ID != initial.ID || result.Accepted != initial.Accepted {
				initial.Active = false
				initial.Dismissal = "replaced"
				return initial, nil
			}
			if !result.Active {
				return result, nil
			}
		}
	}
}
