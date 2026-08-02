package releaseplane

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// LocalManifest publishes the primary host's deduplicated artifact inventory
// in the same product-neutral format consumed by DiscoverManifest. Download
// URLs stay relative to this authenticated host and never expose local paths.
func (service *Service) LocalManifest() (Manifest, error) {
	listing, err := service.artifacts.List(nil)
	if err != nil {
		return Manifest{}, err
	}
	result := Manifest{Format: ManifestFormat, GeneratedAt: time.Now().UTC()}
	result.Artifacts = make([]ManifestArtifact, 0, len(listing.Artifacts))
	for _, descriptor := range listing.Artifacts {
		result.Artifacts = append(result.Artifacts, ManifestArtifact{
			Kind: descriptor.Kind, Name: descriptor.Name, URL: descriptor.DownloadURL,
			Bytes: descriptor.Bytes, SHA256: descriptor.SHA256,
			BuildHash: descriptor.BuildHash, BuildTimestamp: descriptor.BuildTimestamp,
			PackedTimestamp: descriptor.PackedTimestamp, Platform: descriptor.Platform,
			CreatedAt: descriptor.CreatedAt, Metadata: cloneMetadata(descriptor.Metadata),
		})
	}
	return result, nil
}

func (client *Client) DiscoverManifest(ctx context.Context, request ManifestRequest) (DiscoveryResult, error) {
	manifestURL := strings.TrimSpace(request.URL)
	response, err := client.get(ctx, manifestURL, request.BearerToken, "application/json")
	if err != nil {
		return DiscoveryResult{}, err
	}
	if response.ContentLength > maxMetadataBytes {
		response.Body.Close()
		return DiscoveryResult{}, fmt.Errorf("update manifest exceeds %d bytes", maxMetadataBytes)
	}
	var manifest Manifest
	// The format identifier defines the semantic contract. Unknown additive
	// fields are ignored so a newer publisher can extend v1 without breaking an
	// older host that still understands every field it needs.
	if err := decodeJSONResponse(response, &manifest); err != nil {
		return DiscoveryResult{}, err
	}
	if manifest.Format != ManifestFormat {
		return DiscoveryResult{}, fmt.Errorf("unsupported update manifest format %q", manifest.Format)
	}
	base, err := url.Parse(manifestURL)
	if err != nil {
		return DiscoveryResult{}, err
	}
	result := DiscoveryResult{Source: "manifest", CheckedAt: time.Now().UTC()}
	for _, item := range manifest.Artifacts {
		resolved, resolveErr := base.Parse(strings.TrimSpace(item.URL))
		if resolveErr != nil || resolved == nil {
			return DiscoveryResult{}, fmt.Errorf("resolve manifest artifact %q URL", item.Name)
		}
		candidate := Candidate{
			Source: "manifest", Kind: item.Kind, Name: item.Name,
			ArtifactName: item.ArtifactName, URL: resolved.String(), Platform: strings.ToLower(strings.TrimSpace(item.Platform)),
			Bytes: item.Bytes, SHA256: item.SHA256, Archive: item.Archive,
			ArchiveBytes: item.ArchiveBytes, ArchiveSHA256: item.ArchiveSHA256, ArchivePath: item.ArchivePath,
			BuildHash: item.BuildHash, BuildTimestamp: item.BuildTimestamp,
			PackedTimestamp: item.PackedTimestamp, CreatedAt: item.CreatedAt,
			Metadata: cloneMetadata(item.Metadata),
		}
		if candidate.CreatedAt.IsZero() {
			candidate.CreatedAt = manifest.GeneratedAt
		}
		if err := validateCandidate(candidate); err != nil {
			return DiscoveryResult{}, fmt.Errorf("manifest artifact %q: %w", item.Name, err)
		}
		candidate.SHA256, err = optionalNormalizedDigest(candidate.SHA256)
		if err != nil {
			return DiscoveryResult{}, fmt.Errorf("manifest artifact %q: %w", item.Name, err)
		}
		candidate.ArchiveSHA256, err = optionalNormalizedDigest(candidate.ArchiveSHA256)
		if err != nil {
			return DiscoveryResult{}, fmt.Errorf("manifest artifact %q: %w", item.Name, err)
		}
		candidate.ID = candidateID(candidate)
		result.Candidates = append(result.Candidates, candidate)
	}
	if len(result.Candidates) == 0 {
		return DiscoveryResult{}, fmt.Errorf("update manifest contains no artifacts")
	}
	return result, nil
}

