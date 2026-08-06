package releaseplane

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/artifacts"
)

func TestGitHubWorkflowAndReleaseDiscoveryPreserveDigests(t *testing.T) {
	archiveDigest := strings.Repeat("a", 64)
	assetDigest := strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/acme/device/actions/runs":
			writeTestJSON(writer, map[string]any{"workflow_runs": []map[string]any{{
				"id": 42, "name": "Build", "head_branch": "main", "head_sha": "abcdef012345",
				"status": "completed", "conclusion": "success", "created_at": "2026-08-02T01:02:03Z",
				"ignored_future_field": true,
			}}})
		case "/repos/acme/device/actions/runs/42/artifacts":
			writeTestJSON(writer, map[string]any{"artifacts": []map[string]any{{
				"id": 7, "name": "board-build", "size_in_bytes": 500,
				"archive_download_url": serverURL(request) + "/download/workflow.zip",
				"digest":               "sha256:" + archiveDigest, "expired": false, "created_at": "2026-08-02T01:03:00Z",
			}}})
		case "/repos/acme/device/releases/latest":
			writeTestJSON(writer, map[string]any{
				"id": 9, "tag_name": "v2", "name": "Release", "target_commitish": "1234567abcdef",
				"draft": false, "prerelease": false, "published_at": "2026-08-02T02:00:00Z",
				"assets": []map[string]any{
					{"id": 10, "name": "device-windows-amd64.zip", "state": "uploaded", "size": 800, "browser_download_url": serverURL(request) + "/download/device.zip", "created_at": "2026-08-02T02:01:00Z"},
					{"id": 11, "name": "SHA256SUMS", "state": "uploaded", "size": 100, "browser_download_url": serverURL(request) + "/download/SHA256SUMS", "created_at": "2026-08-02T02:01:00Z"},
				},
			})
		case "/download/SHA256SUMS":
			_, _ = io.WriteString(writer, assetDigest+"  device-windows-amd64.zip\n")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	workflow, err := client.DiscoverWorkflow(context.Background(), GitHubWorkflowRequest{
		Repository: "acme/device", Branch: "main", Workflow: "Build",
		Kind: artifacts.KindFirmware, APIBaseURL: server.URL, PackedTimestamp: 1234,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(workflow.Candidates) != 1 || workflow.Candidates[0].ArchiveSHA256 != archiveDigest ||
		workflow.Candidates[0].BuildHash != "abcdef012345" || workflow.Candidates[0].PackedTimestamp != 1234 {
		t.Fatalf("workflow candidate = %#v", workflow.Candidates)
	}
	release, err := client.DiscoverRelease(context.Background(), GitHubReleaseRequest{
		Repository: "acme/device", Kind: artifacts.KindHostExecutable,
		Platform: "windows/amd64", APIBaseURL: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(release.Candidates) != 1 || release.Candidates[0].ArchiveSHA256 != assetDigest ||
		release.Candidates[0].Platform != "windows/amd64" || release.Candidates[0].ReleaseTag != "v2" {
		t.Fatalf("release candidate = %#v", release.Candidates)
	}
}

func TestManifestResolutionAndPlatformAwareComparison(t *testing.T) {
	manifest := Manifest{
		Format: ManifestFormat, GeneratedAt: time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC),
		Artifacts: []ManifestArtifact{{
			Kind: artifacts.KindFirmware, Name: "board.hex", URL: "files/board.hex",
			Bytes: 42, SHA256: strings.Repeat("c", 64), BuildHash: "beef1234",
			BuildTimestamp: "260802030000", PackedTimestamp: 0x12345678,
			Metadata: map[string]string{"channel": "stable"},
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeTestJSON(writer, manifest)
	}))
	defer server.Close()
	result, err := NewClient(server.Client()).DiscoverManifest(context.Background(), ManifestRequest{URL: server.URL + "/updates/manifest.json"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := result.Candidates[0]
	if candidate.URL != server.URL+"/updates/files/board.hex" || candidate.PackedTimestamp != 0x12345678 || candidate.Metadata["channel"] != "stable" {
		t.Fatalf("manifest candidate = %#v", candidate)
	}
	comparison, err := CheckForUpdate(CheckRequest{
		Current: Identity{SHA256: strings.Repeat("d", 64), BuildTimestamp: candidate.BuildTimestamp, PackedTimestamp: candidate.PackedTimestamp},
		Kind:    artifacts.KindFirmware, Candidates: []Candidate{candidate},
	})
	if err != nil || comparison.Status != "different" {
		t.Fatalf("comparison = %#v, %v", comparison, err)
	}
	comparison, err = CheckForUpdate(CheckRequest{
		Current: Identity{PackedTimestamp: candidate.PackedTimestamp - 1},
		Kind:    artifacts.KindFirmware, Candidates: []Candidate{candidate},
	})
	if err != nil || comparison.Status != "newer" {
		t.Fatalf("newer comparison = %#v, %v", comparison, err)
	}
}

func TestManifestIgnoresAdditiveFieldsWithinKnownFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{
			"format":"controller-update-manifest/v1",
			"publisher_extension":{"channel":"canary"},
			"artifacts":[{
				"kind":"firmware","name":"board.hex","url":"/board.hex",
				"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"future_hint":"safe to ignore"
			}]
		}`)
	}))
	defer server.Close()

	result, err := NewClient(server.Client()).DiscoverManifest(context.Background(), ManifestRequest{URL: server.URL + "/manifest.json"})
	if err != nil {
		t.Fatalf("discover additive manifest: %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].URL != server.URL+"/board.hex" {
		t.Fatalf("candidate=%#v", result.Candidates)
	}
}

func TestArchiveSelectionRejectsTraversal(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	unsafe, _ := writer.Create("../outside.hex")
	_, _ = unsafe.Write(testFirmwareHex())
	safe, _ := writer.Create("payload/application.hex")
	_, _ = safe.Write(testFirmwareHex())
	_ = writer.Close()
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selectArchiveEntry(reader, Candidate{Kind: artifacts.KindFirmware}); err == nil || !strings.Contains(err.Error(), "unsafe ZIP entry") {
		t.Fatalf("unsafe archive error = %v", err)
	}
}

func TestArchiveStageStreamsProgressAndPreservesMetadata(t *testing.T) {
	firmware := testFirmwareHex()
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	entry, _ := zipWriter.Create("bundle/application.hex")
	_, _ = entry.Write(firmware)
	_ = zipWriter.Close()
	archiveHash := sha256.Sum256(archive.Bytes())
	firmwareHash := sha256.Sum256(firmware)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", stringInt64(int64(archive.Len())))
		_, _ = writer.Write(archive.Bytes())
	}))
	defer server.Close()
	store, err := artifacts.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artifactService, err := artifacts.NewService(artifacts.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer artifactService.Close()
	events := make(chan string, 32)
	service, err := NewService(NewClient(server.Client()), artifactService, func(kind, _ string, _ map[string]string) { events <- kind })
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	started, err := service.StartStage(StageRequest{Candidate: Candidate{
		Source: "manifest", Repository: "owner/repository", Kind: artifacts.KindFirmware, Name: "release.zip", ArtifactName: "board.hex",
		URL: server.URL + "/release.zip", Archive: true, ArchiveBytes: int64(archive.Len()),
		ArchiveSHA256: hex.EncodeToString(archiveHash[:]), ArchivePath: "bundle/application.hex",
		Bytes: int64(len(firmware)), SHA256: hex.EncodeToString(firmwareHash[:]),
		BuildHash: "deadbeef", BuildTimestamp: "260802040000", PackedTimestamp: 0x11223344,
		Metadata: map[string]string{"release_id": "987", "ignored_extension": "not persisted"},
	}, IdempotencyKey: "stage-one"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-events:
			if event != "artifact.discovery.completed" {
				continue
			}
			status, statusErr := service.Status(started.Operation.ID)
			if statusErr != nil || status.Artifact == nil {
				t.Fatalf("completed status = %#v, %v", status, statusErr)
			}
			if status.Artifact.BuildHash != "DEADBEEF" || status.Artifact.BuildTimestamp != "260802040000" ||
				status.Artifact.PackedTimestamp != 0x11223344 || status.Artifact.Platform != "" {
				t.Fatalf("staged metadata = %#v", status.Artifact)
			}
			if status.Artifact.Metadata["provider"] != "manifest" ||
				status.Artifact.Metadata["repository"] != "owner/repository" ||
				status.Artifact.Metadata["release_id"] != "987" ||
				status.Artifact.Metadata["archive_sha256"] != hex.EncodeToString(archiveHash[:]) ||
				status.Artifact.Metadata["candidate_id"] == "" ||
				status.Artifact.Metadata["ignored_extension"] != "" {
				t.Fatalf("staged provenance = %#v", status.Artifact.Metadata)
			}
			request := StageRequest{Candidate: Candidate{
				Source: "manifest", Repository: "owner/repository", Kind: artifacts.KindFirmware, Name: "release.zip", ArtifactName: "board.hex",
				URL: server.URL + "/release.zip", Archive: true, ArchiveBytes: int64(archive.Len()),
				ArchiveSHA256: hex.EncodeToString(archiveHash[:]), ArchivePath: "bundle/application.hex",
				Bytes: int64(len(firmware)), SHA256: hex.EncodeToString(firmwareHash[:]),
				BuildHash: "deadbeef", BuildTimestamp: "260802040000", PackedTimestamp: 0x11223344,
				Metadata: map[string]string{"release_id": "987", "ignored_extension": "not persisted"},
			}, IdempotencyKey: "stage-one"}
			if repeated, repeatErr := service.StartStage(request); repeatErr != nil || repeated.Operation.ID != started.Operation.ID {
				t.Fatalf("idempotent replay = %#v, %v", repeated, repeatErr)
			}
			request.Candidate.Name = "different.hex"
			if _, conflictErr := service.StartStage(request); conflictErr == nil || !strings.Contains(conflictErr.Error(), "different staging request") {
				t.Fatalf("idempotency conflict = %v", conflictErr)
			}
			return
		case <-deadline:
			t.Fatal("stage did not complete")
		}
	}
}

func TestDefaultClientUsesProxyEnvironment(t *testing.T) {
	manifest := Manifest{Format: ManifestFormat, Artifacts: []ManifestArtifact{{
		Kind: artifacts.KindFirmware, Name: "board.hex", URL: "http://origin.invalid/board.hex",
	}}}
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Host != "origin.invalid" {
			t.Fatalf("proxy received URL %s", request.URL)
		}
		writeTestJSON(writer, manifest)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	result, err := NewClient(nil).DiscoverManifest(context.Background(), ManifestRequest{URL: "http://origin.invalid/manifest.json"})
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("proxied discovery = %#v, %v", result, err)
	}
}

func TestClosedServiceRejectsNewStaging(t *testing.T) {
	store, err := artifacts.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artifactService, err := artifacts.NewService(artifacts.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer artifactService.Close()
	service, err := NewService(nil, artifactService, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = service.StartStage(StageRequest{Candidate: Candidate{
		Source: "manifest", Kind: artifacts.KindFirmware, Name: "board.hex", URL: "https://example.invalid/board.hex",
	}})
	if err == nil || !strings.Contains(err.Error(), "service is closed") {
		t.Fatalf("closed staging error = %v", err)
	}
}

func TestLocalManifestServesContentAddressedInventory(t *testing.T) {
	store, err := artifacts.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artifactService, err := artifacts.NewService(artifacts.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer artifactService.Close()
	descriptor, err := artifactService.Upload(bytes.NewReader(testFirmwareHex()), artifacts.PutOptions{
		Kind: artifacts.KindFirmware, Name: "board.hex", Source: "release-test",
		BuildHash: "feedbeef", PackedTimestamp: 0x01020304,
		Metadata: map[string]string{"provider": "github-release", "release_id": "55"},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(nil, artifactService, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	request := httptest.NewRequest(http.MethodGet, "/api/discovery/manifest", nil)
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("manifest status=%d body=%s", response.Code, response.Body.String())
	}
	var manifest Manifest
	if err := json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != ManifestFormat || len(manifest.Artifacts) != 1 {
		t.Fatalf("manifest=%#v", manifest)
	}
	item := manifest.Artifacts[0]
	if item.SHA256 != descriptor.SHA256 || item.URL != "/api/artifacts/firmware/"+descriptor.SHA256 ||
		item.BuildHash != "FEEDBEEF" || item.PackedTimestamp != 0x01020304 ||
		item.Metadata["provider"] != "github-release" || item.Metadata["release_id"] != "55" {
		t.Fatalf("published artifact=%#v", item)
	}
}

func testFirmwareHex() []byte {
	return []byte(":0400000001020304F2\n:00000001FF\n")
}

func writeTestJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func serverURL(request *http.Request) string { return "http://" + request.Host }

func stringInt64(value int64) string { return strconv.FormatInt(value, 10) }
