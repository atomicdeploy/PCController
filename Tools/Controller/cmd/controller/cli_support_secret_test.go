package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestConfigShowAndSecretStatusRedactPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	value := appconfig.Defaults()
	value.IPC.AuthToken = "plaintext-config-token-012345"
	value.Integrations.OutboundWebhooks = []appconfig.Webhook{{
		Name: "test", URL: "https://example.test/hook", Method: "POST",
		Headers:       map[string]string{"Authorization": "Bearer plaintext-header"},
		SigningSecret: "plaintext-signing-secret",
	}}
	if err := appconfig.Write(path, value); err != nil {
		t.Fatal(err)
	}
	store, err := appconfig.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"show"}, {"secrets", "status"}} {
		var output bytes.Buffer
		if err := runConfigWithInput(args, strings.NewReader(""), &output, store); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"plaintext-config-token-012345", "Bearer plaintext-header", "plaintext-signing-secret",
		} {
			if strings.Contains(output.String(), forbidden) {
				t.Fatalf("%v leaked %q: %s", args, forbidden, output.String())
			}
		}
	}
}

func TestConfigSecretSetRejectsCommandLineValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := appconfig.Write(path, appconfig.Defaults()); err != nil {
		t.Fatal(err)
	}
	store, err := appconfig.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runConfigWithInput(
		[]string{"secrets", "set", "os:test", "visible-secret"},
		strings.NewReader(""), &output, store,
	)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("visible command-line value was accepted: %v", err)
	}
}
