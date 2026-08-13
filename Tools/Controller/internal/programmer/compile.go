package programmer

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var firmwareDomainSources = []string{"LocalLib", "Project", "src"}

// CompileIdentity describes the deterministic source identity and isolated
// Arduino paths selected for one controller-owned firmware build.
type CompileIdentity struct {
	SourceHash      uint32
	SourceSHA256    string
	SourceFiles     int
	Features        []FirmwareFeature
	PackedTimestamp uint32
	SourceRoot      string
	SketchPath      string
	BuildPath       string
	OutputDir       string
}

// PlanCompile hashes only curated firmware sources and chooses out-of-tree,
// source-keyed Arduino paths. It is read-only, so dry-run remains a true dry-run.
func PlanCompile(options Options) (Options, CompileIdentity, error) {
	if options.Method != MethodCompile {
		return options, CompileIdentity{}, errors.New("compile planning requires method compile")
	}
	featureValues := firmwareFeatureNames(options.FirmwareFeatures)
	features, err := NormalizeFirmwareFeatures(featureValues)
	if err != nil {
		return options, CompileIdentity{}, err
	}
	options.FirmwareFeatures = features
	if options.compilePlanned {
		return options, compileIdentity(options), nil
	}
	if strings.TrimSpace(options.SketchPath) == "" {
		return options, CompileIdentity{}, errors.New("compile requires a sketch path")
	}
	sourceRoot, err := filepath.Abs(options.SketchPath)
	if err != nil {
		return options, CompileIdentity{}, fmt.Errorf("resolve firmware source root: %w", err)
	}
	info, err := os.Stat(sourceRoot)
	if err != nil {
		return options, CompileIdentity{}, fmt.Errorf("inspect firmware source root: %w", err)
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Base(sourceRoot), "PCController.ino") {
			return options, CompileIdentity{}, errors.New("compile sketch path must be the project directory or PCController.ino")
		}
		sourceRoot = filepath.Dir(sourceRoot)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, "PCController.ino")); err != nil {
		return options, CompileIdentity{}, fmt.Errorf("firmware source root requires PCController.ino: %w", err)
	}

	sourceHash, sourceSHA256, sourceFiles, err := firmwareCompileInputDigest(sourceRoot, features)
	if err != nil {
		return options, CompileIdentity{}, err
	}
	packedTimestamp, err := requestedBuildTimestamp(time.Now())
	if err != nil {
		return options, CompileIdentity{}, err
	}
	cacheRoot, err := compileCacheRoot()
	if err != nil {
		return options, CompileIdentity{}, err
	}
	state := filepath.Join(cacheRoot, fmt.Sprintf("%08X", sourceHash))
	outputDir := strings.TrimSpace(options.OutputDir)
	if outputDir == "" {
		outputDir = filepath.Join(sourceRoot, ".build", "firmware")
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return options, CompileIdentity{}, fmt.Errorf("resolve firmware output directory: %w", err)
	}

	options.CompileSourceRoot = sourceRoot
	options.SketchPath = filepath.Join(state, "PCController")
	options.BuildPath = filepath.Join(state, "work")
	options.OutputDir = outputDir
	options.FirmwareSourceHash = sourceHash
	options.FirmwareSourceSHA256 = sourceSHA256
	options.FirmwareSourceFiles = sourceFiles
	options.FirmwareBuildTimestamp = packedTimestamp
	options.compilePlanned = true
	return options, compileIdentity(options), nil
}

