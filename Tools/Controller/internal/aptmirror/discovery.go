package aptmirror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	maximumSourceDefinitionBytes = int64(1 << 20)
	maximumSourceInventoryBytes  = int64(8 << 20)
	maximumSourceInventoryFiles  = 512
	maximumSourceInventoryDepth  = 4
	maximumSourceDefinitions     = 4096
	maximumDiscoveredCandidates  = 32
)

var (
	errSourceFileMissing    = fs.ErrNotExist
	errSourceFileSymlink    = errors.New("source definition is a symbolic link")
	errSourceFileNonRegular = errors.New("source definition is not a regular file")
	errSourceFileTooLarge   = errors.New("source definition exceeds its size limit")
	errSourceInventoryLimit = errors.New("APT source inventory file limit exceeded")
)

type SourceDefinitionStatus string

const (
	SourceStatusActive     SourceDefinitionStatus = "active"
	SourceStatusCommented  SourceDefinitionStatus = "commented"
	SourceStatusDisabled   SourceDefinitionStatus = "disabled"
	SourceStatusBackup     SourceDefinitionStatus = "backup"
	SourceStatusMirrorList SourceDefinitionStatus = "legacy-mirror-list"
	SourceStatusIgnored    SourceDefinitionStatus = "ignored"
)

type SourceDefinitionClassification string

const (
	SourceClassManagedTopology SourceDefinitionClassification = "managed-topology"
	SourceClassConfigured      SourceDefinitionClassification = "configured-ubuntu-archive"
	SourceClassVerified        SourceDefinitionClassification = "verified-ubuntu-archive"
	SourceClassUnverified      SourceDefinitionClassification = "unverified-ubuntu-archive"
	SourceClassThirdParty      SourceDefinitionClassification = "third-party"
	SourceClassUnsafe          SourceDefinitionClassification = "unsafe"
)

// SourceDefinition is a credential-free record of one parsed APT source or
// legacy mirror-list entry. CandidateID links a definition to Refresh.Candidates
// without treating a filename or an "ubuntu" substring as archive identity.
type SourceDefinition struct {
	Path           string                         `json:"path"`
	Line           int                            `json:"line,omitempty"`
	Status         SourceDefinitionStatus         `json:"status"`
	URI            string                         `json:"uri,omitempty"`
	Suites         []string                       `json:"suites,omitempty"`
	Classification SourceDefinitionClassification `json:"classification"`
	CandidateID    string                         `json:"candidate_id,omitempty"`
	Verification   []CandidateReport              `json:"verification,omitempty"`
	Detail         string                         `json:"detail,omitempty"`
}

type sourceDocumentKind uint8

const (
	sourceDocumentActive sourceDocumentKind = iota + 1
	sourceDocumentBackup
	sourceDocumentMirrorList
)

type sourceDocumentPath struct {
	Path string
	Kind sourceDocumentKind
}

type sourceDocument struct {
	sourceDocumentPath
	Content []byte
}

type sourceDiscovery struct {
	Definitions       []SourceDefinition
	Candidates        []Candidate
	InactiveInventory []string
}

