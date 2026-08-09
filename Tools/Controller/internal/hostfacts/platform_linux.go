//go:build linux

package hostfacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type nativeBackend struct{}

func platformHostFactsSource() string { return "linux-native" }

func platformHostFactsClass(profile, windowsClass string) string {
	classes := map[string]string{
		"computer": "LinuxDMI",
		"firmware": "LinuxFirmware",
		"serial":   "LinuxSerialDevice",
		"storage":  "LinuxMount",
		"system":   "LinuxOperatingSystem",
	}
	if value := classes[profile]; value != "" {
		return value
	}
	return windowsClass
}

func (nativeBackend) query(
	ctx context.Context,
	spec querySpec,
) ([]map[string]any, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	switch spec.Profile {
	case "computer":
		row, err := linuxComputerFacts()
		return oneLinuxFactRow(row, err)
	case "firmware":
		row, err := linuxFirmwareFacts()
		return oneLinuxFactRow(row, err)
	case "serial":
		rows, err := linuxSerialFacts(ctx)
		return rows, false, err
	case "storage":
		rows, err := linuxStorageFacts(ctx)
		return rows, false, err
	case "system":
		row, err := linuxSystemFacts()
		return oneLinuxFactRow(row, err)
	default:
		return nil, false, fmt.Errorf("unsupported Linux host-facts profile %q", spec.Profile)
	}
}

func oneLinuxFactRow(row map[string]any, err error) ([]map[string]any, bool, error) {
	if err != nil {
		return nil, false, err
	}
	return []map[string]any{row}, false, nil
}

func linuxComputerFacts() (map[string]any, error) {
	info, err := linuxSysinfo()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Manufacturer":              readLinuxFact("/sys/devices/virtual/dmi/id/sys_vendor"),
		"Model":                     readLinuxFact("/sys/devices/virtual/dmi/id/product_name"),
		"SystemType":                runtime.GOARCH,
		"TotalPhysicalMemory":       scaledLinuxMemory(info.Totalram, info.Unit),
		"NumberOfLogicalProcessors": runtime.NumCPU(),
	}, nil
}

func linuxFirmwareFacts() (map[string]any, error) {
	major, minor := linuxSMBIOSVersion("/sys/firmware/dmi/tables/smbios_entry_point")
	return map[string]any{
		"Manufacturer":       readLinuxFact("/sys/devices/virtual/dmi/id/bios_vendor"),
		"SMBIOSBIOSVersion":  readLinuxFact("/sys/devices/virtual/dmi/id/bios_version"),
		"ReleaseDate":        readLinuxFact("/sys/devices/virtual/dmi/id/bios_date"),
		"SMBIOSMajorVersion": major,
		"SMBIOSMinorVersion": minor,
	}, nil
}

func linuxSystemFacts() (map[string]any, error) {
	info, err := linuxSysinfo()
	if err != nil {
		return nil, err
	}
	values, _ := readLinuxOSRelease("/etc/os-release")
	caption := values["PRETTY_NAME"]
	if caption == "" {
		caption = values["NAME"]
	}
	var uname unix.Utsname
	if err := unix.Uname(&uname); err != nil {
		return nil, fmt.Errorf("read Linux kernel identity: %w", err)
	}
	booted := time.Now().Add(-time.Duration(info.Uptime) * time.Second).UTC()
	return map[string]any{
		"Caption":                caption,
		"Version":                values["VERSION_ID"],
		"BuildNumber":            linuxUnameString(uname.Release[:]),
		"OSArchitecture":         runtime.GOARCH,
		"LastBootUpTime":         booted.Format(time.RFC3339),
		"TotalVisibleMemorySize": scaledLinuxMemory(info.Totalram, info.Unit) / 1024,
		"FreePhysicalMemory":     scaledLinuxMemory(info.Freeram, info.Unit) / 1024,
	}, nil
}

func linuxSysinfo() (*unix.Sysinfo_t, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return nil, fmt.Errorf("read Linux system information: %w", err)
	}
	return &info, nil
}

func scaledLinuxMemory(value uint64, unit uint32) uint64 {
	if unit == 0 {
		unit = 1
	}
	return value * uint64(unit)
}

func readLinuxFact(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func readLinuxOSRelease(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			continue
		}
		if unquoted, quoteErr := strconv.Unquote(value); quoteErr == nil {
			value = unquoted
		}
		values[key] = value
	}
	return values, nil
}

func linuxSMBIOSVersion(path string) (uint8, uint8) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	switch {
	case len(content) >= 9 && string(content[:5]) == "_SM3_":
		return content[7], content[8]
	case len(content) >= 8 && string(content[:4]) == "_SM_":
		return content[6], content[7]
	default:
		return 0, 0
	}
}

func linuxUnameString(value []byte) string {
	if index := strings.IndexByte(string(value), 0); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(string(value))
}

func linuxSerialFacts(ctx context.Context) ([]map[string]any, error) {
	var devices []string
	for _, pattern := range []string{"/dev/ttyACM*", "/dev/ttyUSB*"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		devices = append(devices, matches...)
	}
	sort.Strings(devices)
	rows := make([]map[string]any, 0, len(devices))
	for _, device := range devices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := filepath.Base(device)
		rows = append(rows, map[string]any{
			"DeviceID":               device,
			"Name":                   name,
			"Description":            linuxTTYDescription(name),
			"Status":                 "OK",
			"ConfigManagerErrorCode": 0,
		})
	}
	return rows, nil
}

func linuxTTYDescription(name string) string {
	path, err := filepath.EvalSymlinks(filepath.Join("/sys/class/tty", name, "device"))
	if err != nil {
		return "USB serial device"
	}
	for depth := 0; depth < 8; depth++ {
		if product := readLinuxFact(filepath.Join(path, "product")); product != "" {
			return product
		}
		parent := filepath.Dir(path)
		if parent == path || !strings.HasPrefix(parent, "/sys/") {
			break
		}
		path = parent
	}
	return "USB serial device"
}

func linuxStorageFacts(ctx context.Context) ([]map[string]any, error) {
	content, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("read Linux mount table: %w", err)
	}
	rows := make([]map[string]any, 0, 8)
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(content), "\n") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fields := strings.Fields(line)
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if separator < 0 || len(fields) <= separator+2 || len(fields) <= 4 {
			continue
		}
		mountpoint := decodeLinuxMountPath(fields[4])
		filesystem, source := fields[separator+1], decodeLinuxMountPath(fields[separator+2])
		if mountpoint != "/" && !strings.HasPrefix(source, "/dev/") {
			continue
		}
		if seen[mountpoint] {
			continue
		}
		seen[mountpoint] = true
		var stats unix.Statfs_t
		if err := unix.Statfs(mountpoint, &stats); err != nil {
			continue
		}
		blockSize := uint64(stats.Bsize)
		rows = append(rows, map[string]any{
			"DeviceID":   mountpoint,
			"DriveType":  3,
			"FileSystem": filesystem,
			"Size":       stats.Blocks * blockSize,
			"FreeSpace":  stats.Bavail * blockSize,
		})
	}
	if len(rows) == 0 {
		return nil, errors.New("Linux host facts found no fixed mounted filesystem")
	}
	sort.Slice(rows, func(left, right int) bool {
		return rows[left]["DeviceID"].(string) < rows[right]["DeviceID"].(string)
	})
	return rows, nil
}

func decodeLinuxMountPath(value string) string {
	replacer := strings.NewReplacer(
		`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`,
	)
	return replacer.Replace(value)
}
