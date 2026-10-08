package main

import (
	"io"
	"path/filepath"
	"slices"
	"testing"
)

func TestServiceRuntimeOptionsPinAbsoluteConfigDataAndLoopback(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.json")
	data := filepath.Join(t.TempDir(), "service-data")
	options, err := parseServiceRuntimeOptions("install", []string{"--data-dir", data, "--listen", "127.0.0.1:9187", "--start=false"}, config, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.ConfigPath != config || options.DataDir != data || options.Listen != "127.0.0.1:9187" || options.Start {
		t.Fatalf("unexpected service options: %+v", options)
	}
	args := serviceImageArguments(options)
	want := []string{"--config=" + config, "service", "run", "--data-dir=" + data, "--listen=127.0.0.1:9187"}
	if !slices.Equal(args, want) {
		t.Fatalf("service image args = %q, want %q", args, want)
	}
}

func TestServiceRuntimeOptionsRejectNonLoopbackListen(t *testing.T) {
	_, err := parseServiceRuntimeOptions("install", []string{"--listen", "0.0.0.0:8787"}, filepath.Join(t.TempDir(), "config.json"), io.Discard)
	if err == nil {
		t.Fatal("non-loopback service endpoint accepted")
	}
}