func discoverSourceDefinitions(config Config) (sourceDiscovery, error) {
	paths, err := collectSourceDocumentPaths(config)
	if err != nil {
		return sourceDiscovery{}, err
	}
	if len(paths) > maximumSourceInventoryFiles {
		return sourceDiscovery{}, fmt.Errorf("APT source inventory contains %d files; limit is %d", len(paths), maximumSourceInventoryFiles)
	}
	var discovery sourceDiscovery
	var total int64
	for _, item := range paths {
		content, readErr := readBoundedSourceFile(item.Path, maximumSourceDefinitionBytes)
		if readErr != nil {
			if errors.Is(readErr, errSourceFileMissing) {
				continue
			}
			if item.Kind == sourceDocumentActive {
				return sourceDiscovery{}, fmt.Errorf("inventory active APT source %s: %w", item.Path, readErr)
			}
			detail := "unreadable"
			switch {
			case errors.Is(readErr, errSourceFileSymlink):
				detail = "symlink"
			case errors.Is(readErr, errSourceFileNonRegular):
				detail = "non-regular"
			case errors.Is(readErr, errSourceFileTooLarge):
				detail = "too-large"
			}
			discovery.Definitions = append(discovery.Definitions, SourceDefinition{
				Path: item.Path, Status: SourceStatusIgnored,
				Classification: SourceClassUnsafe, Detail: detail,
			})
			discovery.InactiveInventory = append(discovery.InactiveInventory, item.Path)
			continue
		}
		total += int64(len(content))
		if total > maximumSourceInventoryBytes {
			return sourceDiscovery{}, fmt.Errorf("APT source inventory exceeds %d bytes", maximumSourceInventoryBytes)
		}
		document := sourceDocument{sourceDocumentPath: item, Content: content}
		definitions := parseSourceDocument(document)
		discovery.Definitions = append(discovery.Definitions, definitions...)
		if len(discovery.Definitions) > maximumSourceDefinitions {
			return sourceDiscovery{}, fmt.Errorf("APT source inventory contains more than %d definitions", maximumSourceDefinitions)
		}
		for _, definition := range definitions {
			if definition.Status == SourceStatusCommented || definition.Status == SourceStatusDisabled ||
				definition.Status == SourceStatusBackup || definition.Status == SourceStatusIgnored {
				discovery.InactiveInventory = append(discovery.InactiveInventory, item.Path)
				break
			}
		}
	}
	discovery.Candidates = classifySourceDefinitions(config, discovery.Definitions)
	if len(discovery.Candidates) > maximumDiscoveredCandidates {
		return sourceDiscovery{}, fmt.Errorf("APT source inventory contains %d unconfigured archive candidates; limit is %d", len(discovery.Candidates), maximumDiscoveredCandidates)
	}
	sort.Slice(discovery.Definitions, func(left, right int) bool {
		a, b := discovery.Definitions[left], discovery.Definitions[right]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.URI < b.URI
	})
	sort.Strings(discovery.InactiveInventory)
	discovery.InactiveInventory = uniqueStrings(discovery.InactiveInventory)
	return discovery, nil
}

func collectSourceDocumentPaths(config Config) ([]sourceDocumentPath, error) {
	aptRoot := filepath.Clean(config.Paths.APTRoot)
	seen := make(map[string]sourceDocumentKind)
	add := func(path string, kind sourceDocumentKind) error {
		path = filepath.Clean(path)
		prior, exists := seen[path]
		if !exists && len(seen) >= maximumSourceInventoryFiles {
			return errSourceInventoryLimit
		}
		if !exists || kind < prior {
			seen[path] = kind
		}
		return nil
	}
	if err := add(filepath.Join(aptRoot, "sources.list"), sourceDocumentActive); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(aptRoot)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inventory APT root: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, "sources.list.") {
			if err := add(filepath.Join(aptRoot, entry.Name()), sourceDocumentBackup); err != nil {
				return nil, err
			}
		}
	}
	sourceDirectory := filepath.Join(aptRoot, "sources.list.d")
	if info, statErr := os.Lstat(sourceDirectory); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("inventory APT sources.list.d: %w", errSourceFileSymlink)
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("inventory APT sources.list.d: %w", statErr)
	}
	err = filepath.WalkDir(sourceDirectory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) && filepath.Clean(path) == filepath.Clean(sourceDirectory) {
				return nil
			}
			return walkErr
		}
		if filepath.Clean(path) == filepath.Clean(sourceDirectory) {
			return nil
		}
		relative, relativeErr := filepath.Rel(sourceDirectory, path)
		if relativeErr != nil || strings.HasPrefix(relative, "..") {
			return fmt.Errorf("inventory APT source path %s", path)
		}
		depth := strings.Count(filepath.ToSlash(relative), "/")
		if entry.IsDir() {
			if depth >= maximumSourceInventoryDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceLikeName(entry.Name()) {
			return nil
		}
		kind := sourceDocumentBackup
		if depth == 0 && (strings.HasSuffix(strings.ToLower(entry.Name()), ".list") ||
			strings.HasSuffix(strings.ToLower(entry.Name()), ".sources")) {
			kind = sourceDocumentActive
		}
		return add(path, kind)
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inventory APT sources.list.d: %w", err)
	}
	mirrorsDirectory := filepath.Join(aptRoot, "mirrors")
	if info, statErr := os.Lstat(mirrorsDirectory); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("inventory APT mirror lists: %w", errSourceFileSymlink)
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("inventory APT mirror lists: %w", statErr)
	}
	mirrorEntries, err := os.ReadDir(mirrorsDirectory)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inventory APT mirror lists: %w", err)
	}
	for _, entry := range mirrorEntries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".list") {
			if err := add(filepath.Join(mirrorsDirectory, entry.Name()), sourceDocumentMirrorList); err != nil {
				return nil, err
			}
		}
	}
	result := make([]sourceDocumentPath, 0, len(seen))
	for path, kind := range seen {
		result = append(result, sourceDocumentPath{Path: path, Kind: kind})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Path < result[right].Path })
	return result, nil
}

