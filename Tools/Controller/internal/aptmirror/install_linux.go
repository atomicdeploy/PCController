//go:build linux

package aptmirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const maximumControllerExecutableBytes = 512 << 20

func readPinnedExecutable(path string, requireTrusted bool) ([]byte, error) {
	if requireTrusted && os.Geteuid() != 0 {
		return nil, errors.New("APT mirror profile --apply requires root")
	}
	return readPinnedExecutableWithPolicy(path, requireTrusted, 0, nil)
}

func readPinnedExecutableWithPolicy(path string, requireTrusted bool, expectedUID uint32, afterOpen func()) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open no-follow Controller executable: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open pinned Controller executable")
	}
	defer file.Close()
	if afterOpen != nil {
		afterOpen()
	}
	var status unix.Stat_t
	if err := unix.Fstat(fd, &status); err != nil {
		return nil, fmt.Errorf("inspect pinned Controller executable: %w", err)
	}
	if status.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.New("current Controller executable must be a regular file, not a symlink")
	}
	if requireTrusted && (status.Uid != expectedUID || status.Mode&0o022 != 0) {
		return nil, errors.New("current Controller executable must be root-owned and not group/world writable")
	}
	if status.Size < 0 || status.Size > maximumControllerExecutableBytes {
		return nil, fmt.Errorf("current Controller executable size %d exceeds the bounded installer limit", status.Size)
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumControllerExecutableBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read pinned Controller executable: %w", err)
	}
	if len(content) > maximumControllerExecutableBytes {
		return nil, errors.New("current Controller executable grew beyond the bounded installer limit")
	}
	return content, nil
}

func selfTestUnattendedUpgradeShim(ctx context.Context, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect installed unattended-upgrade shim: %w", err)
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return errors.New("installed unattended-upgrade shim must be a root-owned regular file and not group/world writable")
	}
	command := exec.CommandContext(ctx, path, "--pccontroller-self-test")
	command.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LC_ALL=C",
		"LANG=C",
	}
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if len(detail) > 4096 {
		detail = detail[:4096] + "..."
	}
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}

func validateMirrorApply(managedPaths []string, backupRoot string) error {
	if os.Geteuid() != 0 {
		return errors.New("APT mirror profile --apply requires root")
	}
	paths := append(append([]string(nil), managedPaths...), backupRoot)
	for _, path := range paths {
		if err := validateRootOwnedAncestors(filepath.Dir(path)); err != nil {
			return fmt.Errorf("validate root-owned managed path %s: %w", path, err)
		}
	}
	if err := validateRootOwnedAncestors(backupRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("validate root-owned backup root %s: %w", backupRoot, err)
	}
	return nil
}

func validateRootOwnedAncestors(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			parent := filepath.Dir(current)
			if parent == current {
				return err
			}
			current = parent
			continue
		}
		if err != nil {
			return err
		}
		status, ok := info.Sys().(*syscall.Stat_t)
		if !ok || status.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s is not a root-owned real directory", current)
		}
		if current == string(filepath.Separator) {
			return nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}
