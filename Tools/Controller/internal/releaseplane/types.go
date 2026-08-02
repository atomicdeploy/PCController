// Package releaseplane discovers, verifies, and stages update artifacts from
// generic manifests and GitHub workflow/release inventories. It never programs
// hardware; the artifacts package remains the only update execution boundary.
package releaseplane

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"pccontroller.local/controller/internal/artifacts"
)

const ManifestFormat = "controller-update-manifest/v1"

// Candidate preserves the source metadata needed to compare and stage one
// immutable update without smuggling authentication secrets into inventories.
type Candidate struct {
	ID              string            `json:"id"`
	Source          string            `json:"source"`
	Repository      string            `json:"repository,omitempty"`
	ReleaseTag      string            `json:"release_tag,omitempty"`
	WorkflowRunID   int64             `json:"workflow_run_id,omitempty"`
	Kind            artifacts.Kind    `json:"kind"`
	Name            string            `json:"name"`
	ArtifactName    string            `json:"artifact_name,omitempty"`
	URL             string            `json:"url"`
	Platform        string            `json:"platform,omitempty"`
	Bytes           int64             `json:"bytes,omitempty"`
	SHA256          string            `json:"sha256,omitempty"`
	Archive         bool              `json:"archive,omitempty"`
	ArchiveBytes    int64             `json:"archive_bytes,omitempty"`
	ArchiveSHA256   string            `json:"archive_sha256,omitempty"`
	ArchivePath     string            `json:"archive_path,omitempty"`
	BuildHash       string            `json:"build_hash,omitempty"`
	BuildTimestamp  string            `json:"build_timestamp,omitempty"`
	PackedTimestamp uint32            `json:"packed_timestamp,omitempty"`
	CreatedAt       time.Time         `json:"created_at,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type DiscoveryResult struct {
	Source     string      `json:"source"`
	CheckedAt  time.Time   `json:"checked_at"`
	Candidates []Candidate `json:"candidates"`
}

type GitHubWorkflowRequest struct {
	Repository      string         `json:"repository"`
	Branch          string         `json:"branch,omitempty"`
	Workflow        string         `json:"workflow,omitempty"`
	Kind            artifacts.Kind `json:"kind"`
	Platform        string         `json:"platform,omitempty"`
	BearerToken     string         `json:"bearer_token,omitempty"`
	APIBaseURL      string         `json:"api_base_url,omitempty"`
	BuildHash       string         `json:"build_hash,omitempty"`
	BuildTimestamp  string         `json:"build_timestamp,omitempty"`
	PackedTimestamp uint32         `json:"packed_timestamp,omitempty"`
}

type GitHubReleaseRequest struct {
	Repository        string         `json:"repository"`
	Tag               string         `json:"tag,omitempty"`
	IncludePrerelease bool           `json:"include_prerelease,omitempty"`
	Kind              artifacts.Kind `json:"kind"`
	Platform          string         `json:"platform,omitempty"`
	BearerToken       string         `json:"bearer_token,omitempty"`
	APIBaseURL        string         `json:"api_base_url,omitempty"`
	PackedTimestamp   uint32         `json:"packed_timestamp,omitempty"`
}

type ManifestRequest struct {
	URL         string `json:"url"`
	BearerToken string `json:"bearer_token,omitempty"`
}

type Manifest struct {
	Format      string             `json:"format"`
	GeneratedAt time.Time          `json:"generated_at,omitempty"`
	Artifacts   []ManifestArtifact `json:"artifacts"`
}

type ManifestArtifact struct {
	Kind            artifacts.Kind    `json:"kind"`
	Name            string            `json:"name"`
	ArtifactName    string            `json:"artifact_name,omitempty"`
	URL             string            `json:"url"`
	Platform        string            `json:"platform,omitempty"`
	Bytes           int64             `json:"bytes,omitempty"`
	SHA256          string            `json:"sha256,omitempty"`
	Archive         bool              `json:"archive,omitempty"`
	ArchiveBytes    int64             `json:"archive_bytes,omitempty"`
	ArchiveSHA256   string            `json:"archive_sha256,omitempty"`
	ArchivePath     string            `json:"archive_path,omitempty"`
	BuildHash       string            `json:"build_hash,omitempty"`
	BuildTimestamp  string            `json:"build_timestamp,omitempty"`
	PackedTimestamp uint32            `json:"packed_timestamp,omitempty"`
	CreatedAt       time.Time         `json:"created_at,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type Identity struct {
	SHA256          string `json:"sha256,omitempty"`
	BuildHash       string `json:"build_hash,omitempty"`
	BuildTimestamp  string `json:"build_timestamp,omitempty"`
	PackedTimestamp uint32 `json:"packed_timestamp,omitempty"`
}

type CheckRequest struct {
	Current    Identity       `json:"current"`
	Kind       artifacts.Kind `json:"kind"`
	Platform   string         `json:"platform,omitempty"`
	Candidates []Candidate    `json:"candidates"`
}

type CheckResult struct {
	Status    string     `json:"status"`
	Candidate *Candidate `json:"candidate,omitempty"`
	Reason    string     `json:"reason"`
}

type StageRequest struct {
	Candidate      Candidate `json:"candidate"`
	BearerToken    string    `json:"bearer_token,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
}

type StageStatus struct {
	ID              string                `json:"id"`
	CandidateID     string                `json:"candidate_id,omitempty"`
	Kind            artifacts.Kind        `json:"kind,omitempty"`
	State           string                `json:"state"`
	ProgressPercent int                   `json:"progress_percent"`
	BytesDone       int64                 `json:"bytes_done,omitempty"`
	BytesTotal      int64                 `json:"bytes_total,omitempty"`
	Detail          string                `json:"detail,omitempty"`
	Error           string                `json:"error,omitempty"`
	StartedAt       time.Time             `json:"started_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
	Artifact        *artifacts.Descriptor `json:"artifact,omitempty"`
}

type StageResult struct {
	Operation StageStatus `json:"operation"`
}

func CurrentPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }

func normalizedPlatform(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "/")
	if value == "" {
		return CurrentPlatform()
	}
	return value
}

func validateKind(kind artifacts.Kind) error {
	if _, err := artifacts.ParseKind(string(kind)); err != nil {
		return err
	}
	return nil
}

func validateCandidate(candidate Candidate) error {
	if err := validateKind(candidate.Kind); err != nil {
		return err
	}
	if strings.TrimSpace(candidate.Name) == "" {
		return fmt.Errorf("candidate name is required")
	}
	if candidate.Bytes < 0 || candidate.ArchiveBytes < 0 {
		return fmt.Errorf("candidate byte counts cannot be negative")
	}
	if err := validateRemoteURL(candidate.URL); err != nil {
		return err
	}
	if candidate.SHA256 != "" {
		if _, err := normalizeDigest(candidate.SHA256); err != nil {
			return fmt.Errorf("candidate sha256: %w", err)
		}
	}
	if candidate.ArchiveSHA256 != "" {
		if _, err := normalizeDigest(candidate.ArchiveSHA256); err != nil {
			return fmt.Errorf("candidate archive_sha256: %w", err)
		}
	}
	return nil
}
