//go:build linux

package hostfacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLinuxSensorsCommandUsesFixedReadOnlyArgumentsAndCache(t *testing.T) {
	directory := t.TempDir()
	program := "#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = '-j' ] && [ \"$LC_ALL\" = C ] || exit 12\nprintf '%s' '{\"fixture\":{\"CPU\":{\"temp1_input\":42.5}}}'\n"
	if err := os.WriteFile(filepath.Join(directory, "sensors"), []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	provider := newCachedProvider(nativeBackend{})
	first, err := provider.Query(context.Background(), "sensors")
	if err != nil || first.Source != "lm-sensors" || len(first.Rows) != 1 || first.Rows[0]["Value"] != 42.5 {
		t.Fatalf("first=%v err=%v", first, err)
	}
	if err := os.Remove(filepath.Join(directory, "sensors")); err != nil {
		t.Fatal(err)
	}
	second, err := provider.Query(context.Background(), "sensors")
	if err != nil || second.CollectedAt != first.CollectedAt {
		t.Fatalf("did not reuse shared cached snapshot: %v", err)
	}
	if _, _, err := linuxSensorFacts(context.Background()); err == nil {
		t.Fatal("missing lm-sensors falsely succeeded")
	}
}

func TestLinuxSensorCommandDeadline(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "sensors"), []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, _, err := linuxSensorFacts(ctx); err == nil {
		t.Fatal("sensor command ignored deadline")
	}
}
