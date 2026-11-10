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
	id := f.String("id", "", "Correlated OSD identifier")
	title := f.String("title", "PCController", "Overlay heading")
	text := f.String("text", "", "Overlay text")
	icon := f.String("icon", "app", "Semantic icon: app, info, success, warning, error, door, hot, relay, motion, radio")
	severity := f.String("severity", "info", "Semantic color: info, success, warning or error")
	duration := f.Duration("duration", 5*time.Second, "Display duration, including transitions (500ms..2m)")
	position := f.String("position", "bottom-center", "bottom-center, top-center, bottom-right or top-right")
	theme := f.String("theme", "system", "system, dark or light")
	reduced := f.Bool("reduce-motion", false, "Disable transitions and automatic scrolling")
	var actions osdActions
	f.Var(&actions, "action", "Button label=URI; repeat for multiple buttons")
	if err := f.Parse(args[1:]); err != nil {
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
	request := hostui.OSDRequest{ID: *id, Title: *title, Text: *text, Icon: *icon, Severity: *severity, DurationMS: int(duration.Milliseconds()), Position: *position, Theme: *theme, ReduceMotion: *reduced, Actions: actions}
	if verb == "show" {
		if *duration < 500*time.Millisecond || *duration > 2*time.Minute {
			return errors.New("OSD duration must be 500ms..2m")
		}
		var err error
		request, err = hostui.ValidateOSD(request)
		if err != nil {
			return err
		}
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
		if err = json.NewEncoder(stdout).Encode(status); err != nil {
			return err
		}
		// No board runtime, serial ownership, LAN discovery or main GUI is created.
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for hostui.DesktopOSDStatus().Active {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
		return nil
	}
	params := any(map[string]string{"id": *id})
	if verb == "show" {
		params = request
	}
	if verb == "status" {
		params = map[string]any{}
	}
	raw, _ := json.Marshal(params)
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
	callCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	response, err := ipcjson.Call(callCtx, *address, ipcjson.Request{Method: "controller.osd." + verb, Params: raw, Auth: token})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(response.Result, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(encoded))
	return err
}
