package aptmirror

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	maximumInReleaseBytes = 8 << 20
	maximumReleaseBytes   = 64 << 10
)

var errUnsafeMirrorRedirect = errors.New("unsafe mirror redirect")

type HTTPProber struct {
	now    func() time.Time
	verify func(context.Context, string, []byte) error
}

func NewHTTPProber(Config) *HTTPProber {
	return &HTTPProber{
		now: time.Now,
		verify: func(ctx context.Context, keyring string, content []byte) error {
			command := exec.CommandContext(ctx, "/usr/bin/gpgv", "--keyring", keyring, "-")
			command.Stdin = bytes.NewReader(content)
			command.Stdout = io.Discard
			command.Stderr = io.Discard
			return command.Run()
		},
	}
}

func (prober *HTTPProber) Probe(ctx context.Context, config Config, candidate Candidate, suite string) ProbeResult {
	if !candidateHasSuite(candidate, suite, config) {
		return ProbeResult{Status: ProbeMissing, Detail: "missing"}
	}
	base, err := url.Parse(candidate.URI)
	if err != nil {
		return ProbeResult{Status: ProbeUnsafe, Detail: "identity"}
	}
	client := mirrorHTTPClient(config, base, candidate.BypassProxy)
	inReleaseURL, err := base.Parse("dists/" + suite + "/InRelease")
	if err != nil {
		return ProbeResult{Status: ProbeUnsafe, Detail: "identity"}
	}
	content, status, err := fetchBounded(ctx, client, inReleaseURL.String(), maximumInReleaseBytes)
	if err != nil {
		return ProbeResult{Status: status, Detail: probeDetailForStatus(status)}
	}
	if err := prober.verify(ctx, config.Paths.Keyring, content); err != nil {
		return ProbeResult{Status: ProbeUnsafe, Detail: "signature"}
	}
	release, err := parseInRelease(content)
	if err != nil || release.Origin != "Ubuntu" || release.Label != "Ubuntu" ||
		release.Suite != suite || release.Codename != config.Codename {
		return ProbeResult{Status: ProbeUnsafe, Detail: "identity"}
	}
	for _, architecture := range config.Architectures {
		if !containsString(release.Architectures, architecture) {
			return ProbeResult{Status: ProbeUnsafe, Detail: "identity"}
		}
	}
	for _, component := range config.Components {
		if !containsString(release.Components, component) {
			return ProbeResult{Status: ProbeUnsafe, Detail: "identity"}
		}
	}
	now := prober.now().UTC()
	if release.Date.After(now.Add(time.Duration(config.FutureToleranceSeconds) * time.Second)) {
		return ProbeResult{Status: ProbeUnsafe, Detail: "future"}
	}
	maximumAge := config.FirstRunMovingAgeSeconds
	if suite == config.Codename {
		maximumAge = config.FirstRunBaseAgeSeconds
	}
	effectiveValidUntil := release.Date.Add(time.Duration(maximumAge) * time.Second)
	if !release.ValidUntil.IsZero() && release.ValidUntil.Before(effectiveValidUntil) {
		effectiveValidUntil = release.ValidUntil
	}
	// Ubuntu does not consistently publish Valid-Until. In that case the
	// Controller supplies the stricter suite-specific Date-derived ceiling;
	// a signed Valid-Until may only shorten, never extend, that ceiling.
	if !release.ValidUntil.IsZero() && !now.Before(release.ValidUntil) {
		return ProbeResult{Status: ProbeUnsafe, Detail: "expired"}
	}
	// Each configured component must expose the tiny binary-ARCH/Release file
	// referenced by the signed InRelease. This rejects mirrors which publish a
	// plausible signed header but omit or corrupt the package-index topology.
	for _, component := range config.Components {
		for _, architecture := range config.Architectures {
			relative := fmt.Sprintf("%s/binary-%s/Release", component, architecture)
			reference, ok := release.SHA256[relative]
			if !ok {
				return ProbeResult{Status: ProbeUnsafe, Detail: "index-reference"}
			}
			indexURL, err := base.Parse("dists/" + suite + "/" + relative)
			if err != nil {
				return ProbeResult{Status: ProbeUnsafe, Detail: "index-reference"}
			}
			index, indexStatus, err := fetchBounded(ctx, client, indexURL.String(), maximumReleaseBytes)
			if err != nil {
				return ProbeResult{Status: indexStatus, Detail: probeDetailForStatus(indexStatus)}
			}
			digest := sha256.Sum256(index)
			if int64(len(index)) != reference.Size || !bytes.Equal(digest[:], reference.Hash[:]) {
				return ProbeResult{Status: ProbeUnsafe, Detail: "content-hash"}
			}
		}
	}
	return ProbeResult{
		Status: ProbeVerified, Publication: release.Date,
		ValidUntil: effectiveValidUntil,
	}
}

type releaseReference struct {
	Hash [sha256.Size]byte
	Size int64
}

type parsedRelease struct {
	Origin        string
	Label         string
	Suite         string
	Codename      string
	Architectures []string
	Components    []string
	Date          time.Time
	ValidUntil    time.Time
	SHA256        map[string]releaseReference
}

