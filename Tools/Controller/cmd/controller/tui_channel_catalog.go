package main

import (
	"context"

	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/tui"
)

// Both the local TUI and remote attachments use the primary's existing RPC
// contract. No second config store or serial connection is created here.
func newTUIChannelCatalog(call func(context.Context, string, any, any) error) tui.ChannelCatalogSource {
	return tui.ChannelCatalogSource{
		SetSemantic: func(ctx context.Context, key string, value uint16, identity, revision, host string, generation uint64) error {
			var result map[string]any
			return call(ctx, "controller.channel.set", map[string]any{"key": key, "value": value, "board_identity": identity, "expected_revision": revision, "expected_host_instance_id": host, "expected_connection_generation": generation}, &result)
		},
		Read: func(ctx context.Context) (ipcjson.PeripheralSettings, error) {
			var value ipcjson.PeripheralSettings
			err := call(ctx, "controller.peripherals.get", map[string]any{}, &value)
			return value, err
		},
		Rename: func(ctx context.Context, key, name, revision string) (ipcjson.PresentationUpdateResult, error) {
			var value ipcjson.PresentationUpdateResult
			err := call(ctx, "controller.peripheral.presentation.update", map[string]any{
				"key": key, "name": name, "expected_revision": revision,
			}, &value)
			return value, err
		},
	}
}
