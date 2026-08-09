package releaseplane

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/artifacts"
)

const (
	maxArchiveEntries = 1024
	maxArchiveBytes   = int64(512 << 20)
)

type transferProgress func(done, total int64, detail string)

func (client *Client) stage(
	ctx context.Context,
	artifactService *artifacts.Service,
	request StageRequest,
	progress transferProgress,
) (artifacts.Descriptor, error) {
	if artifactService == nil {
		return artifacts.Descriptor{}, errors.New("artifact store is unavailable")
	}
	if err := validateCandidate(request.Candidate); err != nil {
		return artifacts.Descriptor{}, err
	}
	candidate := request.Candidate
	response, err := client.get(ctx, candidate.URL, request.BearerToken, "application/octet-stream")
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	defer response.Body.Close()
	expectedTransfer := candidate.Bytes
	expectedDigest := candidate.SHA256
	if candidate.Archive {
		expectedTransfer = candidate.ArchiveBytes
		expectedDigest = candidate.ArchiveSHA256
	}
	if expectedTransfer > 0 && response.ContentLength >= 0 && response.ContentLength != expectedTransfer {
		return artifacts.Descriptor{}, fmt.Errorf("download Content-Length mismatch: expected %d, received %d", expectedTransfer, response.ContentLength)
	}
	total := expectedTransfer
	if total <= 0 {
		total = response.ContentLength
	}
	limit := maxDownloadBytes(candidate.Kind, candidate.Archive)
	if total > limit {
		return artifacts.Descriptor{}, fmt.Errorf("download exceeds %d-byte limit", limit)
	}
	temporary, err := os.CreateTemp("", "controller-release-*.download")
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	reader := io.LimitReader(response.Body, limit+1)
	written, copyErr := copyProgress(io.MultiWriter(temporary, hash), reader, total, progress)
	closeErr := temporary.Close()
	if copyErr != nil {
		return artifacts.Descriptor{}, fmt.Errorf("download candidate: %w", copyErr)
	}
	if closeErr != nil {
		return artifacts.Descriptor{}, closeErr
	}
	if written > limit {
		return artifacts.Descriptor{}, fmt.Errorf("download exceeds %d-byte limit", limit)
	}
	if expectedTransfer > 0 && written != expectedTransfer {
		return artifacts.Descriptor{}, fmt.Errorf("download size mismatch: expected %d, received %d", expectedTransfer, written)
	}
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if expectedDigest != "" {
		normalized, _ := normalizeDigest(expectedDigest)
		if actualDigest != normalized {
			return artifacts.Descriptor{}, fmt.Errorf("download SHA-256 mismatch: expected %s, received %s", normalized, actualDigest)
		}
	}
	if candidate.Archive {
		candidate.ArchiveSHA256 = actualDigest
		return stageArchive(artifactService, temporaryPath, candidate)
	}
	input, err := os.Open(temporaryPath)
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	defer input.Close()
	result, err := artifactService.UploadOperation(input, artifacts.PutOptions{
		Kind: candidate.Kind, Name: candidateName(candidate), Source: candidate.Source,
		ExpectedSHA256: firstValue(candidate.SHA256, actualDigest), ExpectedBytes: firstPositive(candidate.Bytes, written),
		BuildHash: candidate.BuildHash, BuildTimestamp: candidate.BuildTimestamp,
		PackedTimestamp: candidate.PackedTimestamp, Platform: candidate.Platform,
		Metadata: candidateProvenance(candidate),
	})
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	if result.Artifact == nil {
		return artifacts.Descriptor{}, errors.New("artifact import returned no descriptor")
	}
	return *result.Artifact, nil
}

func stageArchive(service *artifacts.Service, archivePath string, candidate Candidate) (artifacts.Descriptor, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return artifacts.Descriptor{}, fmt.Errorf("open update ZIP: %w", err)
	}
	defer reader.Close()
	entry, err := selectArchiveEntry(&reader.Reader, candidate)
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	input, err := entry.Open()
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	defer input.Close()
	expectedBytes := candidate.Bytes
	if expectedBytes == 0 {
		expectedBytes = int64(entry.UncompressedSize64)
	}
	result, err := service.UploadOperation(input, artifacts.PutOptions{
		Kind: candidate.Kind, Name: firstValue(candidate.ArtifactName, path.Base(strings.ReplaceAll(entry.Name, "\\", "/"))),
		Source: candidate.Source, ExpectedSHA256: candidate.SHA256, ExpectedBytes: expectedBytes,
		BuildHash: candidate.BuildHash, BuildTimestamp: candidate.BuildTimestamp,
		PackedTimestamp: candidate.PackedTimestamp, Platform: candidate.Platform,
		Metadata: candidateProvenance(candidate),
	})
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	if result.Artifact == nil {
		return artifacts.Descriptor{}, errors.New("archive import returned no descriptor")
	}
	return *result.Artifact, nil
}

func candidateProvenance(candidate Candidate) map[string]string {
	result := make(map[string]string)
	add := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\r\n\x00") {
			result[key] = value
		}
	}
	provider := strings.ToLower(strings.TrimSpace(candidate.Source))
	switch provider {
	case "github-workflow", "github-release", "manifest":
		add("provider", provider)
	default:
		add("provider", "remote")
	}
	add("repository", candidate.Repository)
	add("release_tag", candidate.ReleaseTag)
	if candidate.WorkflowRunID != 0 {
		add("workflow_run_id", strconv.FormatInt(candidate.WorkflowRunID, 10))
	}
	add("candidate_id", candidate.ID)
	add("archive_sha256", candidate.ArchiveSHA256)
	add("archive_path", candidate.ArchivePath)
	for _, key := range []string{"release_id", "artifact_id", "asset_id", "workflow", "branch"} {
		add(key, candidate.Metadata[key])
	}
	return result
}

