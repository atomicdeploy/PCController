package ownedstorage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMarkerUsesUnversionedProductIdentityAndChecksOwner(t *testing.T) {
	root := filepath.Join(t.TempDir(), "host-data")
	if err := EnsureFor(root, "test-owner"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, MarkerName))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	var format string
	if err := json.Unmarshal(fields["format"], &format); err != nil {
		t.Fatal(err)
	}
	if format != "pccontroller-host-data-owner" {
		t.Fatalf("marker identity = %q", format)
	}
	fields["future_optional"] = json.RawMessage(`true`)
	content, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, MarkerName), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFor(root, "test-owner"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFor(root, "different-owner"); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("different owner verification = %v", err)
	}
	fields["format"] = json.RawMessage(`"pccontroller-host-data-owner/v1"`)
	content, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, MarkerName), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFor(root, "test-owner"); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("generation-branded marker was accepted: %v", err)
	}
}
