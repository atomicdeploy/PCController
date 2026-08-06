package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pccontroller.local/controller/internal/programmer"
)

const (
	zadigLatestReleaseURL = "https://api.github.com/repos/pbatard/libwdi/releases/latest"
	zadigMaximumBytes     = 16 << 20
)

var zadigAssetName = regexp.MustCompile(`(?i)^zadig-[0-9][0-9a-z._-]*\.exe$`)

type zadigRelease struct {
	TagName    string       `json:"tag_name"`
	Draft      bool         `json:"draft"`
	Prerelease bool         `json:"prerelease"`
	Assets     []zadigAsset `json:"assets"`
}

type zadigAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type zadigDownloadOptions struct {
	ReleaseURL     string
	DownloadPrefix string
	DataDir        string
	Client         *http.Client
	Verify         func(string) error
}

func downloadAndLaunchLatestZadig(output io.Writer, downloadOnly bool) error {
	paths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	path, release, err := downloadLatestZadig(ctx, zadigDownloadOptions{
		ReleaseURL: zadigLatestReleaseURL,
		DataDir:    paths.DataDir,
		Client:     http.DefaultClient,
		Verify:     verifyZadigExecutable,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "Verified official Zadig %s: %s\n", release, path)
	if downloadOnly {
		return nil
	}
	if err := launchZadig(path); err != nil {
		return err
	}
	fmt.Fprintln(output, "Zadig opened; select USBasp and WinUSB/libusbK. The Go status/ensure commands will verify later boards without reopening it when the driver is healthy.")
	return nil
}

func launchZadig(path string) error {
	command := newDetachedCommand(path)
	if err := command.Start(); err != nil {
		return fmt.Errorf("launch Zadig: %w", err)
	}
	return nil
}

func downloadLatestZadig(ctx context.Context, options zadigDownloadOptions) (string, string, error) {
	if ctx == nil {
		return "", "", errors.New("download latest Zadig: nil context")
	}
	releaseURL := strings.TrimSpace(options.ReleaseURL)
	if releaseURL == "" {
		releaseURL = zadigLatestReleaseURL
	}
	if strings.TrimSpace(options.DataDir) == "" {
		return "", "", errors.New("download latest Zadig: host data directory is required")
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	verify := options.Verify
	if verify == nil {
		verify = verifyZadigExecutable
	}

	releaseRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return "", "", err
	}
	releaseRequest.Header.Set("Accept", "application/vnd.github+json")
	releaseRequest.Header.Set("User-Agent", "PCController-Zadig-Bootstrap")
	releaseResponse, err := client.Do(releaseRequest)
	if err != nil {
		return "", "", fmt.Errorf("resolve latest official Zadig release: %w", err)
	}
	defer releaseResponse.Body.Close()
	if releaseResponse.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("resolve latest official Zadig release: HTTP %s", releaseResponse.Status)
	}
	var release zadigRelease
	decoder := json.NewDecoder(io.LimitReader(releaseResponse.Body, 2<<20))
	if err := decoder.Decode(&release); err != nil {
		return "", "", fmt.Errorf("decode latest official Zadig release: %w", err)
	}
	downloadPrefix := strings.TrimSpace(options.DownloadPrefix)
	if downloadPrefix == "" {
		downloadPrefix = "https://github.com/pbatard/libwdi/releases/download/"
	}
	asset, err := selectZadigAsset(release, downloadPrefix)
	if err != nil {
		return "", "", err
	}

	directory := filepath.Join(
		options.DataDir,
		"tools",
		"zadig",
		safeZadigPathComponent(release.TagName),
	)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", "", fmt.Errorf("create Zadig tool directory: %w", err)
	}
	destination := filepath.Join(directory, asset.Name)
	if info, statErr := os.Stat(destination); statErr == nil && !info.IsDir() {
		if asset.Size <= 0 || info.Size() == asset.Size {
			if err := validateDownloadedZadig(destination, verify); err == nil {
				return destination, release.TagName, nil
			}
		}
	}

	downloadRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", "", err
	}
	downloadRequest.Header.Set("Accept", "application/octet-stream")
	downloadRequest.Header.Set("User-Agent", "PCController-Zadig-Bootstrap")
	downloadResponse, err := client.Do(downloadRequest)
	if err != nil {
		return "", "", fmt.Errorf("download %s: %w", asset.Name, err)
	}
	defer downloadResponse.Body.Close()
	if downloadResponse.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download %s: HTTP %s", asset.Name, downloadResponse.Status)
	}
	if downloadResponse.ContentLength > zadigMaximumBytes {
		return "", "", fmt.Errorf("download %s: response exceeds %d bytes", asset.Name, zadigMaximumBytes)
	}
	temporary, err := os.CreateTemp(directory, ".zadig-*.download")
	if err != nil {
		return "", "", fmt.Errorf("create temporary Zadig download: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	written, copyErr := io.Copy(temporary, io.LimitReader(downloadResponse.Body, zadigMaximumBytes+1))
	if copyErr != nil {
		return "", "", fmt.Errorf("write Zadig download: %w", copyErr)
	}
	if written > zadigMaximumBytes {
		return "", "", fmt.Errorf("download %s exceeds %d bytes", asset.Name, zadigMaximumBytes)
	}
	if asset.Size > 0 && written != asset.Size {
		return "", "", fmt.Errorf("download %s size %d does not match release metadata %d", asset.Name, written, asset.Size)
	}
	if err := temporary.Sync(); err != nil {
		return "", "", fmt.Errorf("flush Zadig download: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", "", fmt.Errorf("close Zadig download: %w", err)
	}
	if err := validateDownloadedZadig(temporaryPath, verify); err != nil {
		return "", "", err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		if removeErr := os.Remove(destination); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return "", "", fmt.Errorf("replace cached Zadig executable: %w", removeErr)
		}
		if renameErr := os.Rename(temporaryPath, destination); renameErr != nil {
			return "", "", fmt.Errorf("publish Zadig executable: %w", renameErr)
		}
	}
	keep = true
	return destination, release.TagName, nil
}

func selectZadigAsset(release zadigRelease, downloadPrefix string) (zadigAsset, error) {
	if release.Draft || release.Prerelease || strings.TrimSpace(release.TagName) == "" {
		return zadigAsset{}, errors.New("latest libwdi release is not a stable tagged release")
	}
	var selected []zadigAsset
	for _, asset := range release.Assets {
		if zadigAssetName.MatchString(asset.Name) &&
			strings.HasPrefix(strings.ToLower(asset.BrowserDownloadURL), strings.ToLower(downloadPrefix)) &&
			asset.Size > 0 && asset.Size <= zadigMaximumBytes {
			selected = append(selected, asset)
		}
	}
	if len(selected) != 1 {
		return zadigAsset{}, fmt.Errorf("latest official libwdi release contains %d eligible Zadig executables; expected exactly one", len(selected))
	}
	return selected[0], nil
}

func validateDownloadedZadig(path string, verify func(string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open downloaded Zadig executable: %w", err)
	}
	header := make([]byte, 2)
	_, readErr := io.ReadFull(file, header)
	closeErr := file.Close()
	if readErr != nil {
		return fmt.Errorf("read downloaded Zadig executable: %w", readErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if string(header) != "MZ" {
		return errors.New("downloaded Zadig artifact is not a Windows PE executable")
	}
	if err := verify(path); err != nil {
		return fmt.Errorf("downloaded Zadig Authenticode signature is not trusted: %w", err)
	}
	return nil
}

func safeZadigPathComponent(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(character rune) rune {
		switch {
		case character >= 'a' && character <= 'z':
			return character
		case character >= 'A' && character <= 'Z':
			return character
		case character >= '0' && character <= '9':
			return character
		case character == '.', character == '-', character == '_':
			return character
		default:
			return '_'
		}
	}, value)
	if value == "" || value == "." || value == ".." {
		return "latest"
	}
	return value
}
