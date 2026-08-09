package runtimeinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	HostManifestFormat    = "pccontroller-host-package-manifest/v1"
	RuntimeManifestFormat = "pccontroller-linux-runtime-manifest/v1"
	DefaultRoot           = "/opt/pccontroller/runtime"
	DefaultStageRoot      = "/var/lib/pccontroller/runtime-input"
	maximumManifestBytes  = 1 << 20
	maximumBinaryBytes    = 256 << 20
	maximumSmokeOutput    = 32 << 10
)

type HostArtifact struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type HostManifest struct {
	Format string `json:"format"`
	Target struct {
		Platform     string `json:"platform"`
		Architecture string `json:"architecture"`
	} `json:"target"`
	Identity struct {
		Version      string `json:"version"`
		SourceSHA256 string `json:"sourceSHA256"`
		BuildTime    string `json:"buildTime"`
	} `json:"identity"`
	Artifacts []HostArtifact `json:"artifacts"`
}

type FileIdentity struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type ValidatedPackage struct {
	PackageDirectory string
	ManifestPath     string
	ManifestBytes    []byte
	Host             HostManifest
	Controller       FileIdentity
	VirtualBoard     FileIdentity
	controllerFile   *os.File
	virtualBoardFile *os.File
}

func (value *ValidatedPackage) Close() error {
	if value == nil {
		return nil
	}
	var failures []error
	if value.controllerFile != nil {
		failures = append(failures, value.controllerFile.Close())
		value.controllerFile = nil
	}
	if value.virtualBoardFile != nil {
		failures = append(failures, value.virtualBoardFile.Close())
		value.virtualBoardFile = nil
	}
	return errors.Join(failures...)
}

type CommandRunner func(
	ctx context.Context,
	name string,
	arguments []string,
	environment []string,
) (string, error)

type InstallOptions struct {
	Root         string
	TargetUser   string
	TargetUID    uint32
	TargetHome   string
	HostPackage  string
	VirtualBoard string
	Browser      string
	Runuser      string
	Systemctl    string
	Curl         string
	Apply        bool
	Now          time.Time
	Runner       CommandRunner
}

type OperationOptions struct {
	Root      string
	Apply     bool
	Runuser   string
	Systemctl string
	Curl      string
	Runner    CommandRunner
}

type StageOptions struct {
	Root               string
	SourcePackage      string
	SourceVirtualBoard string
	Apply              bool
}

type StageReport struct {
	Platform           string   `json:"platform"`
	Applied            bool     `json:"applied"`
	SourceRepository   string   `json:"source_repository"`
	SourcePackage      string   `json:"source_package"`
	SourceVirtualBoard string   `json:"source_virtual_board"`
	StageRoot          string   `json:"stage_root"`
	StageID            string   `json:"stage_id"`
	PackageDirectory   string   `json:"package_directory"`
	VirtualBoard       string   `json:"virtual_board"`
	ControllerSHA256   string   `json:"controller_sha256"`
	VirtualBoardSHA256 string   `json:"virtual_board_sha256"`
	Actions            []string `json:"actions,omitempty"`
}

type RuntimeManifest struct {
	Format                    string         `json:"format"`
	ReleaseID                 string         `json:"release_id"`
	GeneratedUTC              string         `json:"generated_utc"`
	Target                    string         `json:"target"`
	TargetUser                string         `json:"target_user"`
	TargetUID                 uint32         `json:"target_uid"`
	TargetHome                string         `json:"target_home"`
	HostVersion               string         `json:"host_version"`
	HostSourceSHA256          string         `json:"host_source_sha256"`
	Browser                   string         `json:"browser"`
	Files                     []FileIdentity `json:"files"`
	ExecutableSmokesPassed    bool           `json:"executable_smokes_passed"`
	UserDataPreservedOnRemove bool           `json:"user_data_preserved_on_remove"`
	PrivilegedDaemonInstalled bool           `json:"privileged_daemon_installed"`
}

type Report struct {
	Platform                  string   `json:"platform"`
	Operation                 string   `json:"operation"`
	Applied                   bool     `json:"applied"`
	Installed                 bool     `json:"installed"`
	ReleaseID                 string   `json:"release_id,omitempty"`
	PreviousReleaseID         string   `json:"previous_release_id,omitempty"`
	TargetUser                string   `json:"target_user,omitempty"`
	Root                      string   `json:"root"`
	Browser                   string   `json:"browser,omitempty"`
	PackageValidated          bool     `json:"package_validated,omitempty"`
	ExecutableSmokesPassed    bool     `json:"executable_smokes_passed,omitempty"`
	StableLinksReady          bool     `json:"stable_links_ready,omitempty"`
	UserUnitsReady            bool     `json:"user_units_ready,omitempty"`
	UserManager               string   `json:"user_manager,omitempty"`
	ArtifactsVerified         bool     `json:"artifacts_verified,omitempty"`
	RuntimeReady              bool     `json:"runtime_ready,omitempty"`
	UserDataPreserved         bool     `json:"user_data_preserved"`
	PrivilegedDaemonInstalled bool     `json:"privileged_daemon_installed"`
	Actions                   []string `json:"actions,omitempty"`
	Warnings                  []string `json:"warnings,omitempty"`
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
	seen   int
}