func CheckForUpdate(request CheckRequest) (CheckResult, error) {
	if err := validateKind(request.Kind); err != nil {
		return CheckResult{}, err
	}
	platform := requestedPlatform(request.Platform, request.Kind)
	candidates := make([]Candidate, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		if candidate.Kind != request.Kind || (platform != "" && candidate.Platform != "" && normalizedPlatform(candidate.Platform) != platform) {
			continue
		}
		if err := validateCandidate(candidate); err != nil {
			return CheckResult{}, err
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return CheckResult{Status: "unavailable", Reason: "no candidate matches the requested artifact kind and platform"}, nil
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		return candidateAfter(candidates[left], candidates[right])
	})
	selected := candidates[0]
	status, reason := compareIdentity(request.Current, selected)
	return CheckResult{Status: status, Candidate: &selected, Reason: reason}, nil
}

func compareIdentity(current Identity, candidate Candidate) (string, string) {
	if current.SHA256 != "" && candidate.SHA256 != "" && strings.EqualFold(strings.TrimPrefix(current.SHA256, "sha256:"), strings.TrimPrefix(candidate.SHA256, "sha256:")) {
		return "same", "content hashes match"
	}
	if current.BuildHash != "" && candidate.BuildHash != "" && strings.EqualFold(current.BuildHash, candidate.BuildHash) {
		return "same", "build hashes match"
	}
	if current.PackedTimestamp != 0 && candidate.PackedTimestamp != 0 {
		switch {
		case candidate.PackedTimestamp > current.PackedTimestamp:
			return "newer", "candidate packed timestamp is newer"
		case candidate.PackedTimestamp < current.PackedTimestamp:
			return "older", "candidate packed timestamp is older"
		default:
			return "different", "timestamps match but content/build hashes differ"
		}
	}
	if current.BuildTimestamp != "" && candidate.BuildTimestamp != "" {
		left, leftErr := time.Parse(time.RFC3339Nano, current.BuildTimestamp)
		right, rightErr := time.Parse(time.RFC3339Nano, candidate.BuildTimestamp)
		if leftErr == nil && rightErr == nil {
			switch {
			case right.After(left):
				return "newer", "candidate build time is newer"
			case right.Before(left):
				return "older", "candidate build time is older"
			default:
				return "different", "build times match but content/build hashes differ"
			}
		}
		if len(current.BuildTimestamp) == len(candidate.BuildTimestamp) {
			switch strings.Compare(candidate.BuildTimestamp, current.BuildTimestamp) {
			case 1:
				return "newer", "candidate compact build timestamp is newer"
			case -1:
				return "older", "candidate compact build timestamp is older"
			default:
				return "different", "build timestamps match but content/build hashes differ"
			}
		}
	}
	return "different", "hashes differ and comparable build time is unavailable"
}

func candidateAfter(left, right Candidate) bool {
	if left.PackedTimestamp != right.PackedTimestamp {
		return left.PackedTimestamp > right.PackedTimestamp
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.After(right.CreatedAt)
	}
	return left.BuildTimestamp > right.BuildTimestamp
}

func optionalNormalizedDigest(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return normalizeDigest(value)
}

func cloneMetadata(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || len(key) > 64 || len(value) > 512 || len(result) >= 32 {
			continue
		}
		result[key] = value
	}
	return result
}
