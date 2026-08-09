package aptmirror

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func signedFixture(suite, codename, architecture string, date time.Time, validUntil string, releases map[string][]byte) []byte {
	var sha strings.Builder
	for path, content := range releases {
		digest := sha256.Sum256(content)
		fmt.Fprintf(&sha, " %x %d %s\n", digest, len(content), path)
	}
	valid := ""
	if validUntil != "" {
		valid = "Valid-Until: " + validUntil + "\n"
	}
	payload := fmt.Sprintf("Origin: Ubuntu\nLabel: Ubuntu\nSuite: %s\nCodename: %s\nDate: %s\n%sArchitectures: %s\nComponents: main universe\nSHA256:\n%s",
		suite, codename, date.UTC().Format(time.RFC1123), valid, architecture, sha.String())
	return []byte("-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\n" + payload + "-----BEGIN PGP SIGNATURE-----\nfixture\n-----END PGP SIGNATURE-----\n")
}

func TestHTTPProberValidatesSignedReferencedComponentReleases(t *testing.T) {
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	suite := "resolute-security"
	releases := map[string][]byte{
		"main/binary-amd64/Release":     []byte("Archive: resolute-security\nComponent: main\n"),
		"universe/binary-amd64/Release": []byte("Archive: resolute-security\nComponent: universe\n"),
		"main/binary-i386/Release":      []byte("Archive: resolute-security\nComponent: main\n"),
		"universe/binary-i386/Release":  []byte("Archive: resolute-security\nComponent: universe\n"),
	}
	fixture := signedFixture(suite, "resolute", "amd64 i386", now.Add(-time.Hour), now.Add(24*time.Hour).Format(time.RFC1123), releases)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		prefix := "/repository/ubuntu/dists/" + suite + "/"
		switch {
		case request.URL.Path == prefix+"InRelease":
			_, _ = response.Write(fixture)
		case strings.HasPrefix(request.URL.Path, prefix):
			content, ok := releases[strings.TrimPrefix(request.URL.Path, prefix)]
			if !ok {
				http.NotFound(response, request)
				return
			}
			_, _ = response.Write(content)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	config := DomesticFirstConfig("resolute", "amd64", "i386")
	config.Components = []string{"main", "universe"}
	prober := &HTTPProber{now: func() time.Time { return now }, verify: func(context.Context, string, []byte) error { return nil }}
	result := prober.Probe(context.Background(), config, Candidate{ID: "liara", Role: RoleDomestic, URI: server.URL + "/repository/ubuntu/"}, suite)
	if result.Status != ProbeVerified || result.Publication.IsZero() {
		t.Fatalf("valid signed fixture result=%+v", result)
	}

	// Reproduce the broken Liara-security shape: signed InRelease references a
	// binary-i386 Release object which the endpoint does not actually serve.
	delete(releases, "universe/binary-i386/Release")
	result = prober.Probe(context.Background(), config, Candidate{ID: "liara", Role: RoleDomestic, URI: server.URL + "/repository/ubuntu/"}, suite)
	if result.Status != ProbeMissing {
		t.Fatalf("missing signed-index object was not rejected: %+v", result)
	}
}

func TestHTTPProberRejectsTamperExpiryAndUnsafeRedirect(t *testing.T) {
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	config := DomesticFirstConfig("resolute", "amd64")
	config.Components = []string{"main"}
	release := []byte("Archive: resolute-updates\nComponent: tampered\n")
	references := map[string][]byte{"main/binary-amd64/Release": []byte("expected")}
	fixture := signedFixture("resolute-updates", "resolute", "amd64", now.Add(-time.Hour), now.Add(time.Hour).Format(time.RFC1123), references)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/InRelease") {
			_, _ = response.Write(fixture)
			return
		}
		_, _ = response.Write(release)
	}))
	defer server.Close()
	prober := &HTTPProber{now: func() time.Time { return now }, verify: func(context.Context, string, []byte) error { return nil }}
	candidate := Candidate{ID: "mirror", Role: RoleDomestic, URI: server.URL + "/ubuntu/"}
	if result := prober.Probe(context.Background(), config, candidate, "resolute-updates"); result.Status != ProbeUnsafe || result.Detail != "content-hash" {
		t.Fatalf("tamper result=%+v", result)
	}

	expired := signedFixture("resolute-updates", "resolute", "amd64", now.Add(-time.Hour), now.Add(-time.Minute).Format(time.RFC1123), references)
	fixture = expired
	if result := prober.Probe(context.Background(), config, candidate, "resolute-updates"); result.Status != ProbeUnsafe || result.Detail != "expired" {
		t.Fatalf("expiry result=%+v", result)
	}

	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL+request.URL.Path, http.StatusFound)
	}))
	defer redirect.Close()
	redirectCandidate := Candidate{ID: "redirect", Role: RoleDomestic, URI: redirect.URL + "/ubuntu/"}
	if result := prober.Probe(context.Background(), config, redirectCandidate, "resolute-updates"); result.Status != ProbeUnsafe {
		t.Fatalf("cross-host redirect retained LKG eligibility: %+v", result)
	}
}

