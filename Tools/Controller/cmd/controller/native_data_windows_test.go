//go:build windows && !no_native_gui

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeDataWorkspacePinsOwnerRouteAndProjectsRecords(t *testing.T) {
	for _, path := range []string{"http://other/v1/status", "//other/v1/status", "../secret", "v1/%2e%2e/secret", "v1/status#fragment", "v1\\secret"} {
		if _, err := nativeDataRoute(path); err == nil {
			t.Fatalf("unsafe path accepted: %s", path)
		}
	}
	calls := 0
	p := nativePages{rest: func(_ context.Context, method, path string, body []byte) ([]byte, error) {
		calls++
		if method != "GET" || path != "/api/integrations/datahub/v1/records?limit=3" || len(body) != 0 {
			t.Fatalf("unexpected request: %s %s", method, path)
		}
		return []byte(`{"records":[{"id":1,"name":"Window"},{"id":2,"name":"Garden"}]}`), nil
	}}
	v := map[string]string{"path": "v1/records?limit=3", "method": "GET"}
	page, err := p.submitNativeData(context.Background(), "request", v)
	if err != nil || len(page.Rows) != 2 || len(page.Columns) != 2 || page.Rows[0][1] != "Window" {
		t.Fatalf("record projection: %+v %v", page, err)
	}
	v["method"] = "POST"
	v["body"] = "invalid"
	if _, err := p.submitNativeData(context.Background(), "request", v); err == nil || calls != 1 {
		t.Fatal("invalid draft reached integration")
	}
	v["export_path"] = filepath.Join(t.TempDir(), "records.json")
	if _, err := p.submitNativeData(context.Background(), "export", v); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(v["export_path"])
	if err != nil || len(original) == 0 {
		t.Fatal("export missing")
	}
	if _, err := p.submitNativeData(context.Background(), "export", v); err == nil {
		t.Fatal("export overwrote existing data")
	}
}

func TestNativeToolOperationsHaveDedicatedSectionsAndSafePreview(t *testing.T) {
	for _, key := range []string{"safety", "i2c", "network", "history", "host-tools", "local-device", "firmware-tools", "status-lighting"} {
		title, ops := nativeToolOperations(key)
		if title == "" || len(ops) == 0 {
			t.Fatalf("missing activity %s", key)
		}
		seen := map[string]bool{}
		for _, op := range ops {
			if seen[op.id] || op.id == "" || op.section == "" || op.label == "" || (op.method == "") == (op.command == nil) {
				t.Fatalf("invalid %s operation %+v", key, op)
			}
			seen[op.id] = true
		}
	}
}
