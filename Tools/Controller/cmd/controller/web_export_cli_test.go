package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWebExportWritesNewDeterministicEmbeddedArchive(t *testing.T) {
	output := filepath.Join(t.TempDir(), "control-center.zip")
	var stdout, stderr bytes.Buffer
	if err := runWebExport([]string{"--output", output}, &stdout, &stderr); err != nil {
		t.Fatalf("export: %v stderr=%s", err, stderr.String())
	}
	var result webExportResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != output || result.Files < 7 || result.ArchiveBytes != int(info.Size()) || len(result.SHA256) != 64 {
		t.Fatalf("result=%#v file=%v", result, info)
	}
	reader, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != result.Files {
		t.Fatalf("ZIP files=%d result=%d", len(reader.File), result.Files)
	}

	if err := runWebExport([]string{"--output", output}, &stdout, &stderr); err == nil {
		t.Fatal("existing export destination was overwritten")
	}
}

func TestRunWebExportRequiresExplicitZIPPath(t *testing.T) {
	cases := [][]string{nil, {"--output", "bundle.tar"}, {"extra"}}
	for _, args := range cases {
		if err := runWebExport(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("arguments %v were accepted", args)
		}
	}
}
