package webui

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

func TestExportZIPIsDeterministicAndMatchesEmbeddedDistribution(t *testing.T) {
	var first, second bytes.Buffer
	firstInfo, err := ExportZIP(&first)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := ExportZIP(&second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) || firstInfo != secondInfo {
		t.Fatal("two exports of the same embedded WebUI were not byte-identical")
	}
	digest := sha256.Sum256(first.Bytes())
	if firstInfo.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("reported SHA-256=%q want=%x", firstInfo.SHA256, digest)
	}

	root, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		t.Fatal(err)
	}
	wantNames := make([]string, 0, firstInfo.Files)
	wantContent := make(map[string][]byte, firstInfo.Files)
	err = fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || name == "." || entry.IsDir() {
			return walkErr
		}
		content, readErr := fs.ReadFile(root, name)
		if readErr != nil {
			return readErr
		}
		wantNames = append(wantNames, name)
		wantContent[name] = content
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(wantNames)

	reader, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != firstInfo.Files || len(reader.File) != len(wantNames) {
		t.Fatalf("ZIP files=%d info=%d embedded=%d", len(reader.File), firstInfo.Files, len(wantNames))
	}
	var total int64
	for index, file := range reader.File {
		if file.Name != wantNames[index] {
			t.Fatalf("entry %d=%q want=%q", index, file.Name, wantNames[index])
		}
		if !safeArchiveName(file.Name) || file.FileInfo().Mode().Perm() != 0o644 || file.Method != zip.Store {
			t.Fatalf("unsafe or unstable entry metadata for %q: mode=%v method=%d", file.Name, file.Mode(), file.Method)
		}
		if !file.Modified.Equal(archiveTimestamp) {
			t.Fatalf("entry %q timestamp=%v want=%v", file.Name, file.Modified, archiveTimestamp)
		}
		stream, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(stream)
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %q: read=%v close=%v", file.Name, readErr, closeErr)
		}
		if !bytes.Equal(content, wantContent[file.Name]) {
			t.Fatalf("entry %q does not match the embedded file", file.Name)
		}
		total += int64(len(content))
	}
	if total != firstInfo.UncompressedBytes {
		t.Fatalf("uncompressed bytes=%d want=%d", firstInfo.UncompressedBytes, total)
	}
}

func TestExportZIPIncludesPortableRuntimeAssets(t *testing.T) {
	var output bytes.Buffer
	if _, err := ExportZIP(&output); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	joined := "\n" + strings.Join(names, "\n") + "\n"
	for _, exact := range []string{
		"index.html", "favicon.ico", "favicon.svg", "manifest.webmanifest",
		"service-worker.js", "theme-init.js", "controller-config.js",
	} {
		if !strings.Contains(joined, "\n"+exact+"\n") {
			t.Fatalf("portable export is missing %q; files=%v", exact, names)
		}
	}
	for _, prefix := range []string{"assets/", "fonts/"} {
		if !containsPrefix(names, prefix) {
			t.Fatalf("portable export is missing %q content; files=%v", prefix, names)
		}
	}
}

func TestExportZIPRejectsUnsafeOrOversizedInput(t *testing.T) {
	for _, name := range []string{"", ".", "../escape", "/rooted", "assets\\escape.js", "assets/../escape.js", "nul\x00name"} {
		if safeArchiveName(name) {
			t.Fatalf("unsafe archive name %q was accepted", name)
		}
	}

	oversized := fstest.MapFS{
		"index.html": {Data: make([]byte, maximumExportBytes+1)},
	}
	if _, err := exportZIP(io.Discard, oversized, false); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized export error=%v", err)
	}
	if _, err := exportZIP(nil, fstest.MapFS{}, false); err == nil {
		t.Fatal("nil export writer was accepted")
	}
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
