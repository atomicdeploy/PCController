//go:build linux

package programmer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"pccontroller.local/controller/internal/ownedstorage"
)

const (
	existingHostDataMaxEntries = 20000
	existingHostDataMaxBytes   = int64(1 << 30)
	existingEEPROMBytes        = int64(1024)
)

// AdoptKnownHostDataPaths safely adopts only the small historical Linux
// layout created by older Controller provisioners before ownership markers
// existed. It runs as the target user, never as root.
func AdoptKnownHostDataPaths(paths HostDataPaths) error {
	if err := ownedstorage.Verify(paths.DataDir); err == nil {
		return EnsureHostDataPaths(paths)
	} else if !errors.Is(err, ownedstorage.ErrNotOwned) {
		return err
	}
	if _, err := os.Lstat(paths.DataDir); errors.Is(err, os.ErrNotExist) {
		return EnsureHostDataPaths(paths)
	} else if err != nil {
		return err
	}
	if err := ownedstorage.AdoptKnown(paths.DataDir, func(root string) error {
		return validateKnownLinuxHostData(root, paths)
	}); err != nil {
		return fmt.Errorf("adopt known existing Linux host data: %w", err)
	}
	return EnsureHostDataPaths(paths)
}

func validateKnownLinuxHostData(root string, paths HostDataPaths) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	rootStat, err := linuxDataStat(rootInfo)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || rootInfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("existing host data root is not a private real directory")
	}
	if uint32(os.Geteuid()) != rootStat.Uid {
		return errors.New("existing host data root is not owned by the current user")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Name()] = true
		if entry.Name() != "tools" && entry.Name() != "virtual-board" {
			return fmt.Errorf("existing host data has unsupported top-level entry %q", entry.Name())
		}
	}
	if !seen["tools"] {
		return errors.New("existing host data does not contain tools/toolchain")
	}
	toolchain := filepath.Join(root, "tools", "toolchain")
	if err := validateExistingDataDirectory(filepath.Join(root, "tools"), rootStat); err != nil {
		return err
	}
	toolEntries, err := os.ReadDir(filepath.Join(root, "tools"))
	if err != nil {
		return err
	}
	if len(toolEntries) != 1 || toolEntries[0].Name() != "toolchain" {
		return errors.New("existing tools directory must contain only toolchain")
	}
	if err := validateExistingToolchain(toolchain, rootStat); err != nil {
		return err
	}
	if !seen["virtual-board"] {
		return nil
	}
	boardDir := filepath.Join(root, "virtual-board")
	if err := validateExistingDataDirectory(boardDir, rootStat); err != nil {
		return err
	}
	boardEntries, err := os.ReadDir(boardDir)
	if err != nil {
		return err
	}
	if len(boardEntries) != 1 || boardEntries[0].Name() != "eeprom.bin" {
		return errors.New("existing virtual-board directory must contain only eeprom.bin")
	}
	eeprom := filepath.Join(boardDir, "eeprom.bin")
	info, err := os.Lstat(eeprom)
	if err != nil {
		return err
	}
	stat, err := linuxDataStat(info)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != existingEEPROMBytes || stat.Uid != rootStat.Uid || stat.Dev != rootStat.Dev {
		return errors.New("existing VirtualBoard EEPROM is not the expected target-owned regular 1024-byte file")
	}
	if err := os.Chmod(eeprom, 0o600); err != nil {
		return fmt.Errorf("normalize existing VirtualBoard EEPROM permissions: %w", err)
	}
	return nil
}

func validateExistingToolchain(root string, expected *syscall.Stat_t) error {
	if err := validateExistingDataDirectory(root, expected); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	allowed := map[string]bool{"firmware-cli.yaml": true, "arduino-cli": true, "data": true, "downloads": true, "user": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return fmt.Errorf("existing toolchain has unsupported top-level entry %q", entry.Name())
		}
	}
	for _, name := range []string{"arduino-cli", "data", "downloads", "user"} {
		if err := validateExistingDataDirectory(filepath.Join(root, name), expected); err != nil {
			return err
		}
	}
	config := filepath.Join(root, "firmware-cli.yaml")
	configInfo, err := os.Lstat(config)
	if err != nil {
		return err
	}
	configStat, err := linuxDataStat(configInfo)
	if err != nil || !configInfo.Mode().IsRegular() || configInfo.Mode().Perm()&0o022 != 0 || configStat.Uid != expected.Uid || configStat.Dev != expected.Dev {
		return errors.New("existing toolchain configuration is not a private target-owned regular file")
	}
	var count int
	var bytes int64
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		count++
		if count > existingHostDataMaxEntries {
			return errors.New("existing toolchain exceeds the bounded entry limit")
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat, err := linuxDataStat(info)
		if err != nil || stat.Uid != expected.Uid || stat.Dev != expected.Dev {
			return fmt.Errorf("existing toolchain entry %s has unsafe owner or filesystem", path)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil || filepath.IsAbs(target) {
				return fmt.Errorf("existing toolchain link %s is not relative", path)
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil || !dataPathWithin(root, resolved) {
				return fmt.Errorf("existing toolchain link %s escapes toolchain", path)
			}
			targetInfo, err := os.Stat(resolved)
			if err != nil || !targetInfo.Mode().IsRegular() {
				return fmt.Errorf("existing toolchain link %s does not resolve to a regular file", path)
			}
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("existing toolchain entry %s is not a regular file or directory", path)
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("existing toolchain entry %s is group or world writable", path)
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
			if bytes > existingHostDataMaxBytes {
				return errors.New("existing toolchain exceeds the bounded byte limit")
			}
		}
		return nil
	})
}

func validateExistingDataDirectory(path string, expected *syscall.Stat_t) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, err := linuxDataStat(info)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || stat.Uid != expected.Uid || stat.Dev != expected.Dev {
		return fmt.Errorf("existing data directory %s is not private, target-owned, and on the expected filesystem", path)
	}
	return nil
}

func linuxDataStat(info os.FileInfo) (*syscall.Stat_t, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errors.New("unsupported Linux file metadata")
	}
	return stat, nil
}

func dataPathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
