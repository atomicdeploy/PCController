// Package installer implements the hash-bound, per-user host installation
// lifecycle. Package validation is intentionally independent from serial,
// configuration, IPC, and UI startup.
package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"

	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/pathguard"
	"pccontroller.local/controller/internal/productidentity"
)

const (
	PackageManifestName   = "installation-package.json"
	packageManifestFormat = "pccontroller-installation-package/v1"
	hostManifestFormat    = "pccontroller-host-package-manifest/v1"
	maximumPackageFiles   = 4096
	maximumManifestBytes  = 4 << 20
)

// PackageFile binds one installable relative path to its exact bytes. Only
// entries in this list are copied; unrelated archive documentation is not
// granted executable installation scope.
type PackageFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type PackageTarget struct {
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
}

// PackageManifest is both the release package inventory and the installed
// package identity. RootSHA256 hashes every identity field and ordered file
// record, so state cannot accidentally combine files from different builds.
type PackageManifest struct {
	Format             string        `json:"format"`
	ProductAppID       string        `json:"product_app_id"`
	Version            string        `json:"version"`
	SourceSHA256       string        `json:"source_sha256"`
	BuildTime          string        `json:"build_time"`
	Target             PackageTarget `json:"target"`
	ExecutablePath     string        `json:"executable_path"`
	HostManifestPath   string        `json:"host_manifest_path"`
	HostManifestSHA256 string        `json:"host_manifest_sha256"`
	Files              []PackageFile `json:"files"`
	RootSHA256         string        `json:"root_sha256"`
}

