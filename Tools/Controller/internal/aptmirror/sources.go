package aptmirror

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type sourcePlan struct {
	Edits                map[string][]byte
	ActiveUbuntu         []string
	ExistingTopology     []string
	InactiveInventory    []string
	SourceInventory      []SourceDefinition
	DiscoveredCandidates []Candidate
}

func planUbuntuSources(config Config) (sourcePlan, error) {
	plan := sourcePlan{Edits: make(map[string][]byte)}
	discovery, err := discoverSourceDefinitions(config)
	if err != nil {
		return plan, err
	}
	plan.InactiveInventory = discovery.InactiveInventory
	plan.SourceInventory = discovery.Definitions
	plan.DiscoveredCandidates = discovery.Candidates
	activeFiles := []string{filepath.Join(config.Paths.APTRoot, "sources.list")}
	for _, pattern := range []string{
		filepath.Join(config.Paths.APTRoot, "sources.list.d", "*.list"),
		filepath.Join(config.Paths.APTRoot, "sources.list.d", "*.sources"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return plan, err
		}
		activeFiles = append(activeFiles, matches...)
	}
	seen := make(map[string]bool)
	for _, path := range activeFiles {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		content, err := readBoundedSourceFile(path, maximumSourceDefinitionBytes)
		if errors.Is(err, errSourceFileMissing) {
			continue
		}
		if err != nil {
			return plan, fmt.Errorf("inventory APT source %s: %w", path, err)
		}
		var edited []byte
		var active, topology bool
		if strings.HasSuffix(path, ".sources") {
			edited, active, topology, err = reconcileDeb822Source(path, content, config)
		} else {
			edited, active, topology, err = reconcileListSource(path, content, config)
		}
		if err != nil {
			return plan, err
		}
		if active {
			plan.ActiveUbuntu = append(plan.ActiveUbuntu, path)
		}
		if topology {
			plan.ExistingTopology = append(plan.ExistingTopology, path)
		}
		if path != filepath.Clean(config.Paths.CanonicalSource) && !bytesEqual(content, edited) {
			plan.Edits[path] = edited
		}
	}
	sort.Strings(plan.ActiveUbuntu)
	sort.Strings(plan.ExistingTopology)
	return plan, nil
}

func reconcileListSource(path string, content []byte, config Config) ([]byte, bool, bool, error) {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	active, topology, changed := false, false, false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") || trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || (fields[0] != "deb" && fields[0] != "deb-src") {
			continue
		}
		location, suites := sourceURIAndSuites(fields[1:])
		if strings.HasPrefix(location, "mirror+file:") {
			if location != "mirror+file:"+config.Paths.MirrorList {
				return nil, false, false, fmt.Errorf("refusing ambiguous APT mirror topology in %s: %s", path, location)
			}
			if !hasOnlyConfiguredSuites(suites, config) {
				return nil, false, false, fmt.Errorf("active PCController mirror source %s has unsupported suites", path)
			}
			topology = true
			active = true
			lines[index] = "# Disabled by PCController after adopting canonical mirror+file topology: " + line
			changed = true
			continue
		}
		if isUbuntuArchiveURI(location, config) {
			if !hasOnlyConfiguredSuites(suites, config) {
				return nil, false, false, fmt.Errorf("active Ubuntu source %s has suites outside the selected release", path)
			}
			active = true
			lines[index] = "# Disabled by PCController after adopting domestic-first topology: " + line
			changed = true
		}
	}
	if !changed {
		return append([]byte(nil), content...), active, topology, nil
	}
	return []byte(strings.Join(lines, "\n")), active, topology, nil
}

