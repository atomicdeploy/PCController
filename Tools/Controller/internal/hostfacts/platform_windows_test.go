//go:build windows

package hostfacts

import (
	"context"
	"testing"
)

func TestNativeSystemProfileReturnsBoundedRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), MaxQueryTimeout)
	defer cancel()
	spec := queryCatalog["system"]
	rows, truncated, err := (nativeBackend{}).query(ctx, spec)
	if err != nil {
		t.Fatalf("native WMI system profile: %v", err)
	}
	if truncated || len(rows) != 1 || len(rows) > spec.MaxRows {
		t.Fatalf("rows=%#v truncated=%t max=%d", rows, truncated, spec.MaxRows)
	}
	if caption, ok := rows[0]["Caption"].(string); !ok || caption == "" {
		t.Fatalf("system caption=%#v", rows[0]["Caption"])
	}
	for column := range rows[0] {
		allowed := false
		for _, candidate := range spec.Columns {
			if column == candidate {
				allowed = true
				break
			}
		}
		if !allowed {
			t.Fatalf("native backend returned unlisted column %q", column)
		}
	}
}
