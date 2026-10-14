package main

import (
	"context"
	"errors"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func remoteTUIBootstrap(ctx context.Context, snapshot func(context.Context) (control.Snapshot, error), catalog func(context.Context) (*shell.Engine, error)) (control.Snapshot, time.Time, *shell.Engine, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type snapshotResult struct {
		value control.Snapshot
		at    time.Time
		err   error
	}
	type catalogResult struct {
		value *shell.Engine
		err   error
	}
	snapshots, catalogs := make(chan snapshotResult, 1), make(chan catalogResult, 1)
	go func() { value, err := snapshot(ctx); snapshots <- snapshotResult{value, time.Now(), err} }()
	go func() { value, err := catalog(ctx); catalogs <- catalogResult{value, err} }()
	var initial snapshotResult
	select {
	case initial = <-snapshots:
	case <-ctx.Done():
		return control.Snapshot{}, time.Time{}, nil, ctx.Err()
	}
	var commands catalogResult
	select {
	case commands = <-catalogs:
	case <-ctx.Done():
		return initial.value, initial.at, nil, errors.Join(initial.err, ctx.Err())
	}
	return initial.value, initial.at, commands.value, errors.Join(initial.err, commands.err)
}

func remoteTUIStartupPreferences(ctx context.Context, client *remoteTUIIPC) (remoteUISettingsWire, bool, appconfig.RFConfig) {
	type uiResult struct {
		value remoteUISettingsWire
		err   error
	}
	type rfResult struct {
		value appconfig.RFConfig
		err   error
	}
	uis, rfs := make(chan uiResult, 1), make(chan rfResult, 1)
	go func() { value, err := client.UISettings(ctx); uis <- uiResult{value, err} }()
	go func() { value, err := client.RFPresentation(ctx); rfs <- rfResult{value, err} }()
	var ui uiResult
	select {
	case ui = <-uis:
	case <-ctx.Done():
		return remoteUISettingsWire{}, false, appconfig.DefaultRFConfig()
	}
	rf := appconfig.DefaultRFConfig()
	select {
	case result := <-rfs:
		if result.err == nil {
			rf = result.value
		}
	case <-ctx.Done():
	}
	return ui.value, ui.err == nil, rf
}
