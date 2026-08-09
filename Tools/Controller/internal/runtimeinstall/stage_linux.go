//go:build linux

package runtimeinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	stageOwnerMarker = "pccontroller-linux-runtime-input/v1\n"
	stageFormat      = "pccontroller-linux-runtime-input-stage/v1"
)

type stageManifest struct {
	Format             string `json:"format"`
	StageID            string `json:"stage_id"`
	SourceRepository   string `json:"source_repository"`
	ControllerSHA256   string `json:"controller_sha256"`
	VirtualBoardSHA256 string `json:"virtual_board_sha256"`
}

// Stage pins the exact canonical repo outputs with O_NOFOLLOW and publishes
// those bytes into a root-owned trust boundary without executing either
// artifact. Runtime installation performs its target-UID smokes later.
func Stage(_ context.Context, options StageOptions) (StageReport, error) {
	root, err := runtimeRoot(defaultString(options.Root, DefaultStageRoot))
	report := StageReport{Platform: "linux", Applied: options.Apply, StageRoot: root}
	if err != nil {
		return report, err
	}
	validated, repository, err := validateCanonicalStageSource(options.SourcePackage, options.SourceVirtualBoard)
	if err != nil {
		return report, err
	}
	defer validated.Close()
	stageID := validated.Controller.SHA256 + "-" + validated.VirtualBoard.SHA256
	destination := filepath.Join(root, stageID)
	report.SourceRepository = repository
	report.SourcePackage = validated.PackageDirectory
	report.SourceVirtualBoard = validated.VirtualBoard.Path
	report.StageID = stageID
	report.PackageDirectory = filepath.Join(destination, "host")
	report.VirtualBoard = filepath.Join(destination, "virtual_board")
	report.ControllerSHA256 = validated.Controller.SHA256
	report.VirtualBoardSHA256 = validated.VirtualBoard.SHA256
	report.Actions = []string{
		"pin canonical repo package and VirtualBoard with O_NOFOLLOW",
		"validate Linux host manifest target, size, and hashes without executing source artifacts",
		"publish exact bytes into root-owned runtime input stage " + destination,
	}
	if !options.Apply {
		return report, nil
	}
	if root == DefaultStageRoot && os.Geteuid() != 0 {
		return report, errors.New("runtime input staging under /var/lib requires root; no state was changed")
	}
	lock, err := acquireStageLock(root)
	if err != nil {
		return report, err
	}
	defer lock.Close()
	if err := ensureStageRoot(root); err != nil {
		return report, err
	}
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return report, errors.New("existing runtime input stage is not a real directory")
		}
		if err := verifyStageDestination(destination, validated, repository); err != nil {
			return report, fmt.Errorf("verify existing runtime input stage: %w", err)
		}
		return report, nil
	} else if !os.IsNotExist(err) {
		return report, err
	}
	stage, err := os.MkdirTemp(root, ".input-stage-")
	if err != nil {
		return report, err
	}
	stageOwned := true
	defer func() {
		if stageOwned {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := os.Chmod(stage, 0o700); err != nil {
		return report, err
	}
	hostDirectory := filepath.Join(stage, "host")
	if err := os.Mkdir(hostDirectory, 0o700); err != nil {
		return report, err
	}
	if err := copyVerifiedExecutable(validated.controllerFile, validated.Controller, filepath.Join(hostDirectory, "controller")); err != nil {
		return report, err
	}
	if err := atomicWrite(filepath.Join(hostDirectory, "host-manifest.json"), validated.ManifestBytes, 0o644); err != nil {
		return report, err
	}
	if err := copyVerifiedExecutable(validated.virtualBoardFile, validated.VirtualBoard, filepath.Join(stage, "virtual_board")); err != nil {
		return report, err
	}
	metadata := stageManifest{
		Format: stageFormat, StageID: stageID, SourceRepository: repository,
		ControllerSHA256: validated.Controller.SHA256, VirtualBoardSHA256: validated.VirtualBoard.SHA256,
	}
	encoded, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return report, err
	}
	if err := atomicWrite(filepath.Join(stage, "stage-manifest.json"), append(encoded, '\n'), 0o644); err != nil {
		return report, err
	}
	for _, directory := range []string{hostDirectory, stage} {
		if err := os.Chmod(directory, 0o755); err != nil {
			return report, err
		}
	}
	if err := verifyStageDestination(stage, validated, repository); err != nil {
		return report, fmt.Errorf("verify staged runtime input: %w", err)
	}
	if err := os.Rename(stage, destination); err != nil {
		return report, fmt.Errorf("atomically publish runtime input stage: %w", err)
	}
	if err := syncDirectory(root); err != nil {
		_ = os.RemoveAll(destination)
		return report, fmt.Errorf("durably publish runtime input stage: %w", err)
	}
	stageOwned = false
	return report, nil
}

