// Command generate-sfx writes the deterministic bundled PCController PCM WAV
// catalog. It is host-neutral so Linux release builders can reproduce the
// exact assets without Windows audio APIs or CGO.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"pccontroller.local/desktop/internal/sfxgen"
)

func main() {
	var output string
	flag.StringVar(
		&output,
		"out",
		filepath.Join("assets", "sfx"),
		"output directory",
	)
	flag.Parse()
	if flag.NArg() != 0 {
		fatalf("unexpected argument %q", flag.Arg(0))
	}

	output, err := filepath.Abs(filepath.Clean(output))
	if err != nil {
		fatalf("resolve output directory: %v", err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}

	for _, asset := range sfxgen.GenerateCatalog() {
		target := filepath.Join(output, asset.FileName)
		if err := writeIfChanged(target, asset.Data); err != nil {
			fatalf("write %s: %v", asset.FileName, err)
		}
		sum := sha256.Sum256(asset.Data)
		fmt.Printf(
			"%s  %s  %d bytes\n",
			hex.EncodeToString(sum[:]),
			asset.FileName,
			len(asset.Data),
		)
	}
}

func writeIfChanged(path string, data []byte) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "generate-sfx: "+format+"\n", args...)
	os.Exit(1)
}