type hostPackageManifest struct {
	Format       string `json:"format"`
	GeneratedUTC string `json:"generatedUtc,omitempty"`
	Target       struct {
		Platform     string `json:"platform"`
		Architecture string `json:"architecture"`
	} `json:"target"`
	Identity struct {
		Version                 string `json:"version"`
		AppName                 string `json:"appName"`
		Tagline                 string `json:"tagline"`
		SourceSHA256            string `json:"sourceSHA256"`
		SourceFiles             int    `json:"sourceFiles"`
		BuildTime               string `json:"buildTime"`
		PackedFirmwareTimestamp string `json:"packedFirmwareTimestamp,omitempty"`
	} `json:"identity"`
	Toolchains json.RawMessage `json:"toolchains,omitempty"`
	Validation struct {
		WindowsResources string          `json:"windowsResources"`
		WebUI            json.RawMessage `json:"webUI"`
		EmbeddedDefaults json.RawMessage `json:"embeddedDefaults,omitempty"`
		Notices          json.RawMessage `json:"notices,omitempty"`
		Tests            string          `json:"tests,omitempty"`
		Vet              string          `json:"vet,omitempty"`
		UPX              json.RawMessage `json:"upx,omitempty"`
		SharedLibrary    string          `json:"sharedLibrary,omitempty"`
	} `json:"validation"`
	Artifacts []struct {
		Path   string `json:"path"`
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"artifacts"`
}

type packageVerifier func(string, PackageManifest, hostPackageManifest) error

// ManifestOptions supports deterministic build tooling and focused tests.
// Production callers leave Platform, Architecture, and VerifyExecutable empty.
type ManifestOptions struct {
	Platform         string
	Architecture     string
	VerifyExecutable packageVerifier
}

// GeneratePackageManifest validates the host build manifest, hashes all files
// currently in packageRoot, and atomically emits a deterministic inventory.
func GeneratePackageManifest(packageRoot, outputPath string, options ManifestOptions) (PackageManifest, error) {
	root, err := secureDirectory(packageRoot)
	if err != nil {
		return PackageManifest{}, err
	}
	output, err := filepath.Abs(strings.TrimSpace(outputPath))
	if err != nil {
		return PackageManifest{}, fmt.Errorf("resolve package inventory path: %w", err)
	}
	if !pathWithin(root, output) || filepath.Clean(output) == root {
		return PackageManifest{}, errors.New("package inventory output must be a file inside the package directory")
	}
	hostPath := filepath.Join(root, "host-manifest.json")
	hostContent, err := readBoundedRegularFile(hostPath, maximumManifestBytes)
	if err != nil {
		return PackageManifest{}, fmt.Errorf("read host package manifest: %w", err)
	}
	var host hostPackageManifest
	if err := decodeStrictJSON(hostContent, &host); err != nil {
		return PackageManifest{}, fmt.Errorf("decode host package manifest: %w", err)
	}
	if err := validateHostManifest(host); err != nil {
		return PackageManifest{}, err
	}
	platform := strings.TrimSpace(options.Platform)
	if platform == "" {
		platform = runtime.GOOS
	}
	architecture := strings.TrimSpace(options.Architecture)
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	if platform != "windows" {
		return PackageManifest{}, fmt.Errorf("%w: installation package inventory is available only for Windows hosts", ErrUnsupportedPlatform)
	}
	if normalizeHostPlatform(host.Target.Platform) != platform || host.Target.Architecture != architecture {
		return PackageManifest{}, fmt.Errorf(
			"host package target %s/%s does not match inventory target %s/%s",
			host.Target.Platform, host.Target.Architecture, platform, architecture,
		)
	}

	files, err := inventoryFiles(root, output)
	if err != nil {
		return PackageManifest{}, err
	}
	executable := findHostExecutable(host)
	if executable == "" {
		return PackageManifest{}, errors.New("host manifest does not declare controller.exe")
	}
	manifest := PackageManifest{
		Format: packageManifestFormat, ProductAppID: productidentity.StableAppID,
		Version: host.Identity.Version, SourceSHA256: strings.ToLower(host.Identity.SourceSHA256),
		BuildTime:      host.Identity.BuildTime,
		Target:         PackageTarget{Platform: platform, Architecture: architecture},
		ExecutablePath: executable, HostManifestPath: "host-manifest.json",
		HostManifestSHA256: digestBytes(hostContent), Files: files,
	}
	manifest.RootSHA256, err = manifestDigest(manifest)
	if err != nil {
		return PackageManifest{}, err
	}
	if err := validatePackageManifest(manifest); err != nil {
		return PackageManifest{}, err
	}
	if err := validateHostArtifacts(manifest, host); err != nil {
		return PackageManifest{}, err
	}
	verifier := options.VerifyExecutable
	if verifier == nil {
		verifier = verifyWindowsExecutableResources
	}
	if err := verifier(filepath.Join(root, filepath.FromSlash(executable)), manifest, host); err != nil {
		return PackageManifest{}, fmt.Errorf("verify packaged executable identity: %w", err)
	}
	if err := writeJSONAtomic(output, manifest, 0o644); err != nil {
		return PackageManifest{}, fmt.Errorf("write package inventory: %w", err)
	}
	return manifest, nil
}

// VerifyPackage loads the release-generated inventory and verifies every
// declared byte before an installation transaction may stage it.
func VerifyPackage(packageRoot, expectedRootSHA256 string, options ManifestOptions) (PackageManifest, error) {
	root, err := secureDirectory(packageRoot)
	if err != nil {
		return PackageManifest{}, err
	}
	content, err := readBoundedRegularFile(filepath.Join(root, PackageManifestName), maximumManifestBytes)
	if err != nil {
		return PackageManifest{}, fmt.Errorf("read installation package inventory: %w", err)
	}
	var manifest PackageManifest
	if err := decodeStrictJSON(content, &manifest); err != nil {
		return PackageManifest{}, fmt.Errorf("decode installation package inventory: %w", err)
	}
	if err := validatePackageManifest(manifest); err != nil {
		return PackageManifest{}, err
	}
	expected := strings.TrimSpace(expectedRootSHA256)
	if expected != "" {
		normalized, normalizeErr := normalizeDigest(expected)
		if normalizeErr != nil {
			return PackageManifest{}, fmt.Errorf("expected inventory digest: %w", normalizeErr)
		}
		if !strings.EqualFold(normalized, manifest.RootSHA256) {
			return PackageManifest{}, fmt.Errorf("package inventory digest is %s, expected %s", manifest.RootSHA256, normalized)
		}
	}
	platform := strings.TrimSpace(options.Platform)
	if platform == "" {
		platform = runtime.GOOS
	}
	architecture := strings.TrimSpace(options.Architecture)
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	if platform != "windows" {
		return PackageManifest{}, fmt.Errorf("%w: per-user installation is available only on Windows", ErrUnsupportedPlatform)
	}
	if manifest.Target.Platform != platform || manifest.Target.Architecture != architecture {
		return PackageManifest{}, fmt.Errorf("package target %s/%s does not match this host %s/%s", manifest.Target.Platform, manifest.Target.Architecture, platform, architecture)
	}
	for _, entry := range manifest.Files {
		path, pathErr := inventoryEntryPath(root, entry.Path)
		if pathErr != nil {
			return PackageManifest{}, pathErr
		}
		if err := verifyRegularFile(path, entry.Bytes, entry.SHA256); err != nil {
			return PackageManifest{}, fmt.Errorf("verify package file %s: %w", entry.Path, err)
		}
	}
	hostContent, err := readBoundedRegularFile(filepath.Join(root, filepath.FromSlash(manifest.HostManifestPath)), maximumManifestBytes)
	if err != nil {
		return PackageManifest{}, err
	}
	if digestBytes(hostContent) != manifest.HostManifestSHA256 {
		return PackageManifest{}, errors.New("host package manifest digest differs from installation inventory")
	}
	var host hostPackageManifest
	if err := decodeStrictJSON(hostContent, &host); err != nil {
		return PackageManifest{}, err
	}
	if err := validateHostManifest(host); err != nil {
		return PackageManifest{}, err
	}
	if err := validateHostArtifacts(manifest, host); err != nil {
		return PackageManifest{}, err
	}
	if host.Identity.Version != manifest.Version || !strings.EqualFold(host.Identity.SourceSHA256, manifest.SourceSHA256) || host.Identity.BuildTime != manifest.BuildTime || normalizeHostPlatform(host.Target.Platform) != manifest.Target.Platform || host.Target.Architecture != manifest.Target.Architecture {
		return PackageManifest{}, errors.New("host manifest identity differs from installation inventory")
	}
	verifier := options.VerifyExecutable
	if verifier == nil {
		verifier = verifyWindowsExecutableResources
	}
	if err := verifier(filepath.Join(root, filepath.FromSlash(manifest.ExecutablePath)), manifest, host); err != nil {
		return PackageManifest{}, fmt.Errorf("verify packaged executable identity: %w", err)
	}
	return manifest, nil
}

func validatePackageManifest(manifest PackageManifest) error {
	if manifest.Format != packageManifestFormat || manifest.ProductAppID != productidentity.StableAppID {
		return errors.New("installation package format or product identity is unsupported")
	}
	if manifest.Target.Platform != "windows" || strings.TrimSpace(manifest.Target.Architecture) == "" {
		return errors.New("installation package target is invalid")
	}
	if strings.TrimSpace(manifest.Version) == "" || strings.TrimSpace(manifest.BuildTime) == "" {
		return errors.New("installation package version or build time is missing")
	}
	if _, err := normalizeDigest(manifest.SourceSHA256); err != nil {
		return fmt.Errorf("source digest: %w", err)
	}
	if _, err := normalizeDigest(manifest.HostManifestSHA256); err != nil {
		return fmt.Errorf("host manifest digest: %w", err)
	}
	if len(manifest.Files) == 0 || len(manifest.Files) > maximumPackageFiles {
		return errors.New("installation package file count is invalid")
	}
	seen := make(map[string]bool, len(manifest.Files))
	previous := ""
	for _, entry := range manifest.Files {
		normalized, err := normalizeInventoryPath(entry.Path)
		if err != nil || normalized != entry.Path || entry.Bytes < 0 {
			return fmt.Errorf("installation package path %q is invalid", entry.Path)
		}
		folded := strings.ToLower(normalized)
		if seen[folded] {
			return fmt.Errorf("installation package path %q is duplicated case-insensitively", entry.Path)
		}
		seen[folded] = true
		if previous != "" && strings.Compare(previous, folded) >= 0 {
			return errors.New("installation package files are not in canonical order")
		}
		previous = folded
		if _, err := normalizeDigest(entry.SHA256); err != nil {
			return fmt.Errorf("installation package digest for %s: %w", entry.Path, err)
		}
	}
	for _, required := range []string{manifest.ExecutablePath, manifest.HostManifestPath} {
		normalized, err := normalizeInventoryPath(required)
		if err != nil || !seen[strings.ToLower(normalized)] {
			return fmt.Errorf("required installation file %q is not inventoried", required)
		}
	}
	expected, err := manifestDigest(manifest)
	if err != nil {
		return err
	}
	if !strings.EqualFold(expected, manifest.RootSHA256) {
		return fmt.Errorf("installation package root digest is %s, calculated %s", manifest.RootSHA256, expected)
	}
	return nil
}

func validateHostManifest(host hostPackageManifest) error {
	if host.Format != hostManifestFormat {
		return fmt.Errorf("unsupported host package manifest format %q", host.Format)
	}
	if normalizeHostPlatform(host.Target.Platform) != "windows" || strings.TrimSpace(host.Target.Architecture) == "" {
		return errors.New("host package is not a Windows target")
	}
	if strings.TrimSpace(host.Identity.Version) == "" || strings.TrimSpace(host.Identity.BuildTime) == "" {
		return errors.New("host package identity is incomplete")
	}
	if host.Identity.SourceFiles <= 0 {
		return errors.New("host package source-file count is invalid")
	}
	if _, err := normalizeDigest(host.Identity.SourceSHA256); err != nil {
		return fmt.Errorf("host source digest: %w", err)
	}
	if host.Validation.WindowsResources != "verified" {
		return errors.New("host package does not attest verified Windows resources")
	}
	if len(bytes.TrimSpace(host.Validation.WebUI)) == 0 || bytes.Equal(bytes.TrimSpace(host.Validation.WebUI), []byte("null")) {
		return errors.New("host package does not record embedded WebUI validation")
	}
	if len(host.Artifacts) == 0 || len(host.Artifacts) > maximumPackageFiles {
		return errors.New("host package artifact list is invalid")
	}
	for _, artifact := range host.Artifacts {
		if _, err := normalizeInventoryPath(artifact.Path); err != nil {
			return fmt.Errorf("host artifact path %q is invalid", artifact.Path)
		}
		if artifact.Bytes < 0 {
			return fmt.Errorf("host artifact %q has an invalid size", artifact.Path)
		}
		if _, err := normalizeDigest(artifact.SHA256); err != nil {
			return fmt.Errorf("host artifact %q: %w", artifact.Path, err)
		}
	}
	return nil
}

func normalizeHostPlatform(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "win32") {
		return "windows"
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func findHostExecutable(host hostPackageManifest) string {
	for _, artifact := range host.Artifacts {
		if strings.EqualFold(filepath.Base(filepath.FromSlash(artifact.Path)), "controller.exe") {
			return filepath.ToSlash(filepath.Clean(filepath.FromSlash(artifact.Path)))
		}
	}
	return ""
}

func validateHostArtifacts(manifest PackageManifest, host hostPackageManifest) error {
	files := make(map[string]PackageFile, len(manifest.Files))
	for _, file := range manifest.Files {
		files[strings.ToLower(file.Path)] = file
	}
	for _, artifact := range host.Artifacts {
		path, err := normalizeInventoryPath(artifact.Path)
		if err != nil {
			return err
		}
		file, exists := files[strings.ToLower(path)]
		if !exists || file.Bytes != artifact.Bytes || !strings.EqualFold(file.SHA256, artifact.SHA256) {
			return fmt.Errorf("host artifact %q does not match the installation inventory", artifact.Path)
		}
	}
	return nil
}

func inventoryFiles(root, excluded string) ([]PackageFile, error) {
	files := make([]PackageFile, 0, 32)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filepath.Clean(path) == filepath.Clean(excluded) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := pathguard.ValidateComponent(path, info); err != nil {
			return fmt.Errorf("package path is not trusted: %w", err)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package path %s is not a regular file", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative, err = normalizeInventoryPath(filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		digest, bytes, err := digestFile(path)
		if err != nil {
			return err
		}
		files = append(files, PackageFile{Path: relative, Bytes: bytes, SHA256: digest})
		if len(files) > maximumPackageFiles {
			return fmt.Errorf("package contains more than %d files", maximumPackageFiles)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inventory package: %w", err)
	}
	sort.Slice(files, func(left, right int) bool {
		return strings.ToLower(files[left].Path) < strings.ToLower(files[right].Path)
	})
	return files, nil
}

func manifestDigest(manifest PackageManifest) (string, error) {
	copy := manifest
	copy.RootSHA256 = ""
	content, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return digestBytes(content), nil
}

func verifyWindowsExecutableResources(path string, manifest PackageManifest, host hostPackageManifest) error {
	identity, err := artifacts.InspectHostExecutable(path)
	if err != nil {
		return err
	}
	if identity.OS != "windows" || identity.Arch != manifest.Target.Architecture {
		return fmt.Errorf("executable target %s does not match inventory target %s/%s", identity.Platform(), manifest.Target.Platform, manifest.Target.Architecture)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	resource, err := peVersionResource(content)
	if err != nil {
		return err
	}
	strings, err := parseVersionInfoStrings(resource)
	if err != nil {
		return err
	}
	return verifyWindowsResourceIdentity(strings, manifest, host)
}

// verifyWindowsResourceIdentity checks the immutable PE resource strings that
// remain inspectable when controller.exe is UPX-compressed.
func verifyWindowsResourceIdentity(values map[string]string, manifest PackageManifest, host hostPackageManifest) error {
	for label, value := range map[string]string{
		"product name":      productidentity.DefaultTitle,
		"product version":   host.Identity.Version,
		"original filename": "controller.exe",
		"source hash":       manifest.SourceSHA256,
		"build time":        manifest.BuildTime,
	} {
		key := map[string]string{
			"product name": "ProductName", "product version": "ProductVersion",
			"original filename": "OriginalFilename", "source hash": "PrivateBuild", "build time": "SpecialBuild",
		}[label]
		if values[key] != value {
			return fmt.Errorf("Windows version resource %s does not match declared %s", key, label)
		}
	}
	return nil
}

const (
	imageResourceTypeVersion uint32 = 16
	resourceDirectoryFlag    uint32 = 0x80000000
)

type peResourceLocation struct {
	rawOffset int
	virtual   uint32
	size      int
}

// peVersionResource reads precisely one RT_VERSION payload from the PE
// resource tree. It never scans arbitrary executable bytes for identity text.
func peVersionResource(content []byte) ([]byte, error) {
	location, err := parsePEResourceLocation(content)
	if err != nil {
		return nil, err
	}
	types, err := resourceDirectoryEntries(content, location, 0)
	if err != nil {
		return nil, err
	}
	var versionDirectories []uint32
	for _, entry := range types {
		if entry.id == imageResourceTypeVersion && entry.directory {
			versionDirectories = append(versionDirectories, entry.offset)
		}
	}
	if len(versionDirectories) != 1 {
		return nil, fmt.Errorf("PE resource tree must contain exactly one RT_VERSION directory, found %d", len(versionDirectories))
	}
	names, err := resourceDirectoryEntries(content, location, versionDirectories[0])
	if err != nil {
		return nil, err
	}
	var payloads []resourceEntry
	for _, name := range names {
		if name.id != 1 {
			return nil, fmt.Errorf("RT_VERSION directory has unsupported name id 0x%X", name.id)
		}
		if !name.directory {
			return nil, errors.New("RT_VERSION name entry is not a directory")
		}
		languages, err := resourceDirectoryEntries(content, location, name.offset)
		if err != nil {
			return nil, err
		}
		for _, language := range languages {
			if language.directory {
				return nil, errors.New("RT_VERSION language entry is unexpectedly a directory")
			}
			payloads = append(payloads, language)
		}
	}
	if len(payloads) != 1 {
		return nil, fmt.Errorf("PE resource tree must contain exactly one RT_VERSION payload, found %d", len(payloads))
	}
	entryOffset := location.rawOffset + int(payloads[0].offset)
	if entryOffset < location.rawOffset || entryOffset+16 > location.rawOffset+location.size || entryOffset+16 > len(content) {
		return nil, errors.New("RT_VERSION data entry is outside the resource section")
	}
	rva := binary.LittleEndian.Uint32(content[entryOffset : entryOffset+4])
	size := int(binary.LittleEndian.Uint32(content[entryOffset+4 : entryOffset+8]))
	if rva < location.virtual || size <= 0 {
		return nil, errors.New("RT_VERSION data entry has invalid bounds")
	}
	start := location.rawOffset + int(rva-location.virtual)
	if start < location.rawOffset || start > len(content)-size || start+size > location.rawOffset+location.size {
		return nil, errors.New("RT_VERSION payload is outside the resource section")
	}
	return content[start : start+size], nil
}

type resourceEntry struct {
	id        uint32
	offset    uint32
	directory bool
}

func resourceDirectoryEntries(content []byte, location peResourceLocation, relative uint32) ([]resourceEntry, error) {
	start := location.rawOffset + int(relative)
	if start < location.rawOffset || start+16 > location.rawOffset+location.size || start+16 > len(content) {
		return nil, errors.New("PE resource directory is outside the resource section")
	}
	count := int(binary.LittleEndian.Uint16(content[start+12:start+14])) + int(binary.LittleEndian.Uint16(content[start+14:start+16]))
	if count <= 0 || count > 1024 || start+16+count*8 > location.rawOffset+location.size || start+16+count*8 > len(content) {
		return nil, errors.New("PE resource directory has invalid entries")
	}
	entries := make([]resourceEntry, 0, count)
	for index := 0; index < count; index++ {
		offset := start + 16 + index*8
		name := binary.LittleEndian.Uint32(content[offset : offset+4])
		data := binary.LittleEndian.Uint32(content[offset+4 : offset+8])
		entries = append(entries, resourceEntry{id: name, offset: data &^ resourceDirectoryFlag, directory: data&resourceDirectoryFlag != 0})
	}
	return entries, nil
}

func parsePEResourceLocation(content []byte) (peResourceLocation, error) {
	if len(content) < 64 || content[0] != 'M' || content[1] != 'Z' {
		return peResourceLocation{}, errors.New("executable is not a PE image")
	}
	offset := int(binary.LittleEndian.Uint32(content[0x3c:0x40]))
	if offset < 64 || offset+24 > len(content) || string(content[offset:offset+4]) != "PE\x00\x00" {
		return peResourceLocation{}, errors.New("PE image has an invalid executable header")
	}
	sections := int(binary.LittleEndian.Uint16(content[offset+6 : offset+8]))
	optionalBytes := int(binary.LittleEndian.Uint16(content[offset+20 : offset+22]))
	table := offset + 24 + optionalBytes
	if sections <= 0 || table < 0 || table+sections*40 > len(content) {
		return peResourceLocation{}, errors.New("PE image has an invalid section table")
	}
	for index := 0; index < sections; index++ {
		header := content[table+index*40 : table+(index+1)*40]
		name := strings.TrimRight(string(header[:8]), "\x00")
		if name != ".rsrc" {
			continue
		}
		size := int(binary.LittleEndian.Uint32(header[16:20]))
		start := int(binary.LittleEndian.Uint32(header[20:24]))
		if size <= 0 || start < 0 || start > len(content)-size {
			return peResourceLocation{}, errors.New("PE resource section is outside the executable")
		}
		return peResourceLocation{rawOffset: start, virtual: binary.LittleEndian.Uint32(header[12:16]), size: size}, nil
	}
	return peResourceLocation{}, errors.New("PE executable has no .rsrc section")
}

// parseVersionInfoStrings decodes the canonical VS_VERSIONINFO hierarchy and
// rejects duplicate string keys so an executable cannot carry ambiguous IDs.
func parseVersionInfoStrings(content []byte) (map[string]string, error) {
	root, err := parseVersionBlock(content, 0, len(content))
	if err != nil {
		return nil, fmt.Errorf("parse VS_VERSIONINFO: %w", err)
	}
	if root.key != "VS_VERSION_INFO" {
		return nil, errors.New("RT_VERSION payload is not VS_VERSION_INFO")
	}
	values := make(map[string]string)
	var walk func(versionBlock) error
	walk = func(block versionBlock) error {
		if block.key == "StringFileInfo" {
			for _, table := range block.children {
				for _, entry := range table.children {
					if entry.value == "" {
						return fmt.Errorf("VERSIONINFO string %q is empty", entry.key)
					}
					if _, exists := values[entry.key]; exists {
						return fmt.Errorf("VERSIONINFO has duplicate string key %q", entry.key)
					}
					values[entry.key] = entry.value
				}
			}
		}
		for _, child := range block.children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.New("VS_VERSIONINFO has no StringFileInfo values")
	}
	return values, nil
}

type versionBlock struct {
	key      string
	value    string
	children []versionBlock
}

func parseVersionBlock(content []byte, start, limit int) (versionBlock, error) {
	if start < 0 || start+6 > limit || limit > len(content) {
		return versionBlock{}, errors.New("VERSIONINFO block header is truncated")
	}
	length := int(binary.LittleEndian.Uint16(content[start : start+2]))
	valueLength := int(binary.LittleEndian.Uint16(content[start+2 : start+4]))
	typeCode := binary.LittleEndian.Uint16(content[start+4 : start+6])
	end := start + length
	if length < 6 || end > limit {
		return versionBlock{}, errors.New("VERSIONINFO block length is invalid")
	}
	keyEnd := start + 6
	for {
		if keyEnd+2 > end {
			return versionBlock{}, errors.New("VERSIONINFO key is unterminated")
		}
		if binary.LittleEndian.Uint16(content[keyEnd:keyEnd+2]) == 0 {
			break
		}
		keyEnd += 2
	}
	key, err := decodeUTF16(content[start+6 : keyEnd])
	if err != nil || key == "" {
		return versionBlock{}, errors.New("VERSIONINFO key is invalid")
	}
	valueStart := align4(keyEnd + 2)
	if valueStart > end {
		return versionBlock{}, errors.New("VERSIONINFO value is outside its block")
	}
	valueBytes := valueLength
	if typeCode == 1 {
		valueBytes *= 2
	}
	if valueBytes < 0 || valueStart+valueBytes > end {
		return versionBlock{}, errors.New("VERSIONINFO value length is invalid")
	}
	block := versionBlock{key: key}
	if typeCode == 1 && valueBytes > 0 {
		block.value, err = decodeUTF16(content[valueStart : valueStart+valueBytes])
		if err != nil {
			return versionBlock{}, err
		}
		block.value = strings.TrimRight(block.value, "\x00")
	}
	child := align4(valueStart + valueBytes)
	for child < end {
		if child+2 > end || binary.LittleEndian.Uint16(content[child:child+2]) == 0 {
			break
		}
		parsed, err := parseVersionBlock(content, child, end)
		if err != nil {
			return versionBlock{}, err
		}
		block.children = append(block.children, parsed)
		length := int(binary.LittleEndian.Uint16(content[child : child+2]))
		child = align4(child + length)
	}
	return block, nil
}

func align4(value int) int { return (value + 3) &^ 3 }

func decodeUTF16(content []byte) (string, error) {
	if len(content)%2 != 0 {
		return "", errors.New("UTF-16 string has odd length")
	}
	units := make([]uint16, len(content)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(content[index*2 : index*2+2])
	}
	return string(utf16.Decode(units)), nil
}

func utf16Bytes(value string) []byte {
	units := utf16.Encode([]rune(value))
	result := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(result[index*2:], unit)
	}
	return result
}

func normalizeInventoryPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || strings.HasPrefix(value, "/") || filepath.IsAbs(value) || strings.ContainsRune(value, '\x00') ||
		!filepath.IsLocal(filepath.FromSlash(value)) {
		return "", errors.New("inventory path must be relative")
	}
	if err := pathguard.ValidateRelative(filepath.FromSlash(value)); err != nil {
		return "", fmt.Errorf("inventory path is not local: %w", err)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", errors.New("inventory path escapes the package")
	}
	return clean, nil
}

func inventoryEntryPath(root, relative string) (string, error) {
	normalized, err := normalizeInventoryPath(relative)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, filepath.FromSlash(normalized))
	if !pathWithin(root, path) {
		return "", errors.New("inventory path escapes the package")
	}
	return path, nil
}

func secureDirectory(path string) (string, error) {
	path, err := pathguard.CleanAbsolute(path)
	if err != nil {
		return "", err
	}
	if err := pathguard.ValidateComponents(path, false); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("package root must be a real directory, not a link")
	}
	return filepath.Clean(path), nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func readBoundedRegularFile(path string, maximum int64) ([]byte, error) {
	file, info, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info.Size() < 0 || info.Size() > maximum {
		return nil, errors.New("file is not a bounded regular file")
	}
	return io.ReadAll(io.LimitReader(file, maximum+1))
}

func digestFile(path string) (string, int64, error) {
	file, info, err := openRegularFile(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	if written != info.Size() {
		return "", 0, errors.New("file changed while hashing")
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func openRegularFile(path string) (*os.File, os.FileInfo, error) {
	if err := pathguard.ValidateComponents(path, false); err != nil {
		return nil, nil, err
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("path is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		_ = file.Close()
		return nil, nil, errors.New("regular file changed while opening")
	}
	if err := pathguard.ValidateComponents(path, false); err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, openedInfo, nil
}

func verifyRegularFile(path string, expectedBytes int64, expectedSHA256 string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a regular file")
	}
	if info.Size() != expectedBytes {
		return fmt.Errorf("size is %d, expected %d", info.Size(), expectedBytes)
	}
	digest, bytes, err := digestFile(path)
	if err != nil {
		return err
	}
	if bytes != expectedBytes || !strings.EqualFold(digest, expectedSHA256) {
		return fmt.Errorf("SHA-256 is %s, expected %s", digest, expectedSHA256)
	}
	return nil
}

func normalizeDigest(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("SHA-256 must be exactly 64 hexadecimal characters")
	}
	return value, nil
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func decodeStrictJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("JSON contains trailing data")
	}
	return nil
}
