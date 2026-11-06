package webexport

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"pccontroller.local/desktop/assets"
)

func TestExportDirectoryIsDeterministicAndComplete(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "web")
	writeTestDistribution(t, source)

	firstPath := filepath.Join(root, "first.zip")
	first, err := ExportDirectory(source, firstPath)
	if err != nil {
		t.Fatal(err)
	}

	differentTime := time.Date(2037, time.May, 6, 7, 8, 9, 0, time.UTC)
	if err := filepath.Walk(source, func(name string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			return os.Chtimes(name, differentTime, differentTime)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	secondPath := filepath.Join(root, "second.zip")
	second, err := ExportDirectory(source, secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 || first.Size != second.Size {
		t.Fatalf(
			"archives differ: first=%s/%d second=%s/%d",
			first.SHA256,
			first.Size,
			second.SHA256,
			second.Size,
		)
	}
	firstBytes, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("archives with identical content are not byte-for-byte deterministic")
	}

	reader, err := zip.OpenReader(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	wantNames := []string{
		"README.txt",
		"assets/app-12345678.js",
		"build-edition.json",
		"icons/app.png",
		"index.html",
		"manifest.webmanifest",
		"shared/fonts/Vazirmatn-Regular.ttf",
		"shared/icons/app.png",
		"shared/sfx/error.wav",
		"shared/sfx/message.wav",
		"shared/sfx/off.wav",
		"shared/sfx/on.wav",
		"sw.js",
		"theme-init.js",
	}
	gotNames := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		gotNames = append(gotNames, file.Name)
		if !file.Modified.UTC().Equal(archiveTimestamp) {
			t.Errorf("%s timestamp = %s", file.Name, file.Modified)
		}
		if got := file.Mode().Perm(); got != 0o644 {
			t.Errorf("%s mode = %o", file.Name, got)
		}
		if file.Method != zip.Deflate {
			t.Errorf("%s compression method = %d", file.Name, file.Method)
		}
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("archive names = %#v, want %#v", gotNames, wantNames)
	}
	if first.Entries != len(wantNames) {
		t.Fatalf("entry count = %d, want %d", first.Entries, len(wantNames))
	}

	embedded, err := readmeFiles.ReadFile("static-readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		got := readZipEntry(t, file)
		if file.Name == readmeArchiveName {
			if !bytes.Equal(got, embedded) {
				t.Fatal("generated README does not match the deterministic template")
			}
			continue
		}
		if strings.HasPrefix(file.Name, "shared/") {
			want, err := assets.Files.ReadFile(
				strings.TrimPrefix(file.Name, "shared/"),
			)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from the canonical embedded asset", file.Name)
			}
			continue
		}
		want, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(file.Name)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s content differs from the source distribution", file.Name)
		}
	}
}

func TestExportDirectoryReplacesExistingOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "web")
	writeTestDistribution(t, source)
	output := filepath.Join(root, "release.zip")
	if err := os.WriteFile(output, []byte("old archive"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ExportDirectory(source, output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != output {
		t.Fatalf("result path = %q, want %q", result.Path, output)
	}
	reader, err := zip.OpenReader(output)
	if err != nil {
		t.Fatalf("existing output was not replaced with a ZIP: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".release.zip.*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary archives remain: %#v", matches)
	}
}

func TestExportRejectsSourceMapWithoutTouchingExistingOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "web")
	writeTestDistribution(t, source)
	if err := os.WriteFile(
		filepath.Join(source, "assets", "app.js.map"),
		[]byte("{}"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "release.zip")
	const sentinel = "preserve me"
	if err := os.WriteFile(output, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ExportDirectory(source, output)
	if err == nil || !strings.Contains(err.Error(), "source map") {
		t.Fatalf("error = %v, want source-map rejection", err)
	}
	got, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != sentinel {
		t.Fatalf("existing output changed after validation failure: %q", got)
	}
}

func TestExportRejectsDistributionWithoutPrepaintThemeInitializer(
	t *testing.T,
) {
	root := t.TempDir()
	source := filepath.Join(root, "web")
	writeTestDistribution(t, source)
	if err := os.Remove(filepath.Join(source, "theme-init.js")); err != nil {
		t.Fatal(err)
	}

	_, err := ExportDirectory(source, filepath.Join(root, "release.zip"))
	if err == nil || !strings.Contains(err.Error(), "missing theme-init.js") {
		t.Fatalf("error = %v, want missing theme-init.js rejection", err)
	}
}

func TestExportRejectsDevelopmentEdition(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "web")
	writeTestDistribution(t, source)
	if err := os.WriteFile(
		filepath.Join(source, "build-edition.json"),
		[]byte(`{"edition":"develop","developer_tools":true}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, err := ExportDirectory(source, filepath.Join(root, "release.zip"))
	if err == nil || !strings.Contains(err.Error(), "production edition") {
		t.Fatalf("error = %v, want development-edition rejection", err)
	}
}

func TestCollectEntriesRejectsGeneratedReadmeCollision(t *testing.T) {
	source := fstest.MapFS{
		"build-edition.json":   &fstest.MapFile{Data: []byte(`{"edition":"production","developer_tools":false}`)},
		"index.html":           &fstest.MapFile{Data: []byte("<html></html>")},
		"manifest.webmanifest": &fstest.MapFile{Data: []byte("{}")},
		"sw.js":                &fstest.MapFile{Data: []byte("")},
		"theme-init.js":        &fstest.MapFile{Data: []byte("")},
		"readme.TXT":           &fstest.MapFile{Data: []byte("conflict")},
	}
	_, err := collectEntries(source)
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("error = %v, want README collision", err)
	}
}

func TestCollectEntriesAcceptsOneExactCanonicalSharedAsset(t *testing.T) {
	font, err := assets.Files.ReadFile("fonts/Vazirmatn-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	source := fstest.MapFS{
		"build-edition.json": &fstest.MapFile{
			Data: []byte(`{"edition":"production","developer_tools":false}`),
		},
		"index.html": &fstest.MapFile{
			Data: []byte("<html></html>"),
		},
		"manifest.webmanifest": &fstest.MapFile{Data: []byte("{}")},
		"sw.js":                &fstest.MapFile{Data: []byte("")},
		"theme-init.js":        &fstest.MapFile{Data: []byte("")},
		"shared/fonts/Vazirmatn-Regular.ttf": &fstest.MapFile{
			Data: font,
		},
	}
	entries, err := collectEntries(source)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.name == "shared/fonts/Vazirmatn-Regular.ttf" {
			count++
			if !bytes.Equal(entry.data, font) {
				t.Fatal("retained font differs from the canonical font")
			}
		}
	}
	if count != 1 {
		t.Fatalf("canonical font entry count = %d, want 1", count)
	}
}

func TestCollectEntriesRejectsModifiedCanonicalSharedAsset(t *testing.T) {
	source := fstest.MapFS{
		"build-edition.json": &fstest.MapFile{
			Data: []byte(`{"edition":"production","developer_tools":false}`),
		},
		"index.html": &fstest.MapFile{
			Data: []byte("<html></html>"),
		},
		"manifest.webmanifest": &fstest.MapFile{Data: []byte("{}")},
		"sw.js":                &fstest.MapFile{Data: []byte("")},
		"theme-init.js":        &fstest.MapFile{Data: []byte("")},
		"shared/fonts/Vazirmatn-Regular.ttf": &fstest.MapFile{
			Data: []byte("not-the-canonical-font"),
		},
	}
	_, err := collectEntries(source)
	if err == nil || !strings.Contains(err.Error(), "differs from the canonical") {
		t.Fatalf("error = %v, want canonical-asset mismatch", err)
	}
}

func TestCollectEntriesRejectsSymlinks(t *testing.T) {
	source := fstest.MapFS{
		"build-edition.json":   &fstest.MapFile{Data: []byte(`{"edition":"production","developer_tools":false}`)},
		"index.html":           &fstest.MapFile{Data: []byte("<html></html>")},
		"manifest.webmanifest": &fstest.MapFile{Data: []byte("{}")},
		"sw.js":                &fstest.MapFile{Data: []byte("")},
		"theme-init.js":        &fstest.MapFile{Data: []byte("")},
		"assets/link.js":       &fstest.MapFile{Mode: os.ModeSymlink},
	}
	_, err := collectEntries(source)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error = %v, want symbolic-link rejection", err)
	}
}

func TestValidateArchiveNameRejectsUnsafePortableNames(t *testing.T) {
	tests := []string{
		"",
		".",
		"../escape.js",
		"/absolute.js",
		`assets\windows.js`,
		"assets//empty.js",
		"assets/trailing. ",
		"assets/control\x00.js",
		"assets/question?.js",
		"assets/CON.txt",
		"C:/drive.js",
	}
	for _, name := range tests {
		if err := validateArchiveName(name); err == nil {
			t.Errorf("validateArchiveName(%q) succeeded", name)
		}
	}
	for _, name := range []string{
		"index.html",
		"assets/index-C_BAc-2r.js",
		"icons/app-maskable-512.png",
	} {
		if err := validateArchiveName(name); err != nil {
			t.Errorf("validateArchiveName(%q) = %v", name, err)
		}
	}
}

func TestExportRejectsOutputInsideSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "web")
	writeTestDistribution(t, source)
	_, err := ExportDirectory(source, filepath.Join(source, "release.zip"))
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("error = %v, want output containment rejection", err)
	}
}

func writeTestDistribution(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"index.html":             "<!doctype html><div id=\"app\"></div>",
		"build-edition.json":     `{"edition":"production","developer_tools":false}`,
		"manifest.webmanifest":   `{"name":"PCController"}`,
		"sw.js":                  "self.addEventListener('fetch',()=>{});",
		"theme-init.js":          "document.documentElement.dataset.theme='dark';",
		"assets/app-12345678.js": "console.log('PCController');",
		"icons/app.png":          "not-a-real-png",
	}
	for name, content := range files {
		fullName := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullName), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullName, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readZipEntry(t *testing.T, file *zip.File) []byte {
	t.Helper()
	reader, err := file.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
