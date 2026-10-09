package webui

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	maximumExportFiles = 4096
	maximumExportBytes = 64 << 20
)

var archiveTimestamp = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// ExportInfo describes a completed portable WebUI archive. SHA256 is the
// digest of the ZIP bytes written to the caller, not a digest of a temporary
// or rebuilt directory.
type ExportInfo struct {
	Files             int
	UncompressedBytes int64
	SHA256            string
}

// ExportZIP writes the exact WebUI distribution embedded in this executable
// as a deterministic ZIP archive. Files are stored at the archive root so the
// extracted index.html can be served directly by an ordinary static server.
//
// The archive contains no configuration, session token, or other runtime
// secret. A caller can wire this function directly to a CLI output file.
func ExportZIP(writer io.Writer) (ExportInfo, error) {
	if writer == nil {
		return ExportInfo{}, errors.New("web UI export writer is nil")
	}
	root, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		return ExportInfo{}, fmt.Errorf("open embedded web UI: %w", err)
	}
	return exportZIP(writer, root, true)
}

type exportAsset struct {
	name string
	size int64
}

func exportZIP(writer io.Writer, files fs.FS, requirePortableBundle bool) (ExportInfo, error) {
	if writer == nil {
		return ExportInfo{}, errors.New("web UI export writer is nil")
	}
	if files == nil {
		return ExportInfo{}, errors.New("web UI export filesystem is nil")
	}

	assets, total, err := exportAssets(files)
	if err != nil {
		return ExportInfo{}, err
	}
	if requirePortableBundle {
		if err := requirePortableAssets(assets); err != nil {
			return ExportInfo{}, err
		}
	}

	digest := sha256.New()
	archive := zip.NewWriter(io.MultiWriter(writer, digest))
	for _, asset := range assets {
		header := &zip.FileHeader{
			Name:     asset.name,
			Method:   zip.Store,
			Modified: archiveTimestamp,
		}
		header.SetMode(0o644)
		entry, createErr := archive.CreateHeader(header)
		if createErr != nil {
			_ = archive.Close()
			return ExportInfo{}, fmt.Errorf("create ZIP entry %q: %w", asset.name, createErr)
		}
		file, openErr := files.Open(asset.name)
		if openErr != nil {
			_ = archive.Close()
			return ExportInfo{}, fmt.Errorf("open WebUI asset %q: %w", asset.name, openErr)
		}
		written, copyErr := io.CopyN(entry, file, asset.size)
		if copyErr != nil {
			_ = file.Close()
			_ = archive.Close()
			return ExportInfo{}, fmt.Errorf("write WebUI asset %q: %w", asset.name, copyErr)
		}
		if written != asset.size {
			_ = file.Close()
			_ = archive.Close()
			return ExportInfo{}, fmt.Errorf("write WebUI asset %q: wrote %d of %d bytes", asset.name, written, asset.size)
		}
		var trailing [1]byte
		if count, readErr := file.Read(trailing[:]); count != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
			_ = file.Close()
			_ = archive.Close()
			return ExportInfo{}, fmt.Errorf("WebUI asset %q changed while exporting", asset.name)
		}
		if closeErr := file.Close(); closeErr != nil {
			_ = archive.Close()
			return ExportInfo{}, fmt.Errorf("close WebUI asset %q: %w", asset.name, closeErr)
		}
	}
	if err := archive.Close(); err != nil {
		return ExportInfo{}, fmt.Errorf("finish WebUI ZIP: %w", err)
	}
	return ExportInfo{
		Files:             len(assets),
		UncompressedBytes: total,
		SHA256:            hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func exportAssets(files fs.FS) ([]exportAsset, int64, error) {
	assets := make([]exportAsset, 0, 32)
	var total int64
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." || entry.IsDir() {
			return nil
		}
		if !safeArchiveName(name) {
			return fmt.Errorf("unsafe WebUI archive path %q", name)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect WebUI asset %q: %w", name, err)
		}
		if !info.Mode().IsRegular() || info.Size() < 0 {
			return fmt.Errorf("WebUI asset %q is not a regular file", name)
		}
		if len(assets) >= maximumExportFiles {
			return fmt.Errorf("WebUI export exceeds %d files", maximumExportFiles)
		}
		if info.Size() > maximumExportBytes-total {
			return fmt.Errorf("WebUI export exceeds %d bytes", maximumExportBytes)
		}
		total += info.Size()
		assets = append(assets, exportAsset{name: name, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("index WebUI export: %w", err)
	}
	sort.Slice(assets, func(left, right int) bool { return assets[left].name < assets[right].name })
	return assets, total, nil
}

func safeArchiveName(name string) bool {
	return name != "" && name != "." && fs.ValidPath(name) && path.Clean(name) == name &&
		!strings.Contains(name, "\\") && !strings.ContainsRune(name, '\x00') &&
		!strings.HasPrefix(name, "/")
}

func requirePortableAssets(assets []exportAsset) error {
	available := make(map[string]bool, len(assets))
	var haveAsset, haveFont bool
	for _, asset := range assets {
		available[asset.name] = true
		haveAsset = haveAsset || strings.HasPrefix(asset.name, "assets/")
		haveFont = haveFont || strings.HasPrefix(asset.name, "fonts/")
	}
	for _, required := range []string{
		"index.html",
		"favicon.ico",
		"favicon.svg",
		"manifest.webmanifest",
		"service-worker.js",
		"theme-init.js",
		"controller-config.js",
	} {
		if !available[required] {
			return fmt.Errorf("embedded WebUI export is missing %s", required)
		}
	}
	if !haveAsset || !haveFont {
		return errors.New("embedded WebUI export must contain asset and font directories")
	}
	return nil
}