// StageCompile materializes only the curated firmware files selected by
// PlanCompile. Repository children such as .cache and .build are never scanned.
func StageCompile(options Options) (Options, CompileIdentity, error) {
	planned, identity, err := PlanCompile(options)
	if err != nil {
		return options, CompileIdentity{}, err
	}
	currentHash, err := firmwareCompileInputHash(identity.SourceRoot, planned.FirmwareFeatures)
	if err != nil {
		return options, CompileIdentity{}, err
	}
	if currentHash != identity.SourceHash {
		return options, CompileIdentity{}, fmt.Errorf(
			"firmware sources changed after compile planning (planned %08X, current %08X); retry",
			identity.SourceHash,
			currentHash,
		)
	}
	for _, directory := range []string{identity.SketchPath, identity.BuildPath, identity.OutputDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return options, CompileIdentity{}, fmt.Errorf("create compile directory %s: %w", directory, err)
		}
	}
	topLevel, err := firmwareTopLevelSourceFiles(identity.SourceRoot)
	if err != nil {
		return options, CompileIdentity{}, err
	}
	for _, source := range topLevel {
		info, statErr := os.Stat(source)
		if statErr != nil {
			return options, CompileIdentity{}, fmt.Errorf("inspect firmware source %s: %w", source, statErr)
		}
		if err := copyCompileFile(
			source,
			filepath.Join(identity.SketchPath, filepath.Base(source)),
			info.Mode(),
		); err != nil {
			return options, CompileIdentity{}, err
		}
	}
	for _, name := range firmwareDomainSources {
		source := filepath.Join(identity.SourceRoot, name)
		info, statErr := os.Stat(source)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return options, CompileIdentity{}, fmt.Errorf("inspect firmware domain %s: %w", source, statErr)
		}
		if !info.IsDir() {
			continue
		}
		if err := copyCompileTree(source, filepath.Join(identity.SketchPath, name)); err != nil {
			return options, CompileIdentity{}, err
		}
	}
	stagedHash, err := firmwareCompileInputHash(identity.SketchPath, planned.FirmwareFeatures)
	if err != nil {
		return options, CompileIdentity{}, fmt.Errorf("verify staged firmware sources: %w", err)
	}
	if stagedHash != identity.SourceHash {
		return options, CompileIdentity{}, fmt.Errorf(
			"staged firmware hash %08X does not match planned source hash %08X",
			stagedHash,
			identity.SourceHash,
		)
	}
	planned.compileStaged = true
	return planned, identity, nil
}

func compileIdentity(options Options) CompileIdentity {
	return CompileIdentity{
		SourceHash: options.FirmwareSourceHash, PackedTimestamp: options.FirmwareBuildTimestamp,
		SourceSHA256: options.FirmwareSourceSHA256, SourceFiles: options.FirmwareSourceFiles,
		Features:   append([]FirmwareFeature(nil), options.FirmwareFeatures...),
		SourceRoot: options.CompileSourceRoot, SketchPath: options.SketchPath,
		BuildPath: options.BuildPath, OutputDir: options.OutputDir,
	}
}

func compileCacheRoot() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("PCCONTROLLER_ARDUINO_CACHE")); configured != "" {
		root, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolve PCCONTROLLER_ARDUINO_CACHE: %w", err)
		}
		return root, nil
	}
	root, err := os.UserCacheDir()
	if err == nil && strings.TrimSpace(root) != "" {
		return filepath.Join(root, "PCController", "ArduinoBuild"), nil
	}
	root = os.TempDir()
	if strings.TrimSpace(root) == "" {
		return "", errors.New("neither a user cache directory nor temporary directory is available")
	}
	return filepath.Join(root, "PCController-ArduinoBuild"), nil
}

func requestedBuildTimestamp(now time.Time) (uint32, error) {
	provided := strings.TrimSpace(os.Getenv("PCCONTROLLER_BUILD_TIMESTAMP"))
	if provided != "" {
		provided = strings.TrimPrefix(strings.TrimPrefix(provided, "0x"), "0X")
		if len(provided) == 0 || len(provided) > 8 {
			return 0, errors.New("PCCONTROLLER_BUILD_TIMESTAMP must be one to eight hexadecimal digits")
		}
		value, err := strconv.ParseUint(provided, 16, 32)
		if err != nil {
			return 0, errors.New("PCCONTROLLER_BUILD_TIMESTAMP must be one to eight hexadecimal digits")
		}
		return uint32(value), nil
	}
	year := now.Year() - 2000
	if year < 0 || year > 127 {
		return 0, fmt.Errorf("firmware build year %d is outside 2000..2127", now.Year())
	}
	date := uint32(year<<9 | int(now.Month())<<5 | now.Day())
	clock := uint32(now.Hour()<<11 | now.Minute()<<5 | now.Second()>>1)
	return date<<16 | clock, nil
}

func firmwareSourceHash(root string) (uint32, error) {
	hash, _, _, err := firmwareSourceDigest(root)
	return hash, err
}

// firmwareCompileInputHash includes the reviewed feature selection because it
// changes the generated image despite leaving source files untouched.
func firmwareCompileInputHash(root string, features []FirmwareFeature) (uint32, error) {
	hash, _, _, err := firmwareCompileInputDigest(root, features)
	return hash, err
}

