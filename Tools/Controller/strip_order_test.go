package controller

import (
	"context"
	"strings"
	"testing"
)

func TestStripOrderLibraryStateAndHostHooks(t *testing.T) {
	client := New(Options{})
	t.Cleanup(func() { _ = client.Shutdown() })
	ctx := context.Background()
	if result, err := client.Execute(ctx, "strip order RGB"); err != nil || !strings.Contains(result, `"color_order":"RGB"`) {
		t.Fatalf("library save: %q %v", result, err)
	}
	if result, err := client.Execute(ctx, "strip order"); err != nil || !strings.Contains(result, `"color_order":"RGB"`) {
		t.Fatalf("library readback: %q %v", result, err)
	}
	order := "BGR"
	client.ConfigureStripColorOrder(func() (string, string) { return order, "GRB" }, func(value string) error { order = value; return nil })
	client.ApplyHostOptions(Options{}) // Watched reload must retain host hooks.
	if result, err := client.Execute(ctx, "strip order RBG"); err != nil || order != "RBG" || !strings.Contains(result, `"firmware_order":"GRB"`) {
		t.Fatalf("embedded host save/readback: %q %v, stored=%s", result, err, order)
	}
}