func (writer *boundedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	writer.seen += written
	remaining := writer.limit - writer.buffer.Len()
	if remaining > 0 {
		if remaining > len(value) {
			remaining = len(value)
		}
		_, _ = writer.buffer.Write(value[:remaining])
	}
	if writer.seen > writer.limit {
		return written, errors.New("process output exceeded the safety limit")
	}
	return written, nil
}

func (writer *boundedBuffer) String() string {
	return writer.buffer.String()
}

type smokeFunc func(context.Context, string, ...string) (string, error)

func validatePackageFor(
	ctx context.Context,
	packageDirectory, virtualBoard, platform, architecture string,
	smoke smokeFunc,
) (ValidatedPackage, error) {
	var result ValidatedPackage
	packageDirectory, err := cleanAbsolutePath(packageDirectory, "host package directory")
	if err != nil {
		return result, err
	}
	packageInfo, err := os.Lstat(packageDirectory)
	if err != nil {
		return result, fmt.Errorf("inspect host package directory: %w", err)
	}
	if !packageInfo.IsDir() || packageInfo.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("host package path must be a real directory, not a symlink")
	}
	manifestPath := filepath.Join(packageDirectory, "host-manifest.json")
	manifestBytes, err := readBoundedRegular(manifestPath, maximumManifestBytes)
	if err != nil {
		return result, fmt.Errorf("read host package manifest: %w", err)
	}
	var manifest HostManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	if err := decoder.Decode(&manifest); err != nil {
		return result, fmt.Errorf("decode host package manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return result, errors.New("host package manifest has trailing JSON values")
	}
	if manifest.Format != HostManifestFormat {
		return result, fmt.Errorf("unsupported host package manifest format %q", manifest.Format)
	}
	if manifest.Target.Platform != platform || manifest.Target.Architecture != architecture {
		return result, fmt.Errorf(
			"host package target is %s/%s, but this runtime requires %s/%s",
			manifest.Target.Platform, manifest.Target.Architecture, platform, architecture,
		)
	}
	if strings.TrimSpace(manifest.Identity.Version) == "" || !validSHA256(manifest.Identity.SourceSHA256) {
		return result, errors.New("host package manifest has an incomplete build identity")
	}
	controllerArtifact, err := selectControllerArtifact(manifest.Artifacts, platform)
	if err != nil {
		return result, err
	}
	controllerPath, err := packageArtifactPath(packageDirectory, controllerArtifact.Path)
	if err != nil {
		return result, err
	}
	controllerIdentity, err := validateExecutable("controller", controllerPath, controllerArtifact.Bytes, controllerArtifact.SHA256)
	if err != nil {
		return result, fmt.Errorf("validate Controller artifact: %w", err)
	}
	virtualBoard, err = cleanAbsolutePath(virtualBoard, "VirtualBoard executable")
	if err != nil {
		return result, err
	}
	virtualIdentity, err := validateExecutable("virtual-board", virtualBoard, 0, "")
	if err != nil {
		return result, fmt.Errorf("validate VirtualBoard artifact: %w", err)
	}
	if smoke == nil {
		return result, errors.New("runtime package validation requires a bounded executable smoke runner")
	}
	controllerOutput, err := smoke(ctx, controllerPath, "version")
	if err != nil {
		return result, fmt.Errorf("Controller bounded smoke: %w", err)
	}
	if err := validateControllerSmoke(controllerOutput, manifest); err != nil {
		return result, errors.New("Controller bounded smoke output does not match the host manifest identity")
	}
	boardOutput, err := smoke(ctx, virtualBoard, "--help")
	if err != nil {
		return result, fmt.Errorf("VirtualBoard bounded smoke: %w", err)
	}
	if !strings.Contains(boardOutput, "PCController Virtual Board") || !strings.Contains(boardOutput, "--port") {
		return result, errors.New("VirtualBoard bounded smoke did not expose the expected native helper identity")
	}
	result = ValidatedPackage{
		PackageDirectory: packageDirectory,
		ManifestPath:     manifestPath,
		ManifestBytes:    append([]byte(nil), manifestBytes...),
		Host:             manifest,
		Controller:       controllerIdentity,
		VirtualBoard:     virtualIdentity,
	}
	return result, nil
}

