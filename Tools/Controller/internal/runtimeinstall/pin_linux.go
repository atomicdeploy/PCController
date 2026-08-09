//go:build linux

package runtimeinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

var runtimeEffectiveUID = os.Geteuid

// ValidatePackage pins every privileged input with O_NOFOLLOW and derives the
// manifest bytes, identities, sizes, and hashes from those descriptors. It
// deliberately does not execute caller-selected artifacts. Apply copies the
// pinned bytes to a root-owned release stage and smokes that copy as the
// target account; privileged dry-runs remain non-executing.
func ValidatePackage(_ context.Context, packageDirectory, virtualBoard string) (ValidatedPackage, error) {
	var result ValidatedPackage
	packageDirectory, err := cleanAbsolutePath(packageDirectory, "host package directory")
	if err != nil {
		return result, err
	}
	virtualBoard, err = cleanAbsolutePath(virtualBoard, "VirtualBoard executable")
	if err != nil {
		return result, err
	}
	trusted := runtimeEffectiveUID() == 0
	packageFD, err := openPinnedDirectory(packageDirectory, trusted)
	if err != nil {
		return result, fmt.Errorf("pin host package directory: %w", err)
	}
	defer unix.Close(packageFD)
	manifestFile, err := openPinnedAt(packageFD, "host-manifest.json")
	if err != nil {
		return result, fmt.Errorf("pin host package manifest: %w", err)
	}
	manifestBytes, err := readPinnedRegular(manifestFile, maximumManifestBytes, trusted, false)
	_ = manifestFile.Close()
	if err != nil {
		return result, fmt.Errorf("read pinned host package manifest: %w", err)
	}
	manifest, artifact, err := decodeHostManifest(manifestBytes, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return result, err
	}
	artifactName := filepath.Clean(filepath.FromSlash(strings.TrimSpace(artifact.Path)))
	if filepath.Base(artifactName) != artifactName || artifactName != "controller" {
		return result, errors.New("Linux host manifest Controller artifact must be the direct package child named controller")
	}
	controllerFile, err := openPinnedAt(packageFD, artifactName)
	if err != nil {
		return result, fmt.Errorf("pin Controller artifact: %w", err)
	}
	controllerIdentity, err := identifyPinnedExecutable(
		controllerFile, "controller", filepath.Join(packageDirectory, artifactName),
		artifact.Bytes, artifact.SHA256, trusted,
	)
	if err != nil {
		_ = controllerFile.Close()
		return result, fmt.Errorf("validate pinned Controller artifact: %w", err)
	}
	boardFile, err := openPinnedAbsolute(virtualBoard, trusted)
	if err != nil {
		_ = controllerFile.Close()
		return result, fmt.Errorf("pin VirtualBoard artifact: %w", err)
	}
	boardIdentity, err := identifyPinnedExecutable(boardFile, "virtual-board", virtualBoard, 0, "", trusted)
	if err != nil {
		_ = controllerFile.Close()
		_ = boardFile.Close()
		return result, fmt.Errorf("validate pinned VirtualBoard artifact: %w", err)
	}
	result = ValidatedPackage{
		PackageDirectory: packageDirectory,
		ManifestPath:     filepath.Join(packageDirectory, "host-manifest.json"),
		ManifestBytes:    append([]byte(nil), manifestBytes...),
		Host:             manifest,
		Controller:       controllerIdentity,
		VirtualBoard:     boardIdentity,
		controllerFile:   controllerFile,
		virtualBoardFile: boardFile,
	}
	return result, nil
}

func openPinnedDirectory(path string, trusted bool) (int, error) {
	if trusted {
		return openTrustedDirectory(path)
	}
	path, err := cleanAbsolutePath(path, "pinned directory")
	if err != nil {
		return -1, err
	}
	current, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if component == "" {
			continue
		}
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(current)
		if openErr != nil {
			return -1, fmt.Errorf("open pinned ancestry component %s: %w", component, openErr)
		}
		current = next
	}
	return current, nil
}

func openPinnedRelativeDirectory(parent int, relative string) (int, error) {
	relative = filepath.Clean(filepath.FromSlash(strings.TrimSpace(relative)))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return -1, fmt.Errorf("unsafe pinned relative directory %q", relative)
	}
	current, err := unix.Dup(parent)
	if err != nil {
		return -1, err
	}
	unix.CloseOnExec(current)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			unix.Close(current)
			return -1, fmt.Errorf("unsafe pinned directory component %q", component)
		}
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(current)
		if openErr != nil {
			return -1, fmt.Errorf("open pinned relative component %s: %w", component, openErr)
		}
		current = next
	}
	return current, nil
}

