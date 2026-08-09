package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pccontroller.local/controller/internal/programmer"
)

const usbaspHardwareID = `USB\VID_16C0&PID_05DC`

const usbaspDriverUsage = "usage: controller driver usbasp status | ensure | install [--package DIR] | zadig [--latest] [--download-only] [--exe FILE]"

func runDriver(args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 || !strings.EqualFold(args[0], "usbasp") {
		return errors.New(usbaspDriverUsage)
	}
	if runtime.GOOS != "windows" {
		return errors.New("USBasp driver management is available only on Windows; use the platform's libusb package manager")
	}
	switch strings.ToLower(args[1]) {
	case "status", "check":
		if len(args) != 2 {
			return errors.New("usage: controller driver usbasp status")
		}
		matches, err := connectedUSBaspBlocks(stderr)
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			fmt.Fprintln(stdout, "USBasp is not currently connected.")
			return nil
		}
		fmt.Fprintln(stdout, "Connected USBasp device:")
		fmt.Fprintln(stdout, strings.Join(matches, "\n\n"))
		if usbaspDriverReady(matches) {
			fmt.Fprintln(stdout, "USBasp driver status: ready")
		} else {
			fmt.Fprintln(stdout, "USBasp driver status: missing or not started; run 'controller driver usbasp ensure'.")
		}
		return nil
	case "ensure":
		if len(args) != 2 {
			return errors.New("usage: controller driver usbasp ensure")
		}
		matches, err := connectedUSBaspBlocks(stderr)
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			fmt.Fprintln(stdout, "USBasp is not connected; no driver action is required.")
			return nil
		}
		if usbaspDriverReady(matches) {
			fmt.Fprintln(stdout, "USBasp is connected and its driver is started; no driver action is required.")
			return nil
		}
		fmt.Fprintln(stdout, "USBasp is connected but its driver is missing or not started.")
		return downloadAndLaunchLatestZadig(stdout, false)
	case "install":
		flags := flag.NewFlagSet("driver usbasp install", flag.ContinueOnError)
		flags.SetOutput(stderr)
		packageDirectory := flags.String("package", "", "Zadig/libwdi-generated USBasp driver package directory")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("usage: controller driver usbasp install [--package DIR]")
		}
		if *packageDirectory == "" {
			paths, err := programmer.DefaultHostDataPaths()
			if err != nil {
				return err
			}
			*packageDirectory = filepath.Join(paths.DataDir, "drivers", "usbasp")
		}
		inf, err := validateUSBaspDriverPackage(*packageDirectory)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Installing verified-target USBasp driver package with Windows PnPUtil:", inf)
		fmt.Fprintln(stdout, "This command is non-interactive but must run from an elevated process.")
		command := exec.Command("pnputil.exe", "/add-driver", inf, "/install")
		command.Stdout, command.Stderr = stdout, stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("install USBasp driver package (if Windows reports an untrusted generated certificate, use 'driver usbasp zadig' once on this machine): %w", err)
		}
		return nil
	case "zadig", "open-zadig":
		flags := flag.NewFlagSet("driver usbasp zadig", flag.ContinueOnError)
		flags.SetOutput(stderr)
		executable := flags.String("exe", "", "signed Zadig executable")
		latest := flags.Bool("latest", false, "resolve and download the latest signed official Zadig release")
		downloadOnly := flags.Bool("download-only", false, "download and verify Zadig without launching its GUI")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("usage: controller driver usbasp zadig [--latest] [--download-only] [--exe FILE]")
		}
		if *executable != "" && *latest {
			return errors.New("--exe and --latest are mutually exclusive")
		}
		if *executable == "" {
			// With no explicit path, resolve the release dynamically. No Zadig
			// version or proxy is embedded in the binary: net/http honors the
			// operator's standard proxy environment and the process-wide local
			// no-proxy policy remains in force.
			return downloadAndLaunchLatestZadig(stdout, *downloadOnly)
		}
		absolute, err := filepath.Abs(*executable)
		if err != nil {
			return err
		}
		info, err := os.Stat(absolute)
		if err != nil || info.IsDir() {
			return fmt.Errorf("Zadig executable is unavailable at %s", absolute)
		}
		if err := verifyZadigExecutable(absolute); err != nil {
			return fmt.Errorf("verify Zadig Authenticode signature: %w", err)
		}
		if *downloadOnly {
			fmt.Fprintln(stdout, "Verified signed Zadig executable:", absolute)
			return nil
		}
		// Zadig intentionally owns interactive driver generation/signing. It
		// does not advertise a supported silent install switch, so the Go tool
		// uses PnPUtil for generated packages and launches Zadig only when that
		// one-time GUI path is required.
		if err := exec.Command(absolute).Start(); err != nil {
			return fmt.Errorf("launch Zadig: %w", err)
		}
		fmt.Fprintln(stdout, "Zadig opened; select USBasp and WinUSB/libusbK, then use the Go install/status command for later boards.")
		return nil
	default:
		return errors.New(usbaspDriverUsage)
	}
}

func connectedUSBaspBlocks(stderr io.Writer) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "pnputil.exe", "/enum-devices", "/connected", "/ids")
	var listing bytes.Buffer
	command.Stdout, command.Stderr = &listing, stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("query USBasp device status: %w", err)
	}
	return usbaspDeviceBlocks(listing.String()), nil
}

func usbaspDriverReady(blocks []string) bool {
	for _, block := range blocks {
		normalized := strings.ToLower(block)
		if strings.Contains(normalized, "status:") &&
			strings.Contains(normalized, "started") &&
			strings.Contains(normalized, "driver name:") {
			return true
		}
	}
	return false
}

func usbaspDeviceBlocks(listing string) []string {
	listing = strings.ReplaceAll(listing, "\r\n", "\n")
	blocks := strings.Split(listing, "\n\n")
	matches := make([]string, 0, 1)
	wanted := strings.ToUpper(usbaspHardwareID)
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if strings.Contains(strings.ToUpper(block), wanted) {
			matches = append(matches, block)
		}
	}
	return matches
}

func validateUSBaspDriverPackage(directory string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(directory))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(directory) == "" {
		return "", errors.New("USBasp driver package directory is required")
	}
	inf := filepath.Join(absolute, "USBasp.inf")
	content, err := os.ReadFile(inf)
	if err != nil {
		return "", fmt.Errorf("read USBasp driver INF: %w", err)
	}
	if len(content) > 1024*1024 {
		return "", errors.New("USBasp driver INF exceeds 1 MiB")
	}
	// Windows driver generators commonly emit UTF-16 INF files. The hardware
	// identifier is ASCII in either encoding, so removing NUL code-unit bytes
	// before normalization keeps the exact VID/PID check encoding-independent.
	normalized := bytes.ReplaceAll(content, []byte{0}, nil)
	normalized = bytes.ToUpper(bytes.ReplaceAll(normalized, []byte("_"), nil))
	if !bytes.Contains(normalized, []byte("VID16C0&PID05DC")) {
		return "", errors.New("driver INF does not target USBasp VID 16C0 PID 05DC")
	}
	for _, required := range []string{"USBasp.cat", "amd64", "x86"} {
		if _, err := os.Stat(filepath.Join(absolute, required)); err != nil {
			return "", fmt.Errorf("USBasp driver package is incomplete (%s): %w", required, err)
		}
	}
	return inf, nil
}
