package hostfacts

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBackend struct {
	calls     atomic.Int32
	queryFunc func(context.Context, querySpec) ([]map[string]any, bool, error)
}

func (backend *fakeBackend) query(
	ctx context.Context,
	spec querySpec,
) ([]map[string]any, bool, error) {
	backend.calls.Add(1)
	return backend.queryFunc(ctx, spec)
}

func TestCatalogIsStrictAndOmitsDeviceSecrets(t *testing.T) {
	catalog := Catalog()
	if len(catalog) != 5 {
		t.Fatalf("catalog=%#v", catalog)
	}
	for index, descriptor := range catalog {
		if index > 0 && catalog[index-1].Profile >= descriptor.Profile {
			t.Fatalf("catalog is not sorted: %#v", catalog)
		}
		if descriptor.MaxRows < 1 || descriptor.MaxRows > maxGlobalRows {
			t.Fatalf("profile %s row bound=%d", descriptor.Profile, descriptor.MaxRows)
		}
		for _, column := range descriptor.Columns {
			if sensitiveColumn(column) {
				t.Fatalf("profile %s exposes sensitive column %q", descriptor.Profile, column)
			}
		}
	}
}

func TestProviderRejectsCallerSuppliedQuery(t *testing.T) {
	backend := &fakeBackend{queryFunc: func(context.Context, querySpec) ([]map[string]any, bool, error) {
		t.Fatal("backend called for unlisted profile")
		return nil, false, nil
	}}
	provider := newCachedProvider(backend)
	_, err := provider.Query(context.Background(), "SELECT * FROM Win32_Process")
	if err == nil || !strings.Contains(err.Error(), "allowed:") {
		t.Fatalf("query error=%v", err)
	}
	if backend.calls.Load() != 0 {
		t.Fatalf("backend calls=%d", backend.calls.Load())
	}
}

func TestProviderCachesAndReturnsDefensiveCopies(t *testing.T) {
	backend := &fakeBackend{queryFunc: func(_ context.Context, spec querySpec) ([]map[string]any, bool, error) {
		return []map[string]any{{
			"Caption": " Windows 11 ",
			"Version": "10.0",
			"Ignored": "must not cross the boundary",
		}}, false, nil
	}}
	provider := newCachedProvider(backend)
	first, err := provider.Query(context.Background(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rows) != 1 || first.Rows[0]["Caption"] != "Windows 11" {
		t.Fatalf("first=%#v", first)
	}
	if _, exists := first.Rows[0]["Ignored"]; exists {
		t.Fatalf("unlisted column crossed boundary: %#v", first.Rows[0])
	}
	first.Rows[0]["Caption"] = "mutated"
	second, err := provider.Query(context.Background(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if second.Rows[0]["Caption"] != "Windows 11" || backend.calls.Load() != 1 {
		t.Fatalf("cached=%#v calls=%d", second, backend.calls.Load())
	}
}

func TestProviderCapsRowsCellsAndEncodedResult(t *testing.T) {
	spec := querySpec{QueryDescriptor: QueryDescriptor{
		Profile: "test", Class: "Test",
		Columns: []string{"Name", "SerialNumber"}, MaxRows: 2,
	}}
	rows := []map[string]any{
		{"Name": "safe\u202E\n" + strings.Repeat("x", MaxCellBytes*2), "SerialNumber": "secret-1"},
		{"Name": "second", "SerialNumber": "secret-2"},
		{"Name": "third", "SerialNumber": "secret-3"},
	}
	bounded, truncated := sanitizeRows(spec, rows)
	if !truncated || len(bounded) != 2 {
		t.Fatalf("rows=%#v truncated=%t", bounded, truncated)
	}
	if bounded[0]["SerialNumber"] != "[redacted]" {
		t.Fatalf("serial was not redacted: %#v", bounded[0])
	}
	name := bounded[0]["Name"].(string)
	if strings.ContainsRune(name, '\u202E') || strings.ContainsRune(name, '\n') || len(name) > MaxCellBytes {
		t.Fatalf("unsafe/unbounded text=%q bytes=%d", name, len(name))
	}
	encoded, err := json.Marshal(bounded)
	if err != nil || len(encoded) > MaxResultBytes {
		t.Fatalf("encoded bytes=%d err=%v", len(encoded), err)
	}
	large := Result{
		Profile: "serial", Class: "Win32_SerialPort", Source: "wmi",
		Columns: []string{"A", "B", "C", "D", "E"},
	}
	for index := 0; index < maxGlobalRows; index++ {
		large.Rows = append(large.Rows, map[string]any{
			"A": strings.Repeat("a", MaxCellBytes),
			"B": strings.Repeat("b", MaxCellBytes),
			"C": strings.Repeat("c", MaxCellBytes),
			"D": strings.Repeat("d", MaxCellBytes),
			"E": strings.Repeat("e", MaxCellBytes),
		})
	}
	large = boundResult(large)
	encoded, err = json.Marshal(large)
	if err != nil || len(encoded) > MaxResultBytes || !large.Truncated {
		t.Fatalf("bounded result bytes=%d rows=%d truncated=%t err=%v", len(encoded), len(large.Rows), large.Truncated, err)
	}
}

func TestProviderTimeoutDoesNotSpawnMoreBlockedWorkers(t *testing.T) {
	release := make(chan struct{})
	backend := &fakeBackend{queryFunc: func(context.Context, querySpec) ([]map[string]any, bool, error) {
		<-release
		return []map[string]any{{"Caption": "released"}}, false, nil
	}}
	provider := newCachedProvider(backend)

	firstContext, firstCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer firstCancel()
	if _, err := provider.Query(firstContext, "system"); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("first timeout err=%v", err)
	}
	secondContext, secondCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer secondCancel()
	if _, err := provider.Query(secondContext, "computer"); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("second timeout err=%v", err)
	}
	if backend.calls.Load() != 1 {
		t.Fatalf("blocked backend calls=%d; expected one serialized worker", backend.calls.Load())
	}
	close(release)
}

func TestProviderContainsNativeWorkerPanic(t *testing.T) {
	provider := newCachedProvider(&fakeBackend{queryFunc: func(context.Context, querySpec) ([]map[string]any, bool, error) {
		panic("unexpected COM value")
	}})
	if _, err := provider.Query(context.Background(), "system"); err == nil ||
		!strings.Contains(err.Error(), "native worker failed") {
		t.Fatalf("panic containment err=%v", err)
	}
}