func selectArchiveEntry(reader *zip.Reader, candidate Candidate) (*zip.File, error) {
	if len(reader.File) == 0 || len(reader.File) > maxArchiveEntries {
		return nil, fmt.Errorf("ZIP entry count must be 1..%d", maxArchiveEntries)
	}
	wantedPath := cleanArchivePath(candidate.ArchivePath)
	var total uint64
	bestScore := -1
	var best *zip.File
	ambiguous := false
	for _, entry := range reader.File {
		if !safeArchiveEntry(entry) {
			return nil, fmt.Errorf("unsafe ZIP entry %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		total += entry.UncompressedSize64
		if total > uint64(maxArchiveBytes) {
			return nil, fmt.Errorf("ZIP expands beyond %d bytes", maxArchiveBytes)
		}
		cleaned := cleanArchivePath(entry.Name)
		if wantedPath != "" {
			if cleaned == wantedPath {
				return entry, nil
			}
			continue
		}
		score := archiveEntryScore(entry, candidate)
		if score < 0 {
			continue
		}
		if score > bestScore {
			best, bestScore, ambiguous = entry, score, false
		} else if score == bestScore {
			ambiguous = true
		}
	}
	if wantedPath != "" {
		return nil, fmt.Errorf("ZIP does not contain requested artifact path %q", wantedPath)
	}
	if best == nil {
		return nil, fmt.Errorf("ZIP contains no %s artifact for platform %s", candidate.Kind, candidate.Platform)
	}
	if ambiguous {
		return nil, errors.New("ZIP contains multiple equally suitable artifacts; provide archive_path")
	}
	return best, nil
}

func archiveEntryScore(entry *zip.File, candidate Candidate) int {
	cleaned := cleanArchivePath(entry.Name)
	base := path.Base(cleaned)
	lower := strings.ToLower(base)
	extension := strings.ToLower(path.Ext(lower))
	score := -1
	switch candidate.Kind {
	case artifacts.KindFirmware, artifacts.KindFlashBackup:
		if extension == ".hex" {
			score = 10
			if strings.Contains(lower, "application") && !strings.Contains(lower, "bootloader") {
				score += 4
			}
		}
	case artifacts.KindEEPROM:
		if extension == ".eep" || extension == ".hex" {
			score = 10
		}
	case artifacts.KindHostExecutable:
		platform := normalizedPlatform(candidate.Platform)
		if strings.HasPrefix(platform, "windows/") {
			if extension == ".exe" {
				score = 10
			}
		} else if extension == "" && !strings.EqualFold(lower, "license") && !strings.EqualFold(lower, "readme") {
			score = 10
		}
	}
	if score < 0 {
		return -1
	}
	if candidate.ArtifactName != "" && strings.EqualFold(base, path.Base(strings.ReplaceAll(candidate.ArtifactName, "\\", "/"))) {
		score += 100
	}
	if platform := strings.ReplaceAll(strings.ToLower(candidate.Platform), "/", "-"); platform != "" {
		flat := strings.NewReplacer("_", "-", "/", "-").Replace(strings.ToLower(cleaned))
		if strings.Contains(flat, platform) {
			score += 30
		}
	}
	return score
}

func safeArchiveEntry(entry *zip.File) bool {
	if entry == nil || cleanArchivePath(entry.Name) == "" {
		return false
	}
	normalized := strings.ReplaceAll(entry.Name, "\\", "/")
	if path.IsAbs(normalized) || filepath.IsAbs(entry.Name) {
		return false
	}
	cleaned := path.Clean(normalized)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	mode := entry.Mode()
	return mode&os.ModeSymlink == 0 && mode&os.ModeDevice == 0 && mode&os.ModeNamedPipe == 0 && mode&os.ModeSocket == 0
}

func cleanArchivePath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	if value == "" {
		return ""
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func maxDownloadBytes(kind artifacts.Kind, archive bool) int64 {
	limit := int64(8 << 20)
	if kind == artifacts.KindHostExecutable {
		limit = 256 << 20
	}
	if archive {
		limit *= 2
		if limit > maxArchiveBytes {
			limit = maxArchiveBytes
		}
	}
	return limit
}

func copyProgress(destination io.Writer, source io.Reader, total int64, progress transferProgress) (int64, error) {
	buffer := make([]byte, 128<<10)
	var written int64
	lastUpdate := time.Time{}
	for {
		count, readErr := source.Read(buffer)
		if count > 0 {
			output, writeErr := destination.Write(buffer[:count])
			written += int64(output)
			if writeErr != nil {
				return written, writeErr
			}
			if output != count {
				return written, io.ErrShortWrite
			}
			if progress != nil && (lastUpdate.IsZero() || time.Since(lastUpdate) >= 100*time.Millisecond || (total > 0 && written >= total)) {
				progress(written, total, "streaming verified update candidate")
				lastUpdate = time.Now()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return written, readErr
		}
	}
	return written, nil
}

func candidateName(candidate Candidate) string {
	return firstValue(candidate.ArtifactName, path.Base(strings.ReplaceAll(candidate.Name, "\\", "/")))
}

func firstPositive(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