func openTrustedDirectory(path string) (int, error) {
	path, err := cleanAbsolutePath(path, "trusted directory")
	if err != nil {
		return -1, err
	}
	current, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	if err := validateTrustedFD(current, true, false); err != nil {
		unix.Close(current)
		return -1, fmt.Errorf("trust filesystem root: %w", err)
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if component == "" {
			continue
		}
		next, openErr := unix.Openat(
			current, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		unix.Close(current)
		if openErr != nil {
			return -1, fmt.Errorf("open trusted ancestry component %s: %w", component, openErr)
		}
		current = next
		if err := validateTrustedFD(current, true, false); err != nil {
			unix.Close(current)
			return -1, fmt.Errorf("trust ancestry component %s: %w", component, err)
		}
	}
	return current, nil
}

func openPinnedAt(directory int, name string) (*os.File, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\\x00`) {
		return nil, fmt.Errorf("unsafe pinned child name %q", name)
	}
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func openPinnedAbsolute(path string, trusted bool) (*os.File, error) {
	parent := filepath.Dir(path)
	directory, err := openPinnedDirectory(parent, trusted)
	if err != nil {
		return nil, err
	}
	defer unix.Close(directory)
	return openPinnedAt(directory, filepath.Base(path))
}

func readPinnedRegular(file *os.File, maximum int64, trusted, executable bool) ([]byte, error) {
	if file == nil {
		return nil, errors.New("pinned file is unavailable")
	}
	if err := validatePinnedFile(file, maximum, trusted, executable); err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, errors.New("pinned file grew beyond its safety limit")
	}
	if err := validatePinnedFile(file, maximum, trusted, executable); err != nil {
		return nil, fmt.Errorf("pinned file changed during read: %w", err)
	}
	return content, nil
}

func identifyPinnedExecutable(
	file *os.File,
	role, displayPath string,
	expectedBytes int64,
	expectedSHA string,
	trusted bool,
) (FileIdentity, error) {
	if err := validatePinnedFile(file, maximumBinaryBytes, trusted, true); err != nil {
		return FileIdentity{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return FileIdentity{}, err
	}
	if expectedBytes > 0 && info.Size() != expectedBytes {
		return FileIdentity{}, fmt.Errorf("artifact size is %d; manifest requires %d", info.Size(), expectedBytes)
	}
	digest, err := sha256PinnedFile(file, maximumBinaryBytes)
	if err != nil {
		return FileIdentity{}, err
	}
	if expectedSHA != "" && !strings.EqualFold(digest, expectedSHA) {
		return FileIdentity{}, fmt.Errorf("artifact SHA-256 is %s; manifest requires %s", digest, expectedSHA)
	}
	if err := validatePinnedFile(file, maximumBinaryBytes, trusted, true); err != nil {
		return FileIdentity{}, fmt.Errorf("artifact changed during hashing: %w", err)
	}
	return FileIdentity{Role: role, Path: displayPath, Bytes: info.Size(), SHA256: digest}, nil
}

func validatePinnedFile(file *os.File, maximum int64, trusted, executable bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("pinned input is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > maximum {
		return fmt.Errorf("pinned input size %d is outside 1..%d bytes", info.Size(), maximum)
	}
	if executable && info.Mode().Perm()&0o111 == 0 {
		return errors.New("pinned input is not executable")
	}
	if trusted {
		return validateTrustedInfo(info, false, executable)
	}
	return nil
}

func validateTrustedFD(fd int, directory, executable bool) error {
	var status unix.Stat_t
	if err := unix.Fstat(fd, &status); err != nil {
		return err
	}
	return validateTrustedStat(&status, directory, executable)
}

func validateTrustedInfo(info os.FileInfo, directory, executable bool) error {
	owner, err := fileUID(info)
	if err != nil {
		return err
	}
	permissions := info.Mode().Perm()
	if owner != 0 {
		return fmt.Errorf("trusted input is owned by uid %d, not root", owner)
	}
	if permissions&0o022 != 0 {
		return fmt.Errorf("trusted input mode %04o is group/world writable", permissions)
	}
	if directory && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("trusted ancestry component is not a directory")
	}
	if !directory && !info.Mode().IsRegular() {
		return errors.New("trusted input is not a regular file")
	}
	if executable && permissions&0o111 == 0 {
		return errors.New("trusted input is not executable")
	}
	return nil
}

func validateTrustedStat(status *unix.Stat_t, directory, executable bool) error {
	permissions := os.FileMode(status.Mode).Perm()
	if status.Uid != 0 {
		return fmt.Errorf("trusted input is owned by uid %d, not root", status.Uid)
	}
	if permissions&0o022 != 0 {
		return fmt.Errorf("trusted input mode %04o is group/world writable", permissions)
	}
	fileType := status.Mode & unix.S_IFMT
	if directory && fileType != unix.S_IFDIR {
		return errors.New("trusted ancestry component is not a directory")
	}
	if !directory && fileType != unix.S_IFREG {
		return errors.New("trusted input is not a regular file")
	}
	if executable && permissions&0o111 == 0 {
		return errors.New("trusted input is not executable")
	}
	return nil
}

func sha256PinnedFile(file *os.File, maximum int64) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, maximum+1))
	if err != nil {
		return "", err
	}
	if written > maximum {
		return "", errors.New("pinned input grew beyond its hash safety limit")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func trustedRegularExecutable(path, label string) (string, error) {
	path, err := cleanAbsolutePath(path, label)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	file, err := openPinnedAbsolute(resolved, true)
	if err != nil {
		return "", fmt.Errorf("pin %s: %w", label, err)
	}
	defer file.Close()
	if err := validatePinnedFile(file, maximumBinaryBytes, true, true); err != nil {
		return "", fmt.Errorf("trust %s: %w", label, err)
	}
	return filepath.Clean(resolved), nil
}

func managedRuntimeExecutable(path, label string) (string, error) {
	path, err := cleanAbsolutePath(path, label)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	trusted := os.Geteuid() == 0
	file, err := openPinnedAbsolute(resolved, trusted)
	if err != nil {
		return "", fmt.Errorf("pin %s: %w", label, err)
	}
	defer file.Close()
	if err := validatePinnedFile(file, maximumBinaryBytes, trusted, true); err != nil {
		return "", fmt.Errorf("trust %s: %w", label, err)
	}
	return filepath.Clean(resolved), nil
}
