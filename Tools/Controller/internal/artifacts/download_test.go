package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type artifactRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip artifactRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestDownloaderVerifiesRemoteSizeAndHash(t *testing.T) {
	digest := sha256.Sum256([]byte(validIntelHEX))
	hash := hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Checksum-SHA256", hash)
		writer.Header().Set("Content-Disposition", `attachment; filename="remote.hex"`)
		_, _ = writer.Write([]byte(validIntelHEX))
	}))
	defer server.Close()
	store := newTestStore(t)
	descriptor, err := NewTrustedDownloader(server.Client()).Fetch(context.Background(), store, FetchRequest{
		URL: server.URL, Kind: KindFirmware, SHA256: hash, Bytes: int64(len(validIntelHEX)),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.SHA256 != hash || descriptor.Name != "remote.hex" {
		t.Fatalf("descriptor=%#v", descriptor)
	}
	_, err = NewTrustedDownloader(server.Client()).Fetch(context.Background(), store, FetchRequest{
		URL: server.URL, Kind: KindFirmware, SHA256: strings.Repeat("0", 64),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum header") {
		t.Fatalf("mismatch err=%v", err)
	}
}

func TestDownloaderDoesNotForwardBearerAcrossAuthority(t *testing.T) {
	var received string
	destination := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received = request.Header.Get("Authorization")
		_, _ = writer.Write([]byte(validIntelHEX))
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusFound)
	}))
	defer origin.Close()
	client := origin.Client()
	_, err := NewTrustedDownloader(client).Fetch(context.Background(), newTestStore(t), FetchRequest{
		URL: origin.URL, Kind: KindFirmware, BearerToken: "secret",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if received != "" {
		t.Fatalf("cross-authority Authorization=%q", received)
	}
}

func TestDefaultDownloaderRejectsNonPublicInitialAndRedirectURLs(t *testing.T) {
	downloader := NewDownloader(nil)
	_, err := downloader.Fetch(context.Background(), newTestStore(t), FetchRequest{
		URL: "http://169.254.169.254/latest/meta-data/", Kind: KindFirmware,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "artifact URL") {
		t.Fatalf("metadata destination error=%v", err)
	}

	origin, _ := http.NewRequest(http.MethodGet, "https://updates.example.com/board.hex", nil)
	redirect, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/private.hex", nil)
	err = downloader.client.CheckRedirect(redirect, []*http.Request{origin})
	if err == nil || !strings.Contains(err.Error(), "public network destination") {
		t.Fatalf("private redirect error=%v", err)
	}
}

func TestDownloaderInjectionCannotRelaxPublicDestinationPolicy(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	_, err := NewDownloader(server.Client()).Fetch(context.Background(), newTestStore(t), FetchRequest{
		URL: server.URL, Kind: KindFirmware,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "public network destination") {
		t.Fatalf("injected client local-destination error=%v", err)
	}
	if requests != 0 {
		t.Fatalf("local server received %d public downloader requests", requests)
	}

	called := false
	custom := &http.Client{Transport: artifactRoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not run")
	})}
	_, err = NewDownloader(custom).Fetch(context.Background(), newTestStore(t), FetchRequest{
		URL: "https://updates.example.com/board.hex", Kind: KindFirmware,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "explicit trusted client") {
		t.Fatalf("custom transport error=%v", err)
	}
	if called {
		t.Fatal("rejected custom artifact transport was invoked")
	}
}
