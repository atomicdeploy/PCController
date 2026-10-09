package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestNetworkPeerAddUsesSecretReferenceAndCanBeRemoved(t *testing.T) {
	t.Setenv("TEST_EDGE_TOKEN", "0123456789abcdefghijklmn")
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runNetwork([]string{
		"peer-add", "--name", "edge", "--url", "ws://192.168.1.2:8787/ipc",
		"--secret-ref", "env:TEST_EDGE_TOKEN",
	}, &output, &output, store)
	if err != nil {
		t.Fatal(err)
	}
	peers := store.Current().Integrations.WebSocketClients
	if len(peers) != 1 || peers[0].AuthToken != "" || peers[0].AuthTokenRef != "env:TEST_EDGE_TOKEN" {
		t.Fatalf("unexpected persisted peer: %#v", peers)
	}
	output.Reset()
	if err := runNetwork([]string{"status"}, &output, &output, store); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("0123456789abcdefghijklmn")) {
		t.Fatal("network status leaked peer bearer token")
	}
	if err := runNetwork([]string{"peer-remove", "--name", "EDGE"}, &output, &output, store); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Current().Integrations.WebSocketClients); got != 0 {
		t.Fatalf("peer count after removal=%d want 0", got)
	}
}