func sourceLikeName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, ".list") || strings.Contains(lower, ".sources")
}

func parseSourceDocument(document sourceDocument) []SourceDefinition {
	if document.Kind == sourceDocumentMirrorList {
		return parseLegacyMirrorList(document)
	}
	if strings.Contains(strings.ToLower(filepath.Base(document.Path)), ".sources") {
		return parseDeb822Definitions(document)
	}
	return parseOneLineDefinitions(document)
}

func parseOneLineDefinitions(document sourceDocument) []SourceDefinition {
	var result []SourceDefinition
	for index, line := range strings.Split(strings.ReplaceAll(string(document.Content), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		commented := strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";")
		if commented {
			trimmed = strings.TrimSpace(strings.TrimLeft(trimmed, "#;"))
		}
		status := SourceStatusActive
		if document.Kind == sourceDocumentBackup {
			status = SourceStatusBackup
		} else if commented {
			status = SourceStatusCommented
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 3 || (fields[0] != "deb" && fields[0] != "deb-src") {
			continue
		}
		location, suites := sourceURIAndSuites(fields[1:])
		if location == "" {
			continue
		}
		result = append(result, newSourceDefinition(document.Path, index+1, status, location, suites))
	}
	return result
}

type deb822Paragraph struct {
	Text string
	Line int
}

func parseDeb822Definitions(document sourceDocument) []SourceDefinition {
	var result []SourceDefinition
	for _, paragraph := range splitDeb822Definitions(string(document.Content)) {
		text := paragraph.Text
		fields := deb822Fields(text)
		commented := false
		if !deb822HasSourceType(fields) || strings.TrimSpace(fields["uris"]) == "" {
			text = uncommentDeb822(text)
			fields = deb822Fields(text)
			commented = true
		}
		if !deb822HasSourceType(fields) {
			continue
		}
		locations := strings.Fields(fields["uris"])
		if len(locations) == 0 {
			continue
		}
		status := SourceStatusActive
		switch {
		case document.Kind == sourceDocumentBackup:
			status = SourceStatusBackup
		case commented:
			status = SourceStatusCommented
		case strings.EqualFold(strings.TrimSpace(fields["enabled"]), "no") ||
			strings.EqualFold(strings.TrimSpace(fields["enabled"]), "false"):
			status = SourceStatusDisabled
		}
		suites := strings.Fields(fields["suites"])
		for _, location := range locations {
			result = append(result, newSourceDefinition(document.Path, paragraph.Line, status, location, suites))
		}
	}
	return result
}

func splitDeb822Definitions(content string) []deb822Paragraph {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	var result []deb822Paragraph
	start := 0
	for index := 0; index <= len(lines); index++ {
		if index != len(lines) && strings.TrimSpace(lines[index]) != "" {
			continue
		}
		result = appendDeb822DefinitionParagraphs(result, lines, start, index)
		start = index + 1
	}
	return result
}

func appendDeb822DefinitionParagraphs(result []deb822Paragraph, lines []string, start, end int) []deb822Paragraph {
	if end <= start {
		return result
	}
	fullyCommented := true
	for _, line := range lines[start:end] {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			fullyCommented = false
			break
		}
	}
	if !fullyCommented {
		return append(result, deb822Paragraph{Text: strings.Join(lines[start:end], "\n"), Line: start + 1})
	}
	paragraphStart := start
	for index := start; index < end; index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed != "" && strings.Trim(trimmed, "#") == "" {
			if index > paragraphStart {
				result = append(result, deb822Paragraph{Text: strings.Join(lines[paragraphStart:index], "\n"), Line: paragraphStart + 1})
			}
			paragraphStart = index + 1
		}
	}
	if end > paragraphStart {
		result = append(result, deb822Paragraph{Text: strings.Join(lines[paragraphStart:end], "\n"), Line: paragraphStart + 1})
	}
	return result
}

