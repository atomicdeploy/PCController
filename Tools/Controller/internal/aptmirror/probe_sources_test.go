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

func TestSourceAdoptionIsConservativeAndInventoriesInactiveDefinitions(t *testing.T) {
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
	disabledPath := filepath.Join(directory, "old.list.disabled")
	_ = os.WriteFile(disabledPath, []byte("# deb https://mirror.example/ubuntu noble main\n"), 0o644)
	_ = os.WriteFile(filepath.Join(config.Paths.APTRoot, "sources.list.save"), []byte("deb https://archive.ubuntu.com/ubuntu noble main\n"), 0o644)
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
	for _, definition := range plan.SourceInventory {
		if definition.Path == unrelatedPath && definition.Classification != SourceClassThirdParty {
			t.Fatalf("third-party source classification=%q", definition.Classification)
		}
	}

	mixed := []byte("Types: deb\nURIs: https://archive.ubuntu.com/ubuntu https://packages.example.invalid/repo\nSuites: resolute\nComponents: main\n")
	_ = os.WriteFile(config.Paths.CanonicalSource, mixed, 0o644)
	if _, err := planUbuntuSources(config); err == nil {
		t.Fatal("mixed canonical source was overwritten")
	}
}

func TestDiscoveryRecursesVerifiesAndPrefersHTTPSWithoutAdoptingThirdParties(t *testing.T) {
	config := mirrorTestConfig(t)
	sourcesDirectory := filepath.Join(config.Paths.APTRoot, "sources.list.d")
	disabledDirectory := filepath.Join(sourcesDirectory, "disabled", "upgrade-history")
	if err := os.MkdirAll(disabledDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(disabledDirectory, "domestic.list.save")
	if err := os.WriteFile(historyPath, []byte("# deb http://history.example.ir/ubuntu jammy main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deb822Path := filepath.Join(sourcesDirectory, "historical.sources")
	deb822 := "Types: deb\nURIs: https://history.example.ir/ubuntu\nSuites: noble\nComponents: main\nEnabled: no\n\n" +
		"# Types: deb\n# URIs: https://history.example.ir/ubuntu\n# Suites: jammy-updates\n# Components: main\n"
	if err := os.WriteFile(deb822Path, []byte(deb822), 0o644); err != nil {
		t.Fatal(err)
	}
	thirdPartyPath := filepath.Join(sourcesDirectory, "third-party.sources")
	thirdParty := "Types: deb\nURIs: https://repo.dovecot.example/ce/ubuntu/noble\nSuites: noble\nComponents: main\n\n" +
		"Types: deb\nURIs: https://download.webmin.example/repository\nSuites: stable\nComponents: contrib\n\n" +
		"Types: deb\nURIs: https://dl.winehq.example/wine-builds/ubuntu\nSuites: noble\nComponents: main\n"
	if err := os.WriteFile(thirdPartyPath, []byte(thirdParty), 0o644); err != nil {
		t.Fatal(err)
	}
	ppaPath := filepath.Join(sourcesDirectory, "vendor-ubuntu-ppa-resolute.list")
	if err := os.WriteFile(ppaPath, []byte("deb https://ppa.example/owner/tool/ubuntu resolute main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mirrorsDirectory := filepath.Join(config.Paths.APTRoot, "mirrors")
	if err := os.MkdirAll(mirrorsDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyMirrorPath := filepath.Join(mirrorsDirectory, "ubuntu-archive.list")
	legacyMirror := "http://history.example.ir/ubuntu/ priority:10\n" +
		"https://history.example.ir/ubuntu/ priority:10 suite:jammy-security\n"
	if err := os.WriteFile(legacyMirrorPath, []byte(legacyMirror), 0o644); err != nil {
		t.Fatal(err)
	}

	firstPlan, err := planUbuntuSources(config)
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := planUbuntuSources(config)
	if err != nil {
		t.Fatal(err)
	}
	var historyCandidate Candidate
	for _, candidate := range firstPlan.DiscoveredCandidates {
		if strings.Contains(candidate.URI, "history.example.ir") {
			historyCandidate = candidate
		}
	}
	if historyCandidate.ID == "" || historyCandidate.URI != "https://history.example.ir/ubuntu/" || !historyCandidate.BypassProxy {
		t.Fatalf("preferred historical candidate=%+v", historyCandidate)
	}
	foundDeterministic := false
	for _, candidate := range secondPlan.DiscoveredCandidates {
		if candidate.URI == historyCandidate.URI && candidate.ID == historyCandidate.ID {
			foundDeterministic = true
		}
	}
	if !foundDeterministic {
		t.Fatalf("candidate identity was not deterministic: first=%+v second=%+v", firstPlan.DiscoveredCandidates, secondPlan.DiscoveredCandidates)
	}

	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	results := probeResults(config,
		ProbeResult{Status: ProbeVerified, Publication: now},
		ProbeResult{Status: ProbeVerified, Publication: now},
	)
	for _, candidate := range firstPlan.DiscoveredCandidates {
		for _, suite := range config.Suites() {
			result := ProbeResult{Status: ProbeUnsafe, Detail: "signature"}
			if strings.Contains(candidate.URI, "history.example.ir") {
				result = ProbeResult{Status: ProbeVerified, Publication: now}
			}
			results[stateKey(candidate.ID, suite)] = result
		}
	}
	executable := filepath.Join(t.TempDir(), "controller")
	if err := os.WriteFile(executable, []byte("verified-controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Install(context.Background(), InstallOptions{
		Config: config, ExecutableSource: executable, Now: now,
		Prober: &tableProber{results: results},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.DiscoveredCandidates) != 1 || report.DiscoveredCandidates[0].URI != "https://history.example.ir/ubuntu/" {
		t.Fatalf("verified discovered candidates=%+v", report.DiscoveredCandidates)
	}
	if routePriority(report.Refresh, report.DiscoveredCandidates[0].ID, config.Codename) != 10 {
		t.Fatalf("verified historical mirror was not domestic-first: %+v", report.Refresh.Routes)
	}
	if containsString(report.InactiveInventory, thirdPartyPath) {
		t.Fatalf("active third-party source mislabeled inactive: %q", report.InactiveInventory)
	}
	for _, wanted := range []string{historyPath, deb822Path} {
		if !containsString(report.InactiveInventory, wanted) {
			t.Fatalf("inactive source %s missing from %q", wanted, report.InactiveInventory)
		}
	}
	var verifiedHistorical, rejectedPPA, activeDovecot, activeWebmin, rejectedWineHQ bool
	for _, definition := range report.SourceInventory {
		switch {
		case strings.Contains(definition.URI, "history.example.ir"):
			if definition.Classification == SourceClassVerified && definition.CandidateID == report.DiscoveredCandidates[0].ID {
				verifiedHistorical = true
				for _, verification := range definition.Verification {
					if !strings.HasPrefix(verification.Suite, config.Codename) {
						t.Fatalf("historical suite was not mapped to current release: %+v", definition.Verification)
					}
				}
			}
		case strings.Contains(definition.URI, "ppa.example"):
			rejectedPPA = definition.Status == SourceStatusActive && definition.Classification == SourceClassThirdParty && len(definition.Verification) != 0
		case strings.Contains(definition.URI, "dovecot.example"):
			activeDovecot = definition.Status == SourceStatusActive && definition.Classification == SourceClassThirdParty && definition.CandidateID == ""
		case strings.Contains(definition.URI, "webmin.example"):
			activeWebmin = definition.Status == SourceStatusActive && definition.Classification == SourceClassThirdParty && definition.CandidateID == ""
		case strings.Contains(definition.URI, "winehq.example"):
			rejectedWineHQ = definition.Status == SourceStatusActive && definition.Classification == SourceClassThirdParty && len(definition.Verification) != 0
		}
	}
	if !verifiedHistorical || !rejectedPPA || !activeDovecot || !activeWebmin || !rejectedWineHQ {
		t.Fatalf("structured discovery missing classifications: verified=%t ppa=%t dovecot=%t webmin=%t winehq=%t inventory=%+v", verifiedHistorical, rejectedPPA, activeDovecot, activeWebmin, rejectedWineHQ, report.SourceInventory)
	}
	for _, candidate := range report.Refresh.Candidates {
		if strings.Contains(candidate.CandidateID, "ppa") {
			t.Fatalf("rejected PPA reached final refresh: %+v", candidate)
		}
	}
}

func TestDiscoveryHTTPSPreferenceStillDisablesActiveHTTPSpelling(t *testing.T) {
	config := mirrorTestConfig(t)
	directory := filepath.Join(config.Paths.APTRoot, "sources.list.d")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "historical.list")
	if err := os.WriteFile(activePath, []byte("deb http://history.example.ir/ubuntu resolute main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(directory, "historical.list.save")
	if err := os.WriteFile(backupPath, []byte("# deb https://history.example.ir/ubuntu noble main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := planUbuntuSources(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.DiscoveredCandidates) != 1 || plan.DiscoveredCandidates[0].URI != "https://history.example.ir/ubuntu/" {
		t.Fatalf("HTTPS candidate was not preferred: %+v", plan.DiscoveredCandidates)
	}
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	results := probeResults(config,
		ProbeResult{Status: ProbeVerified, Publication: now},
		ProbeResult{Status: ProbeVerified, Publication: now},
	)
	for _, suite := range config.Suites() {
		results[stateKey(plan.DiscoveredCandidates[0].ID, suite)] = ProbeResult{Status: ProbeVerified, Publication: now}
	}
	executable := filepath.Join(t.TempDir(), "controller")
	if err := os.WriteFile(executable, []byte("verified-controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Install(context.Background(), InstallOptions{
		Config: config, ExecutableSource: executable, Now: now,
		Prober: &tableProber{results: results},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.DiscoveredCandidates) != 1 || report.DiscoveredCandidates[0].URI != "https://history.example.ir/ubuntu/" {
		t.Fatalf("persisted discovered candidates=%+v", report.DiscoveredCandidates)
	}
	if !containsString(report.DisabledSources, activePath) {
		t.Fatalf("active HTTP spelling was not disabled after HTTPS adoption: %q", report.DisabledSources)
	}
}

func TestDiscoveryDoesNotParseSymlinkOversizeOrCommentProse(t *testing.T) {
	config := mirrorTestConfig(t)
	directory := filepath.Join(config.Paths.APTRoot, "sources.list.d", "disabled")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.list")
	if err := os.WriteFile(target, []byte("deb https://symlink.example.ir/ubuntu resolute main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(directory, "symlink.list.save")
	if err := os.Symlink(target, symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	oversize := filepath.Join(directory, "oversize.list.save")
	content := make([]byte, maximumSourceDefinitionBytes+1)
	copy(content, "deb https://oversize.example.ir/ubuntu resolute main\n")
	if err := os.WriteFile(oversize, content, 0o644); err != nil {
		t.Fatal(err)
	}
	prose := filepath.Join(config.Paths.APTRoot, "sources.list.save")
	if err := os.MkdirAll(filepath.Dir(prose), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prose, []byte("# Ubuntu sources moved to a deb822 file.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(directory, "private.list.save")
	if err := os.WriteFile(private, []byte("deb https://name:secret@private.example/ubuntu resolute main\n"+
		"deb https://private.example/ubuntu?token=secret resolute main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := planUbuntuSources(config)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(plan.InactiveInventory, prose) {
		t.Fatalf("comment prose was misreported as a source definition: %q", plan.InactiveInventory)
	}
	for _, path := range []string{symlink, oversize} {
		if !containsString(plan.InactiveInventory, path) {
			t.Fatalf("unsafe inactive file %s missing from inventory", path)
		}
	}
	var ignored int
	for _, definition := range plan.SourceInventory {
		if (definition.Path == symlink || definition.Path == oversize) &&
			definition.Status == SourceStatusIgnored && definition.Classification == SourceClassUnsafe {
			ignored++
		}
		if strings.Contains(definition.URI, "symlink.example") || strings.Contains(definition.URI, "oversize.example") {
			t.Fatalf("unsafe file content was parsed: %+v", definition)
		}
		if definition.Path == private && (definition.URI != "" || definition.Classification != SourceClassUnsafe) {
			t.Fatalf("credential-bearing source was exposed: %+v", definition)
		}
	}
	if ignored != 2 || len(plan.DiscoveredCandidates) != 0 || strings.Contains(fmt.Sprintf("%+v", plan.SourceInventory), "secret") {
		t.Fatalf("unsafe inventory ignored=%d candidates=%+v inventory=%+v", ignored, plan.DiscoveredCandidates, plan.SourceInventory)
	}
}

func TestDiscoveryRejectsActiveSourceSymlink(t *testing.T) {
	config := mirrorTestConfig(t)
	directory := filepath.Join(config.Paths.APTRoot, "sources.list.d")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.list")
	if err := os.WriteFile(target, []byte("deb https://archive.ubuntu.com/ubuntu resolute main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, "active.list")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := planUbuntuSources(config); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("active source symlink was not rejected: %v", err)
	}
}

func TestDiscoveryBoundsRecursiveFileInventory(t *testing.T) {
	config := mirrorTestConfig(t)
	directory := filepath.Join(config.Paths.APTRoot, "sources.list.d", "disabled")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	// sources.list itself consumes one inventory slot even when absent, so this
	// final backup must trip the bound before any file content is parsed.
	for index := 0; index < maximumSourceInventoryFiles; index++ {
		path := filepath.Join(directory, fmt.Sprintf("history-%03d.list.save", index))
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := planUbuntuSources(config); !errors.Is(err, errSourceInventoryLimit) {
		t.Fatalf("recursive inventory bound error=%v", err)
	}
}

func TestSourceAdoptionRejectsMixedReleaseSuitesWithoutMutation(t *testing.T) {
	tests := []struct {
		name     string
		relative string
		content  string
	}{
		{
			name:     "one-line selected and foreign suites",
			relative: "sources.list",
			content: "deb https://archive.ubuntu.com/ubuntu resolute main\n" +
				"deb https://archive.ubuntu.com/ubuntu noble resolute main\n",
		},
		{
			name:     "deb822 selected and foreign suites",
			relative: filepath.Join("sources.list.d", "mixed.sources"),
			content:  "Types: deb\nURIs: https://archive.ubuntu.com/ubuntu\nSuites: resolute noble\nComponents: main\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := mirrorTestConfig(t)
			sourcePath := filepath.Join(config.Paths.APTRoot, test.relative)
			if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
				t.Fatal(err)
			}
			original := []byte(test.content)
			if err := os.WriteFile(sourcePath, original, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := planUbuntuSources(config); err == nil ||
				!strings.Contains(err.Error(), "suites outside the selected release") {
				t.Fatalf("source adoption did not reject mixed release suites: %v", err)
			}
			after, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(original) {
				t.Fatalf("refused source was mutated:\n%s", after)
			}
			for _, path := range []string{config.Paths.CanonicalSource, config.Paths.StableExecutable} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("refused apply created %s: %v", path, err)
				}
			}
		})
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
	for _, name := range []string{"ALL_PROXY", "HTTP_PROXY", "HTTPS_PROXY", "all_proxy", "http_proxy", "https_proxy"} {
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