func decodeHostManifest(content []byte, platform, architecture string) (HostManifest, HostArtifact, error) {
	var manifest HostManifest
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, HostArtifact{}, fmt.Errorf("decode host package manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return manifest, HostArtifact{}, errors.New("host package manifest has trailing JSON values")
	}
	if manifest.Format != HostManifestFormat {
		return manifest, HostArtifact{}, fmt.Errorf("unsupported host package manifest format %q", manifest.Format)
	}
	if manifest.Target.Platform != platform || manifest.Target.Architecture != architecture {
		return manifest, HostArtifact{}, fmt.Errorf(
			"host package target is %s/%s, but this runtime requires %s/%s",
			manifest.Target.Platform, manifest.Target.Architecture, platform, architecture,
		)
	}
	if strings.TrimSpace(manifest.Identity.Version) == "" || !validSHA256(manifest.Identity.SourceSHA256) {
		return manifest, HostArtifact{}, errors.New("host package manifest has an incomplete build identity")
	}
	artifact, err := selectControllerArtifact(manifest.Artifacts, platform)
	return manifest, artifact, err
}

func validateControllerSmoke(output string, manifest HostManifest) error {
	fields := strings.Fields(output)
	versionFound := false
	hashFound := false
	for _, field := range fields {
		versionFound = versionFound || field == manifest.Identity.Version
		hashFound = hashFound || field == "source-hash="+manifest.Identity.SourceSHA256
	}
	if !versionFound || !hashFound {
		return errors.New("Controller version/source identity differs from the host manifest")
	}
	return nil
}

func selectControllerArtifact(artifacts []HostArtifact, platform string) (HostArtifact, error) {
	expected := "controller"
	if platform == "windows" {
		expected = "controller.exe"
	}
	var selected *HostArtifact
	for index := range artifacts {
		artifact := artifacts[index]
		if filepath.ToSlash(filepath.Clean(artifact.Path)) != expected {
			continue
		}
		if selected != nil {
			return HostArtifact{}, errors.New("host package manifest contains duplicate Controller artifacts")
		}
		selected = &artifact
	}
	if selected == nil {
		return HostArtifact{}, fmt.Errorf("host package manifest has no canonical %s artifact", expected)
	}
	if selected.Bytes <= 0 || !validSHA256(selected.SHA256) {
		return HostArtifact{}, errors.New("host package Controller artifact has an invalid size or SHA-256")
	}
	return *selected, nil
}

func packageArtifactPath(directory, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(name)))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("host package artifact path %q escapes its package directory", name)
	}
	path := filepath.Join(directory, clean)
	relative, err := filepath.Rel(directory, path)
	if err != nil || relative != clean {
		return "", fmt.Errorf("host package artifact path %q is not canonical", name)
	}
	return path, nil
}

func validateExecutable(role, path string, expectedBytes int64, expectedSHA string) (FileIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return FileIdentity{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return FileIdentity{}, errors.New("artifact must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return FileIdentity{}, errors.New("artifact is not executable")
	}
	if info.Size() <= 0 || info.Size() > maximumBinaryBytes {
		return FileIdentity{}, fmt.Errorf("artifact size %d is outside 1..%d bytes", info.Size(), maximumBinaryBytes)
	}
	if expectedBytes > 0 && info.Size() != expectedBytes {
		return FileIdentity{}, fmt.Errorf("artifact size is %d; manifest requires %d", info.Size(), expectedBytes)
	}
	digest, err := sha256File(path, maximumBinaryBytes)
	if err != nil {
		return FileIdentity{}, err
	}
	if expectedSHA != "" && !strings.EqualFold(digest, expectedSHA) {
		return FileIdentity{}, fmt.Errorf("artifact SHA-256 is %s; manifest requires %s", digest, expectedSHA)
	}
	return FileIdentity{Role: role, Path: path, Bytes: info.Size(), SHA256: digest}, nil
}

func smokeExecutable(parent context.Context, path string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	output := &boundedBuffer{limit: maximumSmokeOutput}
	command := exec.CommandContext(ctx, path, arguments...)
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output.String(), errors.New("process exceeded the five-second safety deadline")
	}
	if err != nil {
		return output.String(), fmt.Errorf("process failed: %w: %s", err, boundedText(output.String()))
	}
	return output.String(), nil
}

func readBoundedRegular(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > maximum {
		return nil, fmt.Errorf("file size %d is outside 1..%d bytes", info.Size(), maximum)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, errors.New("file grew beyond its safety limit while being read")
	}
	return content, nil
}

func sha256File(path string, maximum int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, maximum+1))
	if err != nil {
		return "", err
	}
	if written > maximum {
		return "", errors.New("artifact grew beyond its safety limit while being hashed")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func cleanAbsolutePath(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	absolute = filepath.Clean(absolute)
	if absolute == string(filepath.Separator) {
		return "", fmt.Errorf("%s cannot be the filesystem root", label)
	}
	return absolute, nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == sha256.Size
}

func boundedText(value string) string {
	value = strings.Join(strings.Fields(strings.ToValidUTF8(value, "")), " ")
	if len(value) > 512 {
		value = value[:512] + "..."
	}
	return value
}
