package releaseplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pccontroller.local/controller/internal/netpolicy"
)

const (
	defaultGitHubAPI = "https://api.github.com"
	githubAPIVersion = "2022-11-28"
	requestTimeout   = 10 * time.Minute
	maxMetadataBytes = 2 << 20
)

type Client struct {
	http    *http.Client
	scope   netpolicy.HTTPDestinationScope
	initErr error
}

// NewClient always enforces the public-source transport invariant. A non-nil
// client is a settings/*http.Transport template, not permission to reach a
// local destination.
func NewClient(template *http.Client) *Client {
	client, err := netpolicy.NewPublicHTTPClient(template, netpolicy.PublicHTTPClientOptions{
		Timeout: requestTimeout, Operation: "update discovery",
		Subject: "update URL", MaximumRedirects: 5,
	})
	return &Client{http: client, scope: netpolicy.HTTPDestinationPublic, initErr: err}
}

// NewTrustedClient explicitly allows configured local destinations and custom
// transports. Use it only for tests or an authenticated/pinned peer path whose
// trust decision happened before construction.
func NewTrustedClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	copy := *client
	if copy.Timeout == 0 {
		copy.Timeout = requestTimeout
	}
	copy.CheckRedirect = (netpolicy.HTTPRedirectPolicy{
		Operation: "update discovery", Subject: "update URL", MaximumHops: 5,
		Scope: netpolicy.HTTPDestinationConfigured, Previous: copy.CheckRedirect,
	}).CheckRedirect
	return &Client{http: &copy, scope: netpolicy.HTTPDestinationConfigured}
}

func (client *Client) get(ctx context.Context, rawURL, token, accept string) (*http.Response, error) {
	if client == nil {
		return nil, errors.New("release discovery HTTP client is unavailable")
	}
	if client.initErr != nil {
		return nil, fmt.Errorf("initialize release discovery client: %w", client.initErr)
	}
	if client.http == nil {
		return nil, errors.New("release discovery HTTP client is unavailable")
	}
	parsed, err := netpolicy.ParseHTTPURLForScope(rawURL, "update URL", client.scope)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if token = strings.TrimSpace(token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", safeURL(rawURL), err)
	}
	effectiveURL := parsed
	if response.Request != nil && response.Request.URL != nil {
		effectiveURL = response.Request.URL
	}
	if err := netpolicy.ValidateHTTPURLForScope(effectiveURL.String(), "update URL", client.scope); err != nil {
		response.Body.Close()
		return nil, fmt.Errorf("download %s final URL: %w", safeURL(rawURL), err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %d: %s", safeURL(rawURL), response.StatusCode, strings.TrimSpace(string(body)))
	}
	return response, nil
}

func decodeJSONResponse(response *http.Response, destination any) error {
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxMetadataBytes+1))
	return decodeSingleJSON(decoder, destination)
}

func decodeSingleJSON(decoder *json.Decoder, destination any) error {
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode update metadata: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("metadata contains trailing JSON")
		}
		return err
	}
	return nil
}

func validateRemoteURL(raw string) error {
	return netpolicy.ValidateHTTPURL(raw, "update URL")
}

func safeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "update source"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func normalizeDigest(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("SHA-256 must contain 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("SHA-256 is not hexadecimal")
	}
	return value, nil
}

func githubAPIBase(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		value = defaultGitHubAPI
	}
	if err := validateRemoteURL(value); err != nil {
		return "", fmt.Errorf("GitHub API base: %w", err)
	}
	return value, nil
}