func validateCanonicalStageSource(packageDirectory, virtualBoard string) (ValidatedPackage, string, error) {
	var result ValidatedPackage
	packageDirectory, err := cleanAbsolutePath(packageDirectory, "source host package directory")
	if err != nil {
		return result, "", err
	}
	virtualBoard, err = cleanAbsolutePath(virtualBoard, "source VirtualBoard executable")
	if err != nil {
		return result, "", err
	}
	repository := filepath.Dir(filepath.Dir(filepath.Dir(packageDirectory)))
	if packageDirectory != filepath.Join(repository, "Tools", "Controller", "bin") ||
		virtualBoard != filepath.Join(repository, "Tools", "VirtualBoard", ".build", "release", "bin", "virtual_board") {
		return result, "", errors.New("runtime staging sources must be the exact Controller/bin and VirtualBoard release outputs in one PCController repo")
	}
	repositoryFD, err := openPinnedDirectory(repository, false)
	if err != nil {
		return result, "", fmt.Errorf("pin source repository ancestry: %w", err)
	}
	defer unix.Close(repositoryFD)
	var gitStatus unix.Stat_t
	if err := unix.Fstatat(repositoryFD, ".git", &gitStatus, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return result, "", fmt.Errorf("inspect pinned source repository marker: %w", err)
	}
	gitType := gitStatus.Mode & unix.S_IFMT
	if gitType != unix.S_IFDIR && gitType != unix.S_IFREG {
		return result, "", errors.New("pinned source repository .git marker is not a real file/directory")
	}
	packageFD, err := openPinnedRelativeDirectory(repositoryFD, "Tools/Controller/bin")
	if err != nil {
		return result, "", fmt.Errorf("pin canonical host package: %w", err)
	}
	defer unix.Close(packageFD)
	boardDirectoryFD, err := openPinnedRelativeDirectory(repositoryFD, "Tools/VirtualBoard/.build/release/bin")
	if err != nil {
		return result, "", fmt.Errorf("pin canonical VirtualBoard directory: %w", err)
	}
	defer unix.Close(boardDirectoryFD)
	manifestFile, err := openPinnedAt(packageFD, "host-manifest.json")
	if err != nil {
		return result, "", fmt.Errorf("pin host package manifest: %w", err)
	}
	manifestBytes, err := readPinnedRegular(manifestFile, maximumManifestBytes, false, false)
	_ = manifestFile.Close()
	if err != nil {
		return result, "", fmt.Errorf("read pinned host package manifest: %w", err)
	}
	manifest, artifact, err := decodeHostManifest(manifestBytes, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return result, "", err
	}
	if filepath.ToSlash(filepath.Clean(artifact.Path)) != "controller" {
		return result, "", errors.New("Linux host manifest Controller artifact must be the direct package child named controller")
	}
	controllerFile, err := openPinnedAt(packageFD, "controller")
	if err != nil {
		return result, "", fmt.Errorf("pin Controller artifact: %w", err)
	}
	controllerIdentity, err := identifyPinnedExecutable(controllerFile, "controller", filepath.Join(packageDirectory, "controller"), artifact.Bytes, artifact.SHA256, false)
	if err != nil {
		_ = controllerFile.Close()
		return result, "", fmt.Errorf("validate pinned Controller artifact: %w", err)
	}
	boardFile, err := openPinnedAt(boardDirectoryFD, "virtual_board")
	if err != nil {
		_ = controllerFile.Close()
		return result, "", fmt.Errorf("pin VirtualBoard artifact: %w", err)
	}
	boardIdentity, err := identifyPinnedExecutable(boardFile, "virtual-board", virtualBoard, 0, "", false)
	if err != nil {
		_ = controllerFile.Close()
		_ = boardFile.Close()
		return result, "", fmt.Errorf("validate pinned VirtualBoard artifact: %w", err)
	}
	result = ValidatedPackage{
		PackageDirectory: packageDirectory, ManifestPath: filepath.Join(packageDirectory, "host-manifest.json"),
		ManifestBytes: append([]byte(nil), manifestBytes...), Host: manifest,
		Controller: controllerIdentity, VirtualBoard: boardIdentity,
		controllerFile: controllerFile, virtualBoardFile: boardFile,
	}
	return result, repository, nil
}

