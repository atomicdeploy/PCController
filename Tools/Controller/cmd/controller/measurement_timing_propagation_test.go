package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestConfigurationTimingEventReachesIndependentClientCursors(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publishConfigurationChanges(ctx, store, runtime)
	afterID := client.LatestEventID()
	if _, err := store.Update(func(value *appconfig.Config) error {
		value.UI.StatusIntervalMS = 300
		value.UI.MeasurementFreshnessMS = 1600
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for clientIndex := 1; clientIndex <= 2; clientIndex++ {
		wait, stop := context.WithTimeout(context.Background(), time.Second)
		event, eventErr := client.NextEventStream(wait, afterID, "", "activity")
		stop()
		if eventErr != nil || event.Kind != "config" || event.Action != "config.changed" ||
			event.Metadata["status_interval_ms"] != "300" ||
			event.Metadata["measurement_freshness_ms"] != "1600" {
			t.Fatalf("client %d event=%#v error=%v", clientIndex, event, eventErr)
		}
	}
}
