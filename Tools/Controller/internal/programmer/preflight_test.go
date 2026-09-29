package programmer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightProgrammerRejectsMissingConfiguredPaths(t *testing.T) {
	directory := t.TempDir()
	options := Options{Avrdude: filepath.Join(directory, executableName("missing")), AvrdudeConf: filepath.Join(directory, "avrdude.conf")}
	if _, err := PreflightProgrammer(context.Background(), options); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("missing tool: %v", err)
	}
	options.Avrdude, _ = os.Executable()
	if _, err := PreflightProgrammer(context.Background(), options); err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("missing config: %v", err)
	}
	if err := os.WriteFile(options.AvrdudeConf, []byte("# fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := PreflightProgrammer(context.Background(), options)
	if err != nil || got.AvrdudeConf != options.AvrdudeConf {
		t.Fatalf("valid configured pair: %#v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PreflightProgrammer(ctx, options); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestInstalledAvrdudeUsesConfiguredDataAndAllVendors(t *testing.T) {
	directory := t.TempDir()
	for _, relative := range []string{"arduino/tools/avrdude/8.0/bin", "MiniCore/tools/avrdude/7.2/bin"} {
		bin := filepath.Join(directory, "packages", filepath.FromSlash(relative))
		if err := os.MkdirAll(bin, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, executableName("avrdude")), []byte("fixture"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "avrdude.conf"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	executable, conf := findInstalledAvrdude(directory, "")
	if !strings.Contains(executable, "8.0") || filepath.Dir(executable) != filepath.Dir(conf) {
		t.Fatalf("wrong pair: %s %s", executable, conf)
	}
	if executable, _ := findInstalledAvrdude(t.TempDir(), ""); executable != "" {
		t.Fatal("escaped configured data directory")
	}
}