// firmwareCompileInputDigest preserves the historic source-only digest for a
// feature-off build. Enabled feature names are then added canonically, so an
// image cannot be mistaken for the same source compiled with different gates.
func firmwareCompileInputDigest(root string, features []FirmwareFeature) (uint32, string, int, error) {
	sourceHash, sourceSHA256, sourceFiles, err := firmwareSourceDigest(root)
	if err != nil || len(features) == 0 {
		return sourceHash, sourceSHA256, sourceFiles, err
	}
	manifest := sha256.New()
	_, _ = fmt.Fprintf(manifest, "pccontroller-avr-compile-input/v1\nsource-sha256:%s\n", sourceSHA256)
	for _, feature := range features {
		_, _ = fmt.Fprintf(manifest, "feature:%s\n", feature)
	}
	digest := manifest.Sum(nil)
	return binary.BigEndian.Uint32(digest[:4]), fmt.Sprintf("%x", digest), sourceFiles, nil
}

func firmwareSourceDigest(root string) (uint32, string, int, error) {
	files, err := firmwareSourceFiles(root)
	if err != nil {
		return 0, "", 0, err
	}
	manifest := sha256.New()
	for _, source := range files {
		content, readErr := os.Open(source.absolute)
		if readErr != nil {
			return 0, "", 0, fmt.Errorf("open firmware source %s: %w", source.absolute, readErr)
		}
		fileDigest := sha256.New()
		_, copyErr := io.Copy(fileDigest, content)
		closeErr := content.Close()
		if copyErr != nil {
			return 0, "", 0, fmt.Errorf("hash firmware source %s: %w", source.absolute, copyErr)
		}
		if closeErr != nil {
			return 0, "", 0, fmt.Errorf("close firmware source %s: %w", source.absolute, closeErr)
		}
		_, _ = fmt.Fprintf(manifest, "%s:%X\n", source.relative, fileDigest.Sum(nil))
	}
	digest := manifest.Sum(nil)
	return binary.BigEndian.Uint32(digest[:4]), fmt.Sprintf("%x", digest), len(files), nil
}

type firmwareSource struct {
	absolute string
	relative string
}

func firmwareSourceFiles(root string) ([]firmwareSource, error) {
	var files []firmwareSource
	appendFile := func(path string) error {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, firmwareSource{absolute: path, relative: filepath.ToSlash(relative)})
		return nil
	}
	topLevel, err := firmwareTopLevelSourceFiles(root)
	if err != nil {
		return nil, err
	}
	for _, path := range topLevel {
		if err := appendFile(path); err != nil {
			return nil, err
		}
	}
	for _, name := range firmwareDomainSources {
		base := filepath.Join(root, name)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("inspect firmware domain %s: %w", base, err)
		}
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !firmwareSourceExtension(filepath.Ext(entry.Name())) {
				return nil
			}
			return appendFile(path)
		})
		if err != nil {
			return nil, fmt.Errorf("enumerate firmware domain %s: %w", base, err)
		}
	}
	if len(files) == 0 {
		return nil, errors.New("no firmware source files were found for build hashing")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].relative < files[j].relative })
	return files, nil
}

// firmwareTopLevelSourceFiles includes future sketch-domain modules without
// recursively admitting build caches, tools, documentation, or artifacts.
func firmwareTopLevelSourceFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("enumerate top-level firmware sources: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !firmwareSourceExtension(filepath.Ext(entry.Name())) {
			continue
		}
		files = append(files, filepath.Join(root, entry.Name()))
	}
	sort.Slice(files, func(left, right int) bool {
		return filepath.Base(files[left]) < filepath.Base(files[right])
	})
	return files, nil
}

func firmwareSourceExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".ino", ".h", ".hpp", ".c", ".cpp":
		return true
	default:
		return false
	}
}

func copyCompileTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyCompileFile(path, target, info.Mode())
	})
}

func copyCompileFile(source, destination string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open staged firmware source %s: %w", source, err)
	}
	defer input.Close()
	temporary := destination + ".tmp"
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return fmt.Errorf("create staged firmware source %s: %w", temporary, err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("copy staged firmware source %s: %w", source, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("close staged firmware source %s: %w", temporary, closeErr)
	}
	if err := os.Rename(temporary, destination); err != nil {
		// Windows cannot atomically replace an existing file with os.Rename.
		if removeErr := os.Remove(destination); removeErr != nil && !os.IsNotExist(removeErr) {
			_ = os.Remove(temporary)
			return fmt.Errorf("replace staged firmware source %s: %w", destination, removeErr)
		}
		if err = os.Rename(temporary, destination); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("install staged firmware source %s: %w", destination, err)
		}
	}
	return nil
}
