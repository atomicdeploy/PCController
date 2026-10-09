package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"pccontroller.local/controller/internal/webui"
)

type webExportResult struct {
	Output            string `json:"output"`
	Files             int    `json:"files"`
	UncompressedBytes int64  `json:"uncompressed_bytes"`
	ArchiveBytes      int    `json:"archive_bytes"`
	SHA256            string `json:"sha256"`
}

// runWebExport emits the exact already-embedded WebUI. It never rebuilds the
// frontend, reads user configuration, embeds a token, or replaces an existing
// destination. Refusing replacement keeps this audit/export command safe in
// scripts and makes the resulting digest unambiguous.
func runWebExport(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("web export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "new ZIP path for the embedded WebUI")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*output) == "" {
		return errors.New("usage: controller web export --output FILE.zip")
	}

	outputPath, err := filepath.Abs(strings.TrimSpace(*output))
	if err != nil {
		return fmt.Errorf("resolve WebUI export path: %w", err)
	}
	if !strings.EqualFold(filepath.Ext(outputPath), ".zip") {
		return errors.New("WebUI export output must use a .zip extension")
	}

	var archive bytes.Buffer
	info, err := webui.ExportZIP(&archive)
	if err != nil {
		return err
	}
	archiveBytes := archive.Len()
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("WebUI export destination already exists: %s", outputPath)
		}
		return fmt.Errorf("create WebUI export: %w", err)
	}
	removeIncomplete := true
	defer func() {
		if removeIncomplete {
			_ = os.Remove(outputPath)
		}
	}()
	if _, err := io.Copy(file, &archive); err != nil {
		_ = file.Close()
		return fmt.Errorf("write WebUI export: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync WebUI export: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close WebUI export: %w", err)
	}
	removeIncomplete = false

	result := webExportResult{
		Output: outputPath, Files: info.Files, UncompressedBytes: info.UncompressedBytes,
		ArchiveBytes: archiveBytes, SHA256: info.SHA256,
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(stdout, string(encoded))
	return nil
}
