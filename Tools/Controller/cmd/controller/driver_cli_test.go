package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateUSBaspDriverPackageRequiresExactTargetAndPayload(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"USBasp.cat", "amd64", "x86"} {
		path := filepath.Join(directory, name)
		if name == "amd64" || name == "x86" {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, []byte("catalog"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inf := filepath.Join(directory, "USBasp.inf")
	if err := os.WriteFile(inf, []byte("DeviceID=VID_16C0&PID_05DC\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := validateUSBaspDriverPackage(directory); err != nil || got != inf {
		t.Fatalf("validated path=%q err=%v", got, err)
	}
	if err := os.WriteFile(inf, []byte("DeviceID=VID_DEAD&PID_BEEF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateUSBaspDriverPackage(directory); err == nil {
		t.Fatal("unrelated USB driver package was accepted")
	}
}

func TestUSBaspDeviceBlocksFiltersPnPUtilListing(t *testing.T) {
	listing := "Instance ID: USB\\VID_DEAD&PID_BEEF\\1\r\nHardware IDs:\r\n USB\\VID_DEAD&PID_BEEF\r\n\r\n" +
		"Instance ID: USB\\VID_16C0&PID_05DC\\2\r\nHardware IDs:\r\n USB\\VID_16C0&PID_05DC\r\n"
	matches := usbaspDeviceBlocks(listing)
	if len(matches) != 1 || matches[0] == "" {
		t.Fatalf("USBasp matches=%q want one", matches)
	}
}

func TestUSBaspDriverReadyRequiresStartedDriver(t *testing.T) {
	if !usbaspDriverReady([]string{"Status: Started\nDriver Name: oem25.inf"}) {
		t.Fatal("started USBasp driver was not accepted")
	}
	if usbaspDriverReady([]string{"Status: Problem\nDriver Name:"}) {
		t.Fatal("problem USBasp without a driver was accepted")
	}
}

func TestDownloadLatestZadigSelectsCachesAndVerifiesOfficialAsset(t *testing.T) {
	payload := append([]byte("MZ"), []byte("signed-test-payload")...)
	downloads := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/latest":
			_ = json.NewEncoder(output).Encode(zadigRelease{
				TagName: "v9.8.7",
				Assets: []zadigAsset{{
					Name: "zadig-9.8.exe", BrowserDownloadURL: server.URL + "/download/zadig-9.8.exe",
					Size: int64(len(payload)),
				}},
			})
		case "/download/zadig-9.8.exe":
			downloads++
			_, _ = output.Write(payload)
		default:
			http.NotFound(output, request)
		}
	}))
	defer server.Close()
	verified := 0
	options := zadigDownloadOptions{
		ReleaseURL: server.URL + "/latest", DownloadPrefix: server.URL + "/download/",
		DataDir: t.TempDir(), Client: server.Client(),
		Verify: func(path string) error {
			verified++
			if !strings.HasSuffix(path, ".exe") && !strings.Contains(filepath.Base(path), ".zadig-") {
				t.Fatalf("unexpected verification path %q", path)
			}
			return nil
		},
	}
	path, version, err := downloadLatestZadig(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v9.8.7" || filepath.Base(path) != "zadig-9.8.exe" {
		t.Fatalf("download path=%q version=%q", path, version)
	}
	pathAgain, _, err := downloadLatestZadig(context.Background(), options)
	if err != nil || pathAgain != path {
		t.Fatalf("cached download path=%q err=%v", pathAgain, err)
	}
	if downloads != 1 || verified != 2 {
		t.Fatalf("downloads=%d verifications=%d", downloads, verified)
	}
}

func TestDownloadLatestZadigRejectsUntrustedSignatureAndAssetOrigin(t *testing.T) {
	release := zadigRelease{TagName: "v1", Assets: []zadigAsset{{
		Name: "zadig-1.0.exe", BrowserDownloadURL: "https://untrusted.example/zadig-1.0.exe", Size: 10,
	}}}
	if _, err := selectZadigAsset(release, "https://github.com/pbatard/libwdi/releases/download/"); err == nil {
		t.Fatal("untrusted Zadig download origin was accepted")
	}
	path := filepath.Join(t.TempDir(), "zadig-1.0.exe")
	if err := os.WriteFile(path, []byte("MZpayload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateDownloadedZadig(path, func(string) error { return errors.New("untrusted") }); err == nil {
		t.Fatal("untrusted Authenticode result was accepted")
	}
}

func TestValidateUSBaspDriverPackageAcceptsUTF16INF(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"amd64", "x86"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "USBasp.cat"), []byte("catalog"), 0o600); err != nil {
		t.Fatal(err)
	}
	text := []byte{0xff, 0xfe}
	for _, value := range []byte(`DeviceID="VID_16C0&PID_05DC"`) {
		text = append(text, value, 0)
	}
	if err := os.WriteFile(filepath.Join(directory, "USBasp.inf"), text, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateUSBaspDriverPackage(directory); err != nil {
		t.Fatal(err)
	}
}