func acquireStageLock(root string) (*os.File, error) {
	parent := filepath.Dir(root)
	if err := ensureDirectory(parent, 0o755, true); err != nil {
		return nil, err
	}
	path := filepath.Join(parent, ".pccontroller-runtime-stage.lock")
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func ensureStageRoot(root string) error {
	if err := ensureManagedDirectory(root, 0o755, true); err != nil {
		return err
	}
	marker := filepath.Join(root, ".pccontroller-runtime-input-root")
	if content, err := readBoundedRegular(marker, 256); err == nil {
		if string(content) != stageOwnerMarker {
			return errors.New("runtime input root ownership marker is not recognized")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWrite(marker, []byte(stageOwnerMarker), 0o644)
}

func verifyStageDestination(directory string, validated ValidatedPackage, repository string) error {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o755 {
		return errors.New("runtime input stage directory is not a real 0755 directory")
	}
	if os.Geteuid() == 0 {
		owner, ownerErr := fileUID(info)
		if ownerErr != nil || owner != 0 {
			return errors.New("runtime input stage directory is not root-owned")
		}
	}
	host := filepath.Join(directory, "host")
	hostInfo, err := os.Lstat(host)
	if err != nil || !hostInfo.IsDir() || hostInfo.Mode()&os.ModeSymlink != 0 || hostInfo.Mode().Perm() != 0o755 {
		return errors.New("runtime input host package is not a real 0755 directory")
	}
	if os.Geteuid() == 0 {
		owner, ownerErr := fileUID(hostInfo)
		if ownerErr != nil || owner != 0 {
			return errors.New("runtime input host package is not root-owned")
		}
	}
	expected := []struct {
		path   string
		mode   os.FileMode
		bytes  int64
		digest string
	}{
		{filepath.Join(host, "controller"), 0o755, validated.Controller.Bytes, validated.Controller.SHA256},
		{filepath.Join(host, "host-manifest.json"), 0o644, int64(len(validated.ManifestBytes)), sha256Bytes(validated.ManifestBytes)},
		{filepath.Join(directory, "virtual_board"), 0o755, validated.VirtualBoard.Bytes, validated.VirtualBoard.SHA256},
	}
	for _, item := range expected {
		itemInfo, err := os.Lstat(item.path)
		if err != nil || !itemInfo.Mode().IsRegular() || itemInfo.Mode()&os.ModeSymlink != 0 || itemInfo.Mode().Perm() != item.mode || itemInfo.Size() != item.bytes {
			return fmt.Errorf("staged input %s has unexpected type, mode, or size", item.path)
		}
		if os.Geteuid() == 0 {
			owner, ownerErr := fileUID(itemInfo)
			if ownerErr != nil || owner != 0 {
				return fmt.Errorf("staged input %s is not root-owned", item.path)
			}
		}
		digest, err := sha256File(item.path, maximumBinaryBytes)
		if err != nil || !strings.EqualFold(digest, item.digest) {
			return fmt.Errorf("staged input %s SHA-256 mismatch", item.path)
		}
	}
	metadataBytes, err := readBoundedRegular(filepath.Join(directory, "stage-manifest.json"), maximumManifestBytes)
	if err != nil {
		return err
	}
	metadataInfo, err := os.Lstat(filepath.Join(directory, "stage-manifest.json"))
	if err != nil || !metadataInfo.Mode().IsRegular() || metadataInfo.Mode()&os.ModeSymlink != 0 || metadataInfo.Mode().Perm() != 0o644 {
		return errors.New("runtime input stage manifest is not a regular 0644 file")
	}
	if os.Geteuid() == 0 {
		owner, ownerErr := fileUID(metadataInfo)
		if ownerErr != nil || owner != 0 {
			return errors.New("runtime input stage manifest is not root-owned")
		}
	}
	var metadata stageManifest
	decoder := json.NewDecoder(strings.NewReader(string(metadataBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("runtime input stage manifest has trailing JSON values")
	}
	if metadata.Format != stageFormat || metadata.StageID != validated.Controller.SHA256+"-"+validated.VirtualBoard.SHA256 ||
		metadata.SourceRepository != repository || !strings.EqualFold(metadata.ControllerSHA256, validated.Controller.SHA256) ||
		!strings.EqualFold(metadata.VirtualBoardSHA256, validated.VirtualBoard.SHA256) {
		return errors.New("runtime input stage manifest does not match pinned source identities")
	}
	return nil
}
