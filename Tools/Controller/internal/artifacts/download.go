package artifacts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"pccontroller.local/controller/internal/netpolicy"
)

const defaultDownloadTimeout = 10 * time.Minute

type Downloader struct {
	client  *http.Client
	scope   netpolicy.HTTPDestinationScope
	initErr error
}

// NewDownloader always enforces the public-source transport invariant. A
// non-nil client is a settings/*http.Transport template, not permission to
// reach loopback or the LAN.
func NewDownloader(template *http.Client) *Downloader {
	client, err := netpolicy.NewPublicHTTPClient(template, netpolicy.PublicHTTPClientOptions{
		Timeout: defaultDownloadTimeout, Operation: "artifact download",
		Subject: "artifact URL", MaximumRedirects: 5,
	})
	return &Downloader{client: client, scope: netpolicy.HTTPDestinationPublic, initErr: err}
}

// NewTrustedDownloader explicitly allows configured local destinations and
// custom transports. Use it only for tests or an authenticated/pinned peer
// path whose trust decision happened before construction.
func NewTrustedDownloader(client *http.Client) *Downloader {
	if client == nil {
		client = &http.Client{Timeout: defaultDownloadTimeout}
	}
	copy := *client
	if copy.Timeout == 0 {
		copy.Timeout = defaultDownloadTimeout
	}
	copy.CheckRedirect = (netpolicy.HTTPRedirectPolicy{
		Operation: "artifact download", Subject: "artifact URL", MaximumHops: 5,
		Scope: netpolicy.HTTPDestinationConfigured, Previous: copy.CheckRedirect,
	}).CheckRedirect
	return &Downloader{client: &copy, scope: netpolicy.HTTPDestinationConfigured}
}

func (downloader *Downloader) Fetch(
	ctx context.Context,
	store *Store,
	request FetchRequest,
	progress ProgressFunc,
) (Descriptor, error) {
	if downloader == nil {
		return Descriptor{}, errors.New("artifact downloader is unavailable")
	}
	if downloader.initErr != nil {
		return Descriptor{}, fmt.Errorf("initialize artifact downloader: %w", downloader.initErr)
	}
	if downloader.client == nil {
		return Descriptor{}, errors.New("artifact downloader is unavailable")
	}
	if store == nil {
		return Descriptor{}, errors.New("artifact store is unavailable")
	}
	parsed, err := netpolicy.ParseHTTPURLForScope(request.URL, "artifact URL", downloader.scope)
	if err != nil {
		return Descriptor{}, err
	}
	kind, err := ParseKind(string(request.Kind))
	if err != nil {
		return Descriptor{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Descriptor{}, err
	}
	httpRequest.Header.Set("Accept", mediaType(kind)+", application/octet-stream;q=0.8")
	if token := strings.TrimSpace(request.BearerToken); token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
	}
	if progress != nil {
		progress("downloading", 10, "requesting remote artifact")
	}
	response, err := downloader.client.Do(httpRequest)
	if err != nil {
		return Descriptor{}, fmt.Errorf("download artifact: %w", err)
	}
	defer response.Body.Close()
	effectiveURL := parsed
	if response.Request != nil && response.Request.URL != nil {
		effectiveURL = response.Request.URL
	}
	if err := netpolicy.ValidateHTTPURLForScope(effectiveURL.String(), "artifact URL", downloader.scope); err != nil {
		return Descriptor{}, fmt.Errorf("download artifact final URL: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return Descriptor{}, fmt.Errorf("download artifact: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if request.Bytes > 0 && response.ContentLength >= 0 && response.ContentLength != request.Bytes {
		return Descriptor{}, fmt.Errorf("artifact Content-Length mismatch: expected %d, received %d", request.Bytes, response.ContentLength)
	}
	if response.ContentLength > maxBytes(kind) {
		return Descriptor{}, fmt.Errorf("remote %s artifact exceeds %d-byte limit", kind, maxBytes(kind))
	}
	responseHash := strings.TrimSpace(response.Header.Get("X-Checksum-SHA256"))
	if responseHash != "" && strings.TrimSpace(request.SHA256) != "" {
		expected, hashErr := normalizeSHA256(request.SHA256)
		if hashErr != nil {
			return Descriptor{}, hashErr
		}
		fromHeader, hashErr := normalizeSHA256(responseHash)
		if hashErr != nil || expected != fromHeader {
			return Descriptor{}, errors.New("remote checksum header does not match requested SHA-256")
		}
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = responseFilename(response, effectiveURL, kind)
	}
	if progress != nil {
		progress("downloading", 35, "validating and content-addressing remote artifact")
	}
	descriptor, err := store.Put(response.Body, PutOptions{
		Kind: kind, Name: name, Source: "remote:" + parsed.Host,
		ExpectedSHA256: firstNonEmpty(request.SHA256, responseHash), ExpectedBytes: request.Bytes,
		BuildHash: request.BuildHash, BuildTimestamp: request.BuildTimestamp,
		PackedTimestamp: request.PackedTimestamp, Platform: request.Platform,
	})
	if err != nil {
		return Descriptor{}, err
	}
	if progress != nil {
		progress("downloaded", 100, "remote artifact verified")
	}
	return descriptor, nil
}

func responseFilename(response *http.Response, source *url.URL, kind Kind) string {
	if disposition := response.Header.Get("Content-Disposition"); disposition != "" {
		if _, parameters, err := mime.ParseMediaType(disposition); err == nil {
			if value := strings.TrimSpace(parameters["filename"]); value != "" && filepath.Base(value) == value {
				return value
			}
		}
	}
	if base := filepath.Base(strings.TrimSpace(source.Path)); base != "" && base != "." && base != "/" {
		return base
	}
	switch kind {
	case KindFirmware, KindFlashBackup:
		return string(kind) + ".hex"
	case KindEEPROM:
		return "eeprom.eep"
	default:
		return "host-executable.bin"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
