// Package webexport creates the reproducible, frontend-only RayanLamp archive.
package webexport

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"rayanlamp.local/app/assets"
)

const readmeArchiveName = "README.txt"

var (
	// ZIP's DOS timestamp representation begins in 1980. Keeping every entry
	// at that boundary avoids source-file and build-machine timestamp leakage.
	archiveTimestamp = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

	//go:embed static-readme.txt
	readmeFiles embed.FS
)

var requiredDistributionFiles = []string{
	"build-edition.json",
	"index.html",
	"manifest.webmanifest",
	"sw.js",
	"theme-init.js",
}

var sharedAssetArchiveNames = []string{
	"shared/fonts/Vazirmatn-Regular.ttf",
	"shared/icons/app.png",
	"shared/sfx/on.wav",
	"shared/sfx/off.wav",
	"shared/sfx/message.wav",
	"shared/sfx/error.wav",
}

// Result describes a successfully replaced static archive.
type Result struct {
	Path    string
	SHA256  string
	Size    int64
	Entries int
}

type archiveEntry struct {
	name string
	data []byte
}

// ExportDirectory validates sourceDirectory, writes a complete ZIP to a
// temporary sibling of outputPath, synchronizes it, and replaces outputPath.
// Validation finishes before the existing output is touched.
func ExportDirectory(sourceDirectory, outputPath string) (Result, error) {
	sourceDirectory = strings.TrimSpace(sourceDirectory)
	outputPath = strings.TrimSpace(outputPath)
	if sourceDirectory == "" {
		return Result{}, errors.New("web export source directory is required")
	}
	if outputPath == "" {
		return Result{}, errors.New("web export output path is required")
	}
	if !strings.EqualFold(filepath.Ext(outputPath), ".zip") {
		return Result{}, errors.New("web export output must have a .zip extension")
	}

	sourceAbsolute, err := filepath.Abs(sourceDirectory)
	if err != nil {
		return Result{}, fmt.Errorf("resolve web export source: %w", err)
	}
	outputAbsolute, err := filepath.Abs(outputPath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve web export output: %w", err)
	}
	info, err := os.Stat(sourceAbsolute)
	if err != nil {
		return Result{}, fmt.Errorf("inspect web export source: %w", err)
	}
	if !info.IsDir() {
		return Result{}, errors.New("web export source must be a directory")
	}
	if pathWithin(sourceAbsolute, outputAbsolute) {
		return Result{}, errors.New("web export output must be outside the source directory")
	}

	entries, err := collectEntries(os.DirFS(sourceAbsolute))
	if err != nil {
		return Result{}, err
	}

	outputDirectory := filepath.Dir(outputAbsolute)
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return Result{}, fmt.Errorf("create web export output directory: %w", err)
	}
	temporary, err := os.CreateTemp(
		outputDirectory,
		"."+filepath.Base(outputAbsolute)+".*.tmp",
	)
	if err != nil {
		return Result{}, fmt.Errorf("create temporary web export: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := writeArchive(temporary, entries); err != nil {
		_ = temporary.Close()
		return Result{}, fmt.Errorf("write temporary web export: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return Result{}, fmt.Errorf("set temporary web export permissions: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return Result{}, fmt.Errorf("synchronize temporary web export: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Result{}, fmt.Errorf("close temporary web export: %w", err)
	}

	hash, size, err := hashFile(temporaryPath)
	if err != nil {
		return Result{}, fmt.Errorf("verify temporary web export: %w", err)
	}
	if err := os.Rename(temporaryPath, outputAbsolute); err != nil {
		return Result{}, fmt.Errorf("replace web export output: %w", err)
	}
	removeTemporary = false

	return Result{
		Path:    outputAbsolute,
		SHA256:  hash,
		Size:    size,
		Entries: len(entries),
	}, nil
}

func collectEntries(source fs.FS) ([]archiveEntry, error) {
	if source == nil {
		return nil, errors.New("web export source filesystem is nil")
	}

	entries := make([]archiveEntry, 0, 16)
	seen := make([]string, 0, 16)
	foundRequired := make(map[string]bool, len(requiredDistributionFiles))
	err := fs.WalkDir(source, ".", func(sourceName string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if sourceName == "." {
			if !item.IsDir() {
				return errors.New("web export source root is not a directory")
			}
			return nil
		}
		if item.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("web export source contains a symbolic link: %s", sourceName)
		}
		if item.IsDir() {
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return fmt.Errorf("inspect web export entry %s: %w", sourceName, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("web export source contains a non-regular file: %s", sourceName)
		}

		archiveName := strings.ReplaceAll(sourceName, `\`, "/")
		if err := validateArchiveName(archiveName); err != nil {
			return err
		}
		if strings.EqualFold(path.Ext(archiveName), ".map") {
			return fmt.Errorf("web export source map is forbidden: %s", archiveName)
		}
		if duplicateName(seen, archiveName) {
			return fmt.Errorf("web export contains a duplicate archive name: %s", archiveName)
		}
		seen = append(seen, archiveName)

		data, err := fs.ReadFile(source, sourceName)
		if err != nil {
			return fmt.Errorf("read web export entry %s: %w", archiveName, err)
		}
		if archiveName == "build-edition.json" {
			if err := validateStaticEditionMarker(data); err != nil {
				return err
			}
		}
		entries = append(entries, archiveEntry{name: archiveName, data: data})
		for _, required := range requiredDistributionFiles {
			if archiveName == required {
				foundRequired[required] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, required := range requiredDistributionFiles {
		if !foundRequired[required] {
			return nil, fmt.Errorf("web export source is incomplete: missing %s", required)
		}
	}
	for _, archiveName := range sharedAssetArchiveNames {
		assetName := strings.TrimPrefix(archiveName, "shared/")
		data, err := assets.Files.ReadFile(assetName)
		if err != nil {
			return nil, fmt.Errorf(
				"read shared web export asset %s: %w",
				archiveName,
				err,
			)
		}
		if index := archiveEntryIndex(entries, archiveName); index >= 0 {
			if entries[index].name != archiveName {
				return nil, fmt.Errorf(
					"web export distribution has a case-conflicting canonical shared asset %s",
					entries[index].name,
				)
			}
			if !bytes.Equal(entries[index].data, data) {
				return nil, fmt.Errorf(
					"web export distribution asset %s differs from the canonical embedded asset",
					archiveName,
				)
			}
			// Vite emits the static-only font directly so its intermediate
			// distribution is usable before ZIP packaging. Retain that exact
			// canonical entry rather than injecting a second copy.
			continue
		}
		seen = append(seen, archiveName)
		entries = append(entries, archiveEntry{
			name: archiveName,
			data: data,
		})
	}
	if duplicateName(seen, readmeArchiveName) {
		return nil, fmt.Errorf(
			"web export distribution conflicts with generated %s",
			readmeArchiveName,
		)
	}
	readme, err := readmeFiles.ReadFile("static-readme.txt")
	if err != nil {
		return nil, fmt.Errorf("read embedded static dashboard README: %w", err)
	}
	entries = append(entries, archiveEntry{name: readmeArchiveName, data: readme})

	sort.Slice(entries, func(left, right int) bool {
		return entries[left].name < entries[right].name
	})
	return entries, nil
}

func validateStaticEditionMarker(data []byte) error {
	var marker struct {
		Edition        string `json:"edition"`
		DeveloperTools bool   `json:"developer_tools"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return fmt.Errorf("web export build-edition.json is invalid: %w", err)
	}
	if marker.Edition != "production" || marker.DeveloperTools {
		return errors.New(
			"web export must use the production edition without developer tools",
		)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("web export build-edition.json contains trailing JSON")
	}
	return nil
}

func archiveEntryIndex(entries []archiveEntry, candidate string) int {
	for index, entry := range entries {
		if strings.EqualFold(entry.name, candidate) {
			return index
		}
	}
	return -1
}

func writeArchive(destination io.Writer, entries []archiveEntry) error {
	archive := zip.NewWriter(destination)
	for _, entry := range entries {
		header := &zip.FileHeader{
			Name:   entry.name,
			Method: zip.Deflate,
		}
		header.SetModTime(archiveTimestamp)
		header.SetMode(0o644)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			_ = archive.Close()
			return fmt.Errorf("create ZIP entry %s: %w", entry.name, err)
		}
		if _, err := writer.Write(entry.data); err != nil {
			_ = archive.Close()
			return fmt.Errorf("write ZIP entry %s: %w", entry.name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("finish ZIP archive: %w", err)
	}
	return nil
}

func validateArchiveName(name string) error {
	if name == "" || !utf8.ValidString(name) || !fs.ValidPath(name) ||
		path.IsAbs(name) || path.Clean(name) != name ||
		strings.Contains(name, `\`) {
		return fmt.Errorf("unsafe web export archive name: %q", name)
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("unsafe web export archive name: %q", name)
		}
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." ||
			strings.TrimRight(segment, " .") != segment ||
			strings.ContainsAny(segment, `<>:"|?*`) ||
			reservedWindowsName(segment) {
			return fmt.Errorf("unsafe web export archive name: %q", name)
		}
	}
	return nil
}

func reservedWindowsName(segment string) bool {
	base := segment
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func duplicateName(existing []string, candidate string) bool {
	for _, name := range existing {
		if strings.EqualFold(name, candidate) {
			return true
		}
	}
	return false
}

func pathWithin(directory, candidate string) bool {
	relative, err := filepath.Rel(directory, candidate)
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func hashFile(name string) (string, int64, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	sum := sha256.New()
	size, err := io.Copy(sum, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(sum.Sum(nil)), size, nil
}