func reconcileDeb822Source(path string, content []byte, config Config) ([]byte, bool, bool, error) {
	paragraphs, separators := splitDeb822Preserving(string(content))
	active, topology := false, false
	changed := false
	for index, paragraph := range paragraphs {
		fields := deb822Fields(paragraph)
		locations := strings.Fields(fields["uris"])
		if len(locations) == 0 {
			continue
		}
		disabled := strings.EqualFold(strings.TrimSpace(fields["enabled"]), "no")
		suiteMatch := hasOnlyConfiguredSuites(strings.Fields(fields["suites"]), config)
		paragraphUbuntu, paragraphTopology := false, false
		unknownURI := false
		for _, location := range locations {
			if strings.HasPrefix(location, "mirror+file:") {
				if location != "mirror+file:"+config.Paths.MirrorList {
					return nil, false, false, fmt.Errorf("refusing ambiguous APT mirror topology in %s: %s", path, location)
				}
				paragraphTopology = true
				continue
			}
			if isUbuntuArchiveURI(location, config) {
				paragraphUbuntu = true
			} else {
				unknownURI = true
			}
		}
		if filepath.Clean(path) == filepath.Clean(config.Paths.CanonicalSource) && unknownURI {
			return nil, false, false, fmt.Errorf("refusing to overwrite third-party stanza in canonical Ubuntu source %s", path)
		}
		if disabled {
			continue
		}
		if (paragraphUbuntu || paragraphTopology) && unknownURI {
			return nil, false, false, fmt.Errorf("refusing mixed Ubuntu/third-party URIs in APT stanza %s", path)
		}
		if !suiteMatch {
			if paragraphUbuntu || paragraphTopology {
				return nil, false, false, fmt.Errorf("active Ubuntu stanza in %s has suites outside the selected release", path)
			}
			continue
		}
		if paragraphTopology {
			topology = true
			active = true
		}
		if paragraphUbuntu {
			active = true
		}
		if (paragraphUbuntu || paragraphTopology) && filepath.Clean(path) != filepath.Clean(config.Paths.CanonicalSource) {
			paragraphs[index] = disableDeb822Paragraph(paragraph)
			changed = true
		}
	}
	if !changed {
		return append([]byte(nil), content...), active, topology, nil
	}
	var output strings.Builder
	for index, paragraph := range paragraphs {
		output.WriteString(paragraph)
		if index < len(separators) {
			output.WriteString(separators[index])
		}
	}
	return []byte(output.String()), active, topology, nil
}

func isUbuntuArchiveURI(raw string, config Config) bool {
	if raw == "" {
		return false
	}
	normalized := strings.TrimSuffix(raw, "/")
	for _, candidate := range config.Candidates {
		if normalized == strings.TrimSuffix(candidate.URI, "/") {
			return true
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.TrimSuffix(strings.ToLower(parsed.Path), "/")
	return (host == "archive.ubuntu.com" || host == "security.ubuntu.com" ||
		strings.HasSuffix(host, ".archive.ubuntu.com") || host == "ports.ubuntu.com") &&
		(path == "/ubuntu" || path == "/ubuntu-ports")
}

func splitDeb822Preserving(content string) ([]string, []string) {
	separatorExpression := regexp.MustCompile(`(?:\r?\n)[ \t]*(?:\r?\n)+`)
	matches := separatorExpression.FindAllStringIndex(content, -1)
	paragraphs := make([]string, 0, len(matches)+1)
	separators := make([]string, 0, len(matches))
	start := 0
	for _, match := range matches {
		paragraphs = append(paragraphs, content[start:match[0]])
		separators = append(separators, content[match[0]:match[1]])
		start = match[1]
	}
	paragraphs = append(paragraphs, content[start:])
	return paragraphs, separators
}

func deb822Fields(paragraph string) map[string]string {
	result := make(map[string]string)
	key := ""
	for _, line := range strings.Split(paragraph, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if key != "" {
				result[key] += " " + strings.TrimSpace(line)
			}
			continue
		}
		name, value, found := strings.Cut(line, ":")
		if !found {
			key = ""
			continue
		}
		key = strings.ToLower(strings.TrimSpace(name))
		result[key] = strings.TrimSpace(value)
	}
	return result
}

func disableDeb822Paragraph(paragraph string) string {
	lines := strings.Split(paragraph, "\n")
	found := false
	for index, line := range lines {
		name, _, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Enabled") {
			lines[index] = "Enabled: no"
			found = true
		}
	}
	if !found {
		lines = append(lines, "Enabled: no")
	}
	return strings.Join(lines, "\n")
}

func sourceURIAndSuites(fields []string) (string, []string) {
	for index, field := range fields {
		if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") ||
			strings.HasPrefix(field, "mirror+file:") {
			if index+1 < len(fields) {
				// A one-line source has exactly one suite token after the URI;
				// every later token is a component. Keeping components out of
				// suite validation prevents a selected-release-looking component
				// from making a foreign suite appear acceptable.
				return field, []string{fields[index+1]}
			}
			return field, nil
		}
	}
	return "", nil
}

func hasOnlyConfiguredSuites(values []string, config Config) bool {
	if len(values) == 0 {
		return false
	}
	configured := make(map[string]struct{}, len(config.Suites()))
	for _, suite := range config.Suites() {
		configured[suite] = struct{}{}
	}
	for _, value := range values {
		if _, ok := configured[value]; !ok {
			return false
		}
	}
	return true
}

func bytesEqual(left, right []byte) bool { return string(left) == string(right) }

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
