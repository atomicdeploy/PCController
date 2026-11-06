package ipcjson

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type softwareBundleAsset struct {
	ID       string         `json:"id"`
	Path     string         `json:"path,omitempty"`
	Manifest map[string]any `json:"manifest"`
}
type softwareBundleCatalog struct {
	Format string                `json:"format"`
	Assets []softwareBundleAsset `json:"assets"`
}
type softwareBundleRead struct {
	AssetID string `json:"asset_id"`
	SHA256  string `json:"sha256"`
	Offset  int64  `json:"offset"`
	Length  int    `json:"length"`
}
type bundleImage struct {
	file *os.File
	size int64
	hash string
}

var bundleCache = struct {
	sync.Mutex
	files map[string]bundleImage
}{files: make(map[string]bundleImage)}

func loadSoftwareBundle(directory string) ([]softwareBundleAsset, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open("software-bundle.json")
	if errors.Is(err, os.ErrNotExist) {
		return []softwareBundleAsset{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	bytes, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return nil, err
	}
	if len(bytes) > 65536 {
		return nil, errors.New("software bundle exceeds 64 KiB")
	}
	var catalog softwareBundleCatalog
	if err = decodeStrictParams(bytes, &catalog); err != nil {
		return nil, err
	}
	if catalog.Format != "digitalogic-software-bundle" || len(catalog.Assets) > 16 {
		return nil, errors.New("invalid software bundle format or size")
	}
	ids := map[string]bool{}
	for _, asset := range catalog.Assets {
		if asset.ID == "" || len(asset.ID) > 96 || ids[asset.ID] {
			return nil, errors.New("invalid or duplicate software bundle ID")
		}
		ids[asset.ID] = true
		for _, c := range asset.ID {
			if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.", c) {
				return nil, errors.New("invalid software bundle ID")
			}
		}
		if !filepath.IsLocal(asset.Path) || asset.Path == "." {
			return nil, errors.New("software bundle path escapes deployment")
		}
		for _, component := range strings.FieldsFunc(asset.Path, func(r rune) bool { return r == '/' || r == '\\' }) {
			if component == ".." || component == "." {
				return nil, errors.New("software bundle path traverses deployment")
			}
		}
		product, _ := asset.Manifest["service"].(string)
		hash, _ := asset.Manifest["sha256"].(string)
		size, _ := asset.Manifest["size"].(float64)
		digest, decodeErr := hex.DecodeString(hash)
		if (product != "pccontroller" && product != "pealayer") || decodeErr != nil || len(digest) != 32 || hash != strings.ToLower(hash) || size < 1 || size > 512<<20 || size != float64(int64(size)) {
			return nil, errors.New("invalid software bundle product, size or digest")
		}
		info, err := root.Stat(asset.Path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("software bundle asset unavailable")
		}
	}
	return catalog.Assets, nil
}

func softwareBundleList(directory string) (any, error) {
	assets, err := loadSoftwareBundle(directory)
	if err != nil {
		return nil, err
	}
	for i := range assets {
		assets[i].Path = ""
	}
	return map[string]any{"assets": assets}, nil
}

func softwareBundleReadChunk(directory string, request softwareBundleRead) (any, error) {
	if request.Offset < 0 || request.Length < 1 || request.Length > softwareChunkBytes {
		return nil, errors.New("invalid software bundle range")
	}
	// Catalog confinement is checked through os.Root, never an arbitrary caller
	// path. Verified open files are bounded and reused rather than recopied.
	assets, err := loadSoftwareBundle(directory)
	if err != nil {
		return nil, err
	}
	var selected *softwareBundleAsset
	for _, asset := range assets {
		if asset.ID == request.AssetID {
			copy := asset
			selected = &copy
			break
		}
	}
	if selected == nil || selected.Manifest["sha256"] != request.SHA256 {
		return nil, errors.New("software bundle identity changed")
	}
	key := directory + "\x00" + request.AssetID + "\x00" + request.SHA256
	bundleCache.Lock()
	defer bundleCache.Unlock()
	image, ok := bundleCache.files[key]
	if !ok {
		root, err := os.OpenRoot(directory)
		if err != nil {
			return nil, err
		}
		file, err := root.Open(selected.Path)
		root.Close()
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		size, err := io.Copy(hash, io.LimitReader(file, (512<<20)+1))
		if err != nil || hex.EncodeToString(hash.Sum(nil)) != request.SHA256 || size != int64(selected.Manifest["size"].(float64)) {
			file.Close()
			return nil, errors.New("software bundle size or SHA-256 mismatch")
		}
		if len(bundleCache.files) >= 16 {
			for id, old := range bundleCache.files {
				old.file.Close()
				delete(bundleCache.files, id)
				break
			}
		}
		image = bundleImage{file: file, size: size, hash: request.SHA256}
		bundleCache.files[key] = image
	}
	if request.Offset > image.size {
		return nil, errors.New("software bundle offset exceeds size")
	}
	data := make([]byte, min(int64(request.Length), image.size-request.Offset))
	n, err := image.file.ReadAt(data, request.Offset)
	if err != nil && !(err == io.EOF && n == len(data)) {
		return nil, err
	}
	if n != len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	return map[string]any{"sha256": image.hash, "size": image.size, "offset": request.Offset, "next_offset": request.Offset + int64(n), "data": base64.StdEncoding.EncodeToString(data), "eof": request.Offset+int64(n) == image.size}, nil
}

func dispatchSoftwareBundle(method string, raw json.RawMessage) (any, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	directory := filepath.Dir(executable)
	if method == "controller.software.bundle.list" {
		return softwareBundleList(directory)
	}
	var request softwareBundleRead
	if err = decodeStrictParams(raw, &request); err != nil {
		return nil, err
	}
	return softwareBundleReadChunk(directory, request)
}
