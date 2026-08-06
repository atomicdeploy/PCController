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

	"golang.org/x/net/http/httpproxy"
	"pccontroller.local/controller/internal/netpolicy"
)

const (
	defaultGitHubAPI = "https://api.github.com"
	githubAPIVersion = "2022-11-28"
	requestTimeout   = 10 * time.Minute
	maxMetadataBytes = 2 << 20
)

type Client struct {
	http *http.Client
}

func NewClient(client *http.Client) *Client {
	if client == nil {
		proxy := httpproxy.FromEnvironment().ProxyFunc()
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = func(request *http.Request) (*url.URL, error) {
			return proxy(request.URL)
		}
		client = &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("update discovery exceeded five redirects")
				}
				previous := via[len(via)-1]
				if previous.URL.Scheme == "https" && request.URL.Scheme == "http" {
					return errors.New("update discovery refused an HTTPS-to-HTTP redirect")
				}
				if !sameAuthority(previous.URL, request.URL) {
					request.Header.Del("Authorization")
				}
				return nil
			},
		}
	}
	return &Client{http: client}
}

func (client *Client) get(ctx context.Context, rawURL, token, accept string) (*http.Response, error) {
	if client == nil || client.http == nil {
		return nil, errors.New("release discovery HTTP client is unavailable")
	}
	parsed, err := netpolicy.ParseHTTPURL(rawURL, "update URL")
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

func sameAuthority(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
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
