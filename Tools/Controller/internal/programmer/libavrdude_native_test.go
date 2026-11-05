//go:build cgo && libavrdude

package programmer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNativeAvrdudeRepeatedConfigurationAndCanceledWait(t *testing.T) {
	configuration := filepath.Join(t.TempDir(), "avrdude.conf")
	if err := os.WriteFile(configuration, []byte("# deliberately empty, no programmer can be opened\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Optional real packaged parser input still uses a nonexistent programmer:
	// it must fail before port enumeration or any board access.
	if supplied := os.Getenv("PCCONTROLLER_AVRDUDE_TEST_CONFIG"); supplied != "" {
		configuration = supplied
	}
	command := Command{Name: nativeAvrdudeCommand, Args: []string{"--config", configuration,
		"--part", "atmega328p", "--programmer", "nonexistent-offline-fixture"}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var output bytes.Buffer
			if err := Run(context.Background(), command, &output); err == nil || !strings.Contains(output.String(), "was not found") {
				t.Errorf("configuration failure/cleanup: %v %s", err, output.String())
			}
		}()
	}
	wg.Wait()
	malformed := filepath.Join(t.TempDir(), "malformed.conf")
	if err := os.WriteFile(malformed, []byte("part id = \"unfinished\"; memory \"flash\" size = "), 0600); err != nil {
		t.Fatal(err)
	}
	broken := command
	broken.Args = append([]string(nil), command.Args...)
	broken.Args[1] = malformed
	if err := Run(context.Background(), broken, io.Discard); err == nil {
		t.Fatal("malformed config succeeded")
	}
	var recovered bytes.Buffer
	if err := Run(context.Background(), command, &recovered); err == nil || !strings.Contains(recovered.String(), "was not found") {
		t.Fatalf("parser recovery: %v %s", err, recovered.String())
	}
	nativeAvrdudeGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, command, io.Discard)
	<-nativeAvrdudeGate
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting cancellation: %v", err)
	}
}

func TestNativeAvrdudeSoftwareProgrammer(t *testing.T) {
	configuration := os.Getenv("PCCONTROLLER_AVRDUDE_TEST_CONFIG")
	if configuration == "" {
		t.Skip("requires pinned config; uses only libavrdude's software dryrun programmer")
	}
	var output bytes.Buffer
	file := filepath.Join(t.TempDir(), "flash.hex")
	progress := 0
	ctx := WithProgress(context.Background(), func(Progress) { progress++ })
	command := Command{Name: nativeAvrdudeCommand, Args: []string{"--config", configuration,
		"--part", "atmega328p", "--programmer", "dryrun", "--port", "offline", "--update", "flash:r:" + file + ":i"}}
	if err := Run(ctx, command, &output); err != nil {
		t.Fatalf("software read: %v\n%s", err, output.String())
	}
	image, err := LoadIntelHex(file)
	if err != nil {
		t.Fatal(err)
	}
	if image == nil || progress < 2 {
		t.Fatalf("missing software image/progress: %d", progress)
	}
	// A write is against the software programmer's RAM, never a real port.
	input := filepath.Join(t.TempDir(), "input.hex")
	if err := os.WriteFile(input, []byte(":0100000001FE\n:00000001FF\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command.Args[len(command.Args)-1] = "flash:w:" + input + ":i"
	if err := Run(ctx, command, &output); err != nil {
		t.Fatalf("software write/verify: %v\n%s", err, output.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = WithProgress(ctx, func(value Progress) {
		if value.Percent >= 0 {
			cancel()
		}
	})
	command.Args[len(command.Args)-1] = "flash:r:" + file + ":i"
	if err := Run(ctx, command, &output); !errors.Is(err, context.Canceled) {
		t.Fatalf("transfer cancellation: %v", err)
	}
	if err := Run(context.Background(), command, &output); err != nil {
		t.Fatalf("operation after cancellation: %v", err)
	}
}
