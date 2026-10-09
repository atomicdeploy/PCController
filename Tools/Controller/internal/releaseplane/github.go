package releaseplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/artifacts"
)

type githubWorkflowRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	HeadBranch string    `json:"head_branch"`
	HeadSHA    string    `json:"head_sha"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
}

type githubWorkflowRuns struct {
	WorkflowRuns []githubWorkflowRun `json:"workflow_runs"`
}

type githubArtifact struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	SizeInBytes        int64     `json:"size_in_bytes"`
	ArchiveDownloadURL string    `json:"archive_download_url"`
	Digest             string    `json:"digest"`
	Expired            bool      `json:"expired"`
	CreatedAt          time.Time `json:"created_at"`
}

type githubArtifacts struct {
	Artifacts []githubArtifact `json:"artifacts"`
}

type githubRelease struct {
	ID              int64                `json:"id"`
	TagName         string               `json:"tag_name"`
	Name            string               `json:"name"`
	TargetCommitish string               `json:"target_commitish"`
	Draft           bool                 `json:"draft"`
	Prerelease      bool                 `json:"prerelease"`
	PublishedAt     time.Time            `json:"published_at"`
	Assets          []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	State              string    `json:"state"`
	Size               int64     `json:"size"`
	Digest             string    `json:"digest"`
	BrowserDownloadURL string    `json:"browser_download_url"`
	CreatedAt          time.Time `json:"created_at"`
}

func (client *Client) DiscoverWorkflow(ctx context.Context, request GitHubWorkflowRequest) (DiscoveryResult, error) {
	if err := validateKind(request.Kind); err != nil {
		return DiscoveryResult{}, err
	}
	owner, repository, err := parseRepository(request.Repository)
	if err != nil {
		return DiscoveryResult{}, err
	}
	base, err := githubAPIBase(request.APIBaseURL)
	if err != nil {
		return DiscoveryResult{}, err
	}
	query := url.Values{"status": {"success"}, "per_page": {"100"}}
	if branch := strings.TrimSpace(request.Branch); branch != "" {
		query.Set("branch", branch)
	}
	runsURL := fmt.Sprintf("%s/repos/%s/%s/actions/runs?%s", base, owner, repository, query.Encode())
	response, err := client.get(ctx, runsURL, request.BearerToken, "application/vnd.github+json")
	if err != nil {
		return DiscoveryResult{}, err
	}
	var runs githubWorkflowRuns
	if err := decodeJSONResponse(response, &runs); err != nil {
		return DiscoveryResult{}, err
	}
	workflow := strings.TrimSpace(request.Workflow)
	sort.SliceStable(runs.WorkflowRuns, func(left, right int) bool {
		return runs.WorkflowRuns[left].CreatedAt.After(runs.WorkflowRuns[right].CreatedAt)
	})
	var selected *githubWorkflowRun
	for index := range runs.WorkflowRuns {
		run := &runs.WorkflowRuns[index]
		if !strings.EqualFold(run.Conclusion, "success") {
			continue
		}
		if workflow != "" && !strings.EqualFold(run.Name, workflow) {
			continue
		}
		selected = run
		break
	}
	if selected == nil {
		return DiscoveryResult{}, fmt.Errorf("no successful workflow run matched repository %s", request.Repository)
	}
	artifactsURL := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/artifacts?per_page=100", base, owner, repository, selected.ID)
	response, err = client.get(ctx, artifactsURL, request.BearerToken, "application/vnd.github+json")
	if err != nil {
		return DiscoveryResult{}, err
	}
	var listing githubArtifacts
	if err := decodeJSONResponse(response, &listing); err != nil {
		return DiscoveryResult{}, err
	}
	platform := requestedPlatform(request.Platform, request.Kind)
	candidates := make([]Candidate, 0, len(listing.Artifacts))
	for _, item := range listing.Artifacts {
		if item.Expired || item.ArchiveDownloadURL == "" || !platformMatches(item.Name, platform, request.Kind) {
			continue
		}
		digest := optionalDigest(item.Digest)
		candidate := Candidate{
			Source: "github-workflow", Repository: owner + "/" + repository,
			WorkflowRunID: selected.ID, Kind: request.Kind, Name: item.Name + ".zip",
			URL: item.ArchiveDownloadURL, Platform: candidatePlatform(item.Name, platform, request.Kind),
			Archive: true, ArchiveBytes: item.SizeInBytes, ArchiveSHA256: digest,
			BuildHash:       firstValue(request.BuildHash, selected.HeadSHA),
			BuildTimestamp:  firstValue(request.BuildTimestamp, selected.CreatedAt.UTC().Format(time.RFC3339)),
			PackedTimestamp: request.PackedTimestamp, CreatedAt: item.CreatedAt,
			Metadata: map[string]string{
				"workflow": selected.Name, "branch": selected.HeadBranch,
				"run_id": strconv.FormatInt(selected.ID, 10), "artifact_id": strconv.FormatInt(item.ID, 10),
			},
		}
		candidate.ID = candidateID(candidate)
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return DiscoveryResult{}, fmt.Errorf("successful workflow run %d has no non-expired matching artifacts", selected.ID)
	}
	return DiscoveryResult{Source: "github-workflow", CheckedAt: time.Now().UTC(), Candidates: candidates}, nil
}

func (client *Client) DiscoverRelease(ctx context.Context, request GitHubReleaseRequest) (DiscoveryResult, error) {
	if err := validateKind(request.Kind); err != nil {
		return DiscoveryResult{}, err
	}
	owner, repository, err := parseRepository(request.Repository)
	if err != nil {
		return DiscoveryResult{}, err
	}
	base, err := githubAPIBase(request.APIBaseURL)
	if err != nil {
		return DiscoveryResult{}, err
	}
	var release githubRelease
	if tag := strings.TrimSpace(request.Tag); tag != "" {
		releaseURL := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", base, owner, repository, url.PathEscape(tag))
		response, getErr := client.get(ctx, releaseURL, request.BearerToken, "application/vnd.github+json")
		if getErr != nil {
			return DiscoveryResult{}, getErr
		}
		if err := decodeJSONResponse(response, &release); err != nil {
			return DiscoveryResult{}, err
		}
	} else if request.IncludePrerelease {
		releaseURL := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=30", base, owner, repository)
		response, getErr := client.get(ctx, releaseURL, request.BearerToken, "application/vnd.github+json")
		if getErr != nil {
			return DiscoveryResult{}, getErr
		}
		var releases []githubRelease
		if err := decodeJSONResponse(response, &releases); err != nil {
			return DiscoveryResult{}, err
		}
		for _, value := range releases {
			if !value.Draft {
				release = value
				break
			}
		}
	} else {
		releaseURL := fmt.Sprintf("%s/repos/%s/%s/releases/latest", base, owner, repository)
		response, getErr := client.get(ctx, releaseURL, request.BearerToken, "application/vnd.github+json")
		if getErr != nil {
			return DiscoveryResult{}, getErr
		}
		if err := decodeJSONResponse(response, &release); err != nil {
			return DiscoveryResult{}, err
		}
	}
	if release.ID == 0 || release.Draft || (!request.IncludePrerelease && release.Prerelease) {
		return DiscoveryResult{}, errors.New("no published GitHub release matched the request")
	}
	checksums := client.releaseChecksums(ctx, release.Assets, request.BearerToken)
	platform := requestedPlatform(request.Platform, request.Kind)
	candidates := make([]Candidate, 0, len(release.Assets))
	for _, item := range release.Assets {
		if item.State != "uploaded" || item.BrowserDownloadURL == "" || isChecksumAsset(item.Name) ||
			!platformMatches(item.Name, platform, request.Kind) {
			continue
		}
		digest := optionalDigest(item.Digest)
		if digest == "" {
			digest = checksums[item.Name]
		}
		archive := strings.EqualFold(path.Ext(item.Name), ".zip")
		candidate := Candidate{
			Source: "github-release", Repository: owner + "/" + repository,
			ReleaseTag: release.TagName, Kind: request.Kind, Name: item.Name,
			URL: item.BrowserDownloadURL, Platform: candidatePlatform(item.Name, platform, request.Kind),
			BuildHash:       releaseCommit(release.TargetCommitish),
			BuildTimestamp:  release.PublishedAt.UTC().Format(time.RFC3339),
			PackedTimestamp: request.PackedTimestamp, CreatedAt: item.CreatedAt,
			Metadata: map[string]string{
				"release_id": strconv.FormatInt(release.ID, 10), "release_name": release.Name,
				"asset_id": strconv.FormatInt(item.ID, 10),
			},
		}
		if archive {
			candidate.Archive = true
			candidate.ArchiveBytes = item.Size
			candidate.ArchiveSHA256 = digest
		} else {
			candidate.Bytes = item.Size
			candidate.SHA256 = digest
		}
		candidate.ID = candidateID(candidate)
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return DiscoveryResult{}, fmt.Errorf("release %s has no matching uploaded assets", release.TagName)
	}
	return DiscoveryResult{Source: "github-release", CheckedAt: time.Now().UTC(), Candidates: candidates}, nil
}

func (client *Client) releaseChecksums(ctx context.Context, assets []githubReleaseAsset, token string) map[string]string {
	result := make(map[string]string)
	for _, asset := range assets {
		if !isChecksumAsset(asset.Name) || asset.Size <= 0 || asset.Size > 1<<20 {
			continue
		}
		response, err := client.get(ctx, asset.BrowserDownloadURL, token, "text/plain")
		if err != nil {
			return result
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if err != nil {
			return result
		}
		for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			digest, digestErr := normalizeDigest(fields[0])
			if digestErr != nil {
				continue
			}
			name := strings.TrimPrefix(fields[len(fields)-1], "*")
			if path.Base(strings.ReplaceAll(name, "\\", "/")) == name {
				result[name] = digest
			}
		}
		return result
	}
	return result
}

func parseRepository(value string) (string, string, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(value), "/"), "/")
	if len(parts) != 2 || !safeRepositoryPart(parts[0]) || !safeRepositoryPart(parts[1]) {
		return "", "", fmt.Errorf("repository must be owner/name using letters, digits, dot, underscore, or dash")
	}
	return parts[0], parts[1], nil
}

func safeRepositoryPart(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}

func candidateID(candidate Candidate) string {
	value := sha256.Sum256([]byte(candidate.Source + "\x00" + candidate.Repository + "\x00" + candidate.URL + "\x00" + candidate.Name))
	return hex.EncodeToString(value[:8])
}

func optionalDigest(value string) string {
	digest, err := normalizeDigest(value)
	if err != nil {
		return ""
	}
	return digest
}

func isChecksumAsset(name string) bool {
	name = strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	return name == "sha256sums" || name == "sha256sums.txt" || strings.HasSuffix(name, ".sha256") || strings.HasSuffix(name, ".sha256sums")
}

func requestedPlatform(value string, kind artifacts.Kind) string {
	if strings.TrimSpace(value) == "" && kind != artifacts.KindHostExecutable {
		return ""
	}
	return normalizedPlatform(value)
}

func candidatePlatform(name, requested string, kind artifacts.Kind) string {
	if detected := inferPlatform(name); detected != "" {
		return detected
	}
	if kind == artifacts.KindHostExecutable {
		return requested
	}
	return strings.TrimSpace(requested)
}

func platformMatches(name, requested string, kind artifacts.Kind) bool {
	if requested == "" {
		return true
	}
	detected := inferPlatform(name)
	return detected == "" || detected == requested || kind != artifacts.KindHostExecutable
}

func inferPlatform(name string) string {
	value := strings.ToLower(strings.NewReplacer("_", "-", "/", "-").Replace(name))
	operatingSystems := []string{"windows", "linux", "darwin", "macos", "freebsd"}
	architectures := []string{"amd64", "x86-64", "x64", "arm64", "aarch64", "386", "x86"}
	for _, operatingSystem := range operatingSystems {
		if !strings.Contains(value, operatingSystem) {
			continue
		}
		if operatingSystem == "macos" {
			operatingSystem = "darwin"
		}
		for _, architecture := range architectures {
			if strings.Contains(value, architecture) {
				switch architecture {
				case "x86-64", "x64":
					architecture = "amd64"
				case "aarch64":
					architecture = "arm64"
				case "x86":
					architecture = "386"
				}
				return operatingSystem + "/" + architecture
			}
		}
	}
	return ""
}

func releaseCommit(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 7 || len(value) > 64 {
		return ""
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return ""
		}
	}
	return value
}

func firstValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
