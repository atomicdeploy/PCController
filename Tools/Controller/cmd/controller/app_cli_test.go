package main

import (
	"io"
	"testing"
	"time"
)

func TestParseSurfaceLaunchCLIIsBoundedAndBridgeAware(t *testing.T) {
	options, err := parseSurfaceLaunchCLI([]string{
		"tui", "--mode", "focus", "--target", "tui:live", "--page", "updates",
		"--peer", "server", "--idempotency-key", "operator-42", "--timeout", "10s",
	}, io.Discard, "127.0.0.1:8787")
	if err != nil {
		t.Fatal(err)
	}
	if options.Request.Surface != "tui" || options.Request.Mode != "focus" ||
		options.Request.Target != "tui:live" || options.Request.Page != "updates" ||
		options.Peer != "server" || options.Request.IdempotencyKey != "operator-42" ||
		options.Timeout != 10*time.Second {
		t.Fatalf("options=%#v", options)
	}
	for _, args := range [][]string{
		{"tui", "cmd.exe", "/c", "whoami"},
		{"webui", "--peer", "server", "--addr", "server:8787"},
		{"webui", "--peer", "server", "--token-ref", "os:secret"},
		{"webui", "--timeout", "2m"},
	} {
		if _, err := parseSurfaceLaunchCLI(args, io.Discard, "127.0.0.1:8787"); err == nil {
			t.Fatalf("unsafe CLI accepted: %#v", args)
		}
	}
}