func TestUbuntuReleaseUTCDateParses(t *testing.T) {
	parsed, err := parseReleaseTime("Thu, 25 Apr 2024 15:10:33 UTC")
	if err != nil || parsed.Year() != 2024 || parsed.Location() != time.UTC {
		t.Fatalf("Ubuntu UTC date parse=%v err=%v", parsed, err)
	}
}

func TestHTTPProberRejectsVerifierFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("not trusted"))
	}))
	defer server.Close()
	config := DomesticFirstConfig("resolute", "amd64")
	prober := &HTTPProber{
		now:    time.Now,
		verify: func(context.Context, string, []byte) error { return fmt.Errorf("bad signature") },
	}
	result := prober.Probe(context.Background(), config, Candidate{ID: "mirror", Role: RoleDomestic, URI: server.URL + "/ubuntu/"}, "resolute")
	if result.Status != ProbeUnsafe || result.Detail != "signature" {
		t.Fatalf("signature failure=%+v", result)
	}
}

func TestSourceAdoptionIsConservativeAndInventoriesInactiveFiles(t *testing.T) {
	config := mirrorTestConfig(t)
	directory := filepath.Join(config.Paths.APTRoot, "sources.list.d")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	unrelated := []byte("Types: deb\nURIs: https://packages.example.invalid/repo\nSuites: stable\nComponents: main\n\n\n")
	unrelatedPath := filepath.Join(directory, "third-party.sources")
	_ = os.WriteFile(unrelatedPath, unrelated, 0o644)
	legacyPath := filepath.Join(config.Paths.APTRoot, "sources.list")
	_ = os.WriteFile(legacyPath, []byte("deb https://archive.ubuntu.com/ubuntu resolute main\n"), 0o644)
	disabledPath := filepath.Join(directory, "old.sources.disabled")
	_ = os.WriteFile(disabledPath, []byte("# old ubuntu source\n"), 0o644)
	_ = os.WriteFile(filepath.Join(config.Paths.APTRoot, "sources.list.save"), []byte("# ubuntu backup\n"), 0o644)
	plan, err := planUbuntuSources(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed := plan.Edits[unrelatedPath]; changed {
		t.Fatal("third-party source was rewritten")
	}
	if !strings.Contains(string(plan.Edits[legacyPath]), "Disabled by PCController") {
		t.Fatal("legacy Ubuntu source was not disabled")
	}
	if len(plan.InactiveInventory) < 2 {
		t.Fatalf("inactive inventory=%q", plan.InactiveInventory)
	}

	mixed := []byte("Types: deb\nURIs: https://archive.ubuntu.com/ubuntu https://packages.example.invalid/repo\nSuites: resolute\nComponents: main\n")
	_ = os.WriteFile(config.Paths.CanonicalSource, mixed, 0o644)
	if _, err := planUbuntuSources(config); err == nil {
		t.Fatal("mixed canonical source was overwritten")
	}
}

func TestInstallDryRunAndGeneratedTopologyDoNotMutate(t *testing.T) {
	config := mirrorTestConfig(t)
	config.Architectures = []string{"amd64", "i386"}
	executable := filepath.Join(t.TempDir(), "controller")
	if err := os.WriteFile(executable, []byte("verified-controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	results := probeResults(config, ProbeResult{Status: ProbeVerified, Publication: now}, ProbeResult{Status: ProbeVerified, Publication: now})
	report, err := Install(context.Background(), InstallOptions{
		Config: config, ExecutableSource: executable,
		ProxyEnvironment: []string{"HTTPS_PROXY=http://name:secret@proxy.invalid", "NO_PROXY=localhost"},
		Prober:           &tableProber{results: results}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied {
		t.Fatal("dry-run reported applied")
	}
	for _, path := range report.ManagedFiles {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("dry-run created %s", path)
		}
	}
	source := string(SourceDeb822(config))
	if strings.Count(source, "mirror+file:") != 1 || !strings.Contains(source, "Architectures: amd64 i386") {
		t.Fatalf("generated source topology:\n%s", source)
	}
	service := string(SystemdService(config))
	for _, wanted := range []string{config.Paths.StableExecutable, "EnvironmentFile=-" + config.Paths.ProxyEnvironment, "TimeoutStartSec=5min"} {
		if !strings.Contains(service, wanted) {
			t.Fatalf("service missing %q:\n%s", wanted, service)
		}
	}
	if timer := string(SystemdTimer()); !strings.Contains(timer, "OnUnitActiveSec=2h") || strings.Contains(timer, "OnUnitActiveSec=15min") {
		t.Fatalf("mirror timer wastes bandwidth or lacks the reviewed two-hour cadence:\n%s", timer)
	}
	if strings.Contains(fmt.Sprintf("%+v", report), "secret") {
		t.Fatal("report leaked proxy secret")
	}
	resilience := string(APTResilienceConfig(config))
	for _, host := range []string{"mirror.example"} {
		if !strings.Contains(resilience, "Acquire::http::Proxy::"+host+" \"DIRECT\"") ||
			!strings.Contains(resilience, "Acquire::https::Proxy::"+host+" \"DIRECT\"") {
			t.Fatalf("APT proxy policy omitted domestic direct route for %s:\n%s", host, resilience)
		}
	}
	if strings.Contains(resilience, "Proxy::archive.ubuntu.com \"DIRECT\"") {
		t.Fatalf("official fallback incorrectly bypassed configured proxy:\n%s", resilience)
	}
	proxyFile := string(proxyEnvironmentFile([]string{"ALL_PROXY=socks5://name:secret@proxy.invalid:1080"}))
	for _, name := range []string{"ALL_PROXY", "HTTP_PROXY", "HTTPS_PROXY"} {
		if !strings.Contains(proxyFile, name+"=\"socks5://name:secret@proxy.invalid:1080\"") {
			t.Fatalf("ALL_PROXY fallback omitted %s without logging it: %q", name, proxyFile)
		}
	}
}

func TestSourceActivationWritesCanonicalBeforeDisablingLegacy(t *testing.T) {
	config := DomesticFirstConfig("resolute", "amd64", "i386")
	legacy := filepath.Join(config.Paths.APTRoot, "sources.list")
	other := filepath.Join(config.Paths.APTRoot, "sources.list.d", "old-ubuntu.sources")
	plan := sourcePlan{Edits: map[string][]byte{
		legacy: []byte("# disabled legacy\n"),
		other:  []byte("Enabled: no\n"),
	}}
	content := map[string][]byte{
		config.Paths.CanonicalSource: SourceDeb822(config),
		legacy:                       plan.Edits[legacy],
		other:                        plan.Edits[other],
	}
	var order []string
	interrupted := fmt.Errorf("simulated interruption")
	err := activateManagedSources(config, content, plan, func(path string, _ []byte, _ os.FileMode) error {
		order = append(order, path)
		if path == legacy {
			return interrupted
		}
		return nil
	})
	if !errors.Is(err, interrupted) {
		t.Fatalf("activation error=%v", err)
	}
	if len(order) < 2 || order[0] != config.Paths.CanonicalSource || order[1] != legacy {
		t.Fatalf("source activation order=%q", order)
	}
}