func uncommentDeb822(paragraph string) string {
	lines := strings.Split(paragraph, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		marker := len(line) - len(strings.TrimLeft(line, " \t"))
		remaining := line[marker+1:]
		if strings.HasPrefix(remaining, " ") || strings.HasPrefix(remaining, "\t") {
			remaining = remaining[1:]
		}
		lines[index] = line[:marker] + remaining
	}
	return strings.Join(lines, "\n")
}

func deb822HasSourceType(fields map[string]string) bool {
	for _, sourceType := range strings.Fields(strings.ToLower(fields["types"])) {
		if sourceType == "deb" || sourceType == "deb-src" {
			return true
		}
	}
	return false
}

func parseLegacyMirrorList(document sourceDocument) []SourceDefinition {
	var result []SourceDefinition
	for index, line := range strings.Split(strings.ReplaceAll(string(document.Content), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		var suites []string
		for _, field := range fields[1:] {
			if suite, found := strings.CutPrefix(field, "suite:"); found && suite != "" {
				suites = append(suites, suite)
			}
		}
		result = append(result, newSourceDefinition(document.Path, index+1, SourceStatusMirrorList, fields[0], suites))
	}
	return result
}

func newSourceDefinition(path string, line int, status SourceDefinitionStatus, rawURI string, suites []string) SourceDefinition {
	definition := SourceDefinition{Path: filepath.Clean(path), Line: line, Status: status, Suites: append([]string(nil), suites...)}
	if strings.HasPrefix(rawURI, "mirror+file:") {
		definition.URI = rawURI
		definition.Classification = SourceClassManagedTopology
		return definition
	}
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		strings.ContainsAny(rawURI, "\r\n\x00") {
		definition.Classification = SourceClassUnsafe
		definition.Detail = "invalid-or-credential-bearing-uri"
		return definition
	}
	definition.URI = parsed.String()
	definition.Classification = SourceClassThirdParty
	return definition
}

func classifySourceDefinitions(config Config, definitions []SourceDefinition) []Candidate {
	type proposal struct {
		candidate Candidate
		identity  string
	}
	configured := make(map[string]Candidate)
	for _, candidate := range config.Candidates {
		_, identity, ok := normalizeArchiveRoot(candidate.URI)
		if ok {
			configured[identity] = candidate
		}
	}
	proposals := make(map[string]proposal)
	for index := range definitions {
		definition := &definitions[index]
		if definition.Classification == SourceClassManagedTopology || definition.Classification == SourceClassUnsafe {
			continue
		}
		normalized, identity, ok := normalizeArchiveRoot(definition.URI)
		if !ok || !looksLikeUbuntuArchiveRoot(normalized, configured[identity]) {
			definition.Classification = SourceClassThirdParty
			continue
		}
		if existing, found := configured[identity]; found && !preferArchiveURI(normalized, existing.URI) {
			definition.Classification = SourceClassConfigured
			definition.CandidateID = existing.ID
			continue
		}
		candidate := candidateForDiscoveredRoot(normalized, identity)
		if current, found := proposals[identity]; !found || preferArchiveURI(candidate.URI, current.candidate.URI) {
			proposals[identity] = proposal{candidate: candidate, identity: identity}
		}
	}
	identities := make([]string, 0, len(proposals))
	for identity := range proposals {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	result := make([]Candidate, 0, len(identities))
	for _, identity := range identities {
		result = append(result, proposals[identity].candidate)
	}
	for index := range definitions {
		definition := &definitions[index]
		if definition.Classification != SourceClassThirdParty {
			continue
		}
		_, identity, ok := normalizeArchiveRoot(definition.URI)
		if !ok {
			continue
		}
		if proposed, found := proposals[identity]; found {
			definition.Classification = SourceClassUnverified
			definition.CandidateID = proposed.candidate.ID
		}
	}
	return result
}

func normalizeArchiveRoot(raw string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if port != "" {
		host += ":" + port
	}
	path := filepath.ToSlash(filepath.Clean(parsed.Path))
	if path == "." || path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/") + "/"
	parsed.Host = host
	parsed.Path = path
	parsed.RawPath = ""
	identity := host + "\x00" + path
	return parsed.String(), identity, true
}

func looksLikeUbuntuArchiveRoot(normalized string, configured Candidate) bool {
	if configured.ID != "" {
		return true
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return false
	}
	path := strings.TrimSuffix(strings.ToLower(parsed.Path), "/")
	return strings.HasSuffix(path, "/ubuntu") || strings.HasSuffix(path, "/ubuntu-ports")
}

func preferArchiveURI(candidate, current string) bool {
	left, leftErr := url.Parse(candidate)
	right, rightErr := url.Parse(current)
	return leftErr == nil && rightErr == nil && left.Scheme == "https" && right.Scheme == "http"
}

func candidateForDiscoveredRoot(uri, identity string) Candidate {
	parsed, _ := url.Parse(uri)
	role, priority := discoveredCandidateRole(parsed.Hostname())
	digest := sha256.Sum256([]byte(identity))
	host := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(parsed.Hostname()), "-")
	host = strings.Trim(host, "-")
	if len(host) > 30 {
		host = host[:30]
	}
	if host == "" {
		host = "archive"
	}
	return Candidate{
		ID:   "discovered-" + host + "-" + hex.EncodeToString(digest[:6]),
		Role: role, Priority: priority, URI: uri,
		BypassProxy: role == RoleDomestic && domesticHostname(parsed.Hostname()),
	}
}

func discoveredCandidateRole(hostname string) (CandidateRole, int) {
	host := strings.ToLower(hostname)
	switch {
	case host == "security.ubuntu.com":
		return RoleOfficialSecurity, 900
	case host == "archive.ubuntu.com" || host == "ports.ubuntu.com":
		return RoleOfficialBoth, 900
	case strings.HasSuffix(host, ".archive.ubuntu.com"):
		return RoleOfficialMain, 850
	default:
		return RoleDomestic, 0
	}
}

func domesticHostname(hostname string) bool {
	host := strings.ToLower(hostname)
	return strings.HasSuffix(host, ".ir") || strings.HasPrefix(host, "ir.") || strings.Contains(host, ".ir.")
}

type cachedProber struct {
	delegate Prober
	mu       sync.Mutex
	results  map[string]ProbeResult
}

func newCachedProber(delegate Prober) *cachedProber {
	return &cachedProber{delegate: delegate, results: make(map[string]ProbeResult)}
}

func (prober *cachedProber) Probe(ctx context.Context, config Config, candidate Candidate, suite string) ProbeResult {
	key := candidate.URI + "\x00" + suite
	prober.mu.Lock()
	result, ok := prober.results[key]
	prober.mu.Unlock()
	if ok {
		return result
	}
	result = prober.delegate.Probe(ctx, config, candidate, suite)
	prober.mu.Lock()
	if prior, exists := prober.results[key]; exists {
		result = prior
	} else {
		prober.results[key] = result
	}
	prober.mu.Unlock()
	return result
}

type candidateAssessment struct {
	Candidate Candidate
	Reports   []CandidateReport
	Verified  bool
	FinalID   string
}

func assessDiscoveredCandidates(candidates []Candidate, report RefreshReport) []candidateAssessment {
	byID := make(map[string][]CandidateReport)
	for _, candidateReport := range report.Candidates {
		byID[candidateReport.CandidateID] = append(byID[candidateReport.CandidateID], candidateReport)
	}
	result := make([]candidateAssessment, 0, len(candidates))
	for _, candidate := range candidates {
		assessment := candidateAssessment{Candidate: candidate, Reports: byID[candidate.ID]}
		for _, candidateReport := range assessment.Reports {
			if candidateReport.Status == ProbeVerified {
				assessment.Verified = true
				break
			}
		}
		result = append(result, assessment)
	}
	return result
}

func mergeDiscoveredCandidates(config Config, assessments []candidateAssessment) (Config, []Candidate, map[string]string) {
	merged := config
	merged.Candidates = append([]Candidate(nil), config.Candidates...)
	identityIndex := make(map[string]int)
	for index, candidate := range merged.Candidates {
		_, identity, ok := normalizeArchiveRoot(candidate.URI)
		if ok {
			identityIndex[identity] = index
		}
	}
	var accepted []Candidate
	finalIDs := make(map[string]string)
	for index := range assessments {
		assessment := &assessments[index]
		if !assessment.Verified {
			continue
		}
		_, identity, _ := normalizeArchiveRoot(assessment.Candidate.URI)
		if existingIndex, found := identityIndex[identity]; found {
			existing := &merged.Candidates[existingIndex]
			if preferArchiveURI(assessment.Candidate.URI, existing.URI) {
				existing.URI = assessment.Candidate.URI
			}
			assessment.FinalID = existing.ID
			finalIDs[assessment.Candidate.ID] = existing.ID
			accepted = append(accepted, *existing)
			continue
		}
		assessment.FinalID = assessment.Candidate.ID
		finalIDs[assessment.Candidate.ID] = assessment.Candidate.ID
		identityIndex[identity] = len(merged.Candidates)
		merged.Candidates = append(merged.Candidates, assessment.Candidate)
		accepted = append(accepted, assessment.Candidate)
	}
	return merged, accepted, finalIDs
}

func annotateSourceDefinitions(
	definitions []SourceDefinition,
	assessments []candidateAssessment,
	finalReport RefreshReport,
) []SourceDefinition {
	result := append([]SourceDefinition(nil), definitions...)
	assessmentByIdentity := make(map[string]candidateAssessment)
	for _, assessment := range assessments {
		_, identity, ok := normalizeArchiveRoot(assessment.Candidate.URI)
		if ok {
			assessmentByIdentity[identity] = assessment
		}
	}
	finalReports := make(map[string][]CandidateReport)
	for _, report := range finalReport.Candidates {
		finalReports[report.CandidateID] = append(finalReports[report.CandidateID], report)
	}
	for index := range result {
		definition := &result[index]
		_, identity, ok := normalizeArchiveRoot(definition.URI)
		if !ok {
			continue
		}
		if assessment, found := assessmentByIdentity[identity]; found {
			definition.Verification = append([]CandidateReport(nil), assessment.Reports...)
			if assessment.Verified {
				definition.Classification = SourceClassVerified
				definition.CandidateID = assessment.FinalID
				if reports := finalReports[assessment.FinalID]; len(reports) != 0 {
					definition.Verification = append([]CandidateReport(nil), reports...)
				}
			} else {
				allRejected := len(assessment.Reports) != 0
				for _, report := range assessment.Reports {
					if report.Status == ProbeTransient {
						allRejected = false
					}
				}
				if allRejected {
					definition.Classification = SourceClassThirdParty
				} else {
					definition.Classification = SourceClassUnverified
				}
			}
			continue
		}
		if definition.CandidateID != "" {
			definition.Verification = append([]CandidateReport(nil), finalReports[definition.CandidateID]...)
		}
	}
	return result
}