func parseInRelease(signed []byte) (parsedRelease, error) {
	cleartext, err := clearSignedPayload(signed)
	if err != nil {
		return parsedRelease{}, err
	}
	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(cleartext, '\n', '\n'))))
	headers, err := reader.ReadMIMEHeader()
	if err != nil {
		return parsedRelease{}, err
	}
	publication, err := parseReleaseTime(strings.TrimSpace(headers.Get("Date")))
	if err != nil {
		return parsedRelease{}, errors.New("invalid publication date")
	}
	var validUntil time.Time
	if raw := strings.TrimSpace(headers.Get("Valid-Until")); raw != "" {
		validUntil, err = parseReleaseTime(raw)
		if err != nil {
			return parsedRelease{}, errors.New("invalid Valid-Until")
		}
	}
	references := make(map[string]releaseReference)
	fields := strings.Fields(headers.Get("SHA256"))
	if len(fields) == 0 || len(fields)%3 != 0 {
		return parsedRelease{}, errors.New("missing signed SHA256 references")
	}
	for offset := 0; offset < len(fields); offset += 3 {
		digest, err := hex.DecodeString(fields[offset])
		if err != nil || len(digest) != sha256.Size {
			return parsedRelease{}, errors.New("invalid signed SHA256 reference")
		}
		size, err := strconv.ParseInt(fields[offset+1], 10, 64)
		if err != nil || size < 0 {
			return parsedRelease{}, errors.New("invalid signed SHA256 size")
		}
		path := fields[offset+2]
		if strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			return parsedRelease{}, errors.New("unsafe signed SHA256 path")
		}
		var hash [sha256.Size]byte
		copy(hash[:], digest)
		references[path] = releaseReference{Hash: hash, Size: size}
	}
	return parsedRelease{
		Origin: headers.Get("Origin"), Label: headers.Get("Label"),
		Suite: headers.Get("Suite"), Codename: headers.Get("Codename"),
		Architectures: strings.Fields(headers.Get("Architectures")),
		Components:    strings.Fields(headers.Get("Components")),
		Date:          publication.UTC(), ValidUntil: validUntil.UTC(), SHA256: references,
	}, nil
}

func parseReleaseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC1123, time.RFC1123Z} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return http.ParseTime(value)
}

func clearSignedPayload(signed []byte) ([]byte, error) {
	const begin = "-----BEGIN PGP SIGNED MESSAGE-----"
	const signature = "-----BEGIN PGP SIGNATURE-----"
	text := strings.ReplaceAll(string(signed), "\r\n", "\n")
	if !strings.HasPrefix(text, begin+"\n") {
		return nil, errors.New("not a clearsigned InRelease")
	}
	headerEnd := strings.Index(text, "\n\n")
	if headerEnd < 0 {
		return nil, errors.New("invalid clearsigned header")
	}
	payload := text[headerEnd+2:]
	signatureOffset := strings.Index(payload, "\n"+signature)
	if signatureOffset < 0 {
		return nil, errors.New("missing clearsigned signature")
	}
	payload = payload[:signatureOffset]
	lines := strings.Split(payload, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "- ") {
			lines[index] = strings.TrimPrefix(line, "- ")
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func mirrorHTTPClient(config Config, base *url.URL, bypassProxy bool) *http.Client {
	dialer := &net.Dialer{
		Timeout:   config.connectTimeout(),
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   config.connectTimeout(),
		ResponseHeaderTimeout: config.transferTimeout(),
		IdleConnTimeout:       30 * time.Second,
	}
	if bypassProxy {
		transport.Proxy = nil
	}
	return &http.Client{
		Transport: transport,
		Timeout:   config.transferTimeout(),
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			prior := base
			if len(via) != 0 {
				prior = via[len(via)-1].URL
			}
			if len(via) >= 5 || !strings.EqualFold(request.URL.Host, base.Host) ||
				(request.URL.Scheme != prior.Scheme && !(prior.Scheme == "http" && request.URL.Scheme == "https")) {
				return errUnsafeMirrorRedirect
			}
			return nil
		},
	}
}

func fetchBounded(ctx context.Context, client *http.Client, location string, maximum int64) ([]byte, ProbeStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, ProbeUnsafe, err
	}
	request.Header.Set("User-Agent", "PCController-APT-Mirror-Health/1")
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, errUnsafeMirrorRedirect) {
			return nil, ProbeUnsafe, err
		}
		return nil, ProbeTransient, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return nil, ProbeMissing, errors.New("mirror object is missing")
	}
	if response.StatusCode == http.StatusProxyAuthRequired || response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusTooEarly || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, ProbeTransient, errors.New("mirror is temporarily unavailable")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ProbeMissing, errors.New("mirror object is unavailable")
	}
	if response.ContentLength > maximum {
		return nil, ProbeUnsafe, errors.New("mirror object exceeds its safety limit")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, ProbeTransient, err
	}
	if int64(len(content)) > maximum {
		return nil, ProbeUnsafe, errors.New("mirror object exceeds its safety limit")
	}
	return content, ProbeVerified, nil
}

func probeDetailForStatus(status ProbeStatus) string {
	switch status {
	case ProbeTransient:
		return "transport"
	case ProbeMissing:
		return "missing"
	default:
		return "identity"
	}
}
