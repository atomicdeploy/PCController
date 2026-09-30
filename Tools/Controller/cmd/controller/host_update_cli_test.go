package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/artifacts"
)

func TestDelegatePrimaryHostUpdateStreamsAndVerifiesRestartedDigest(t *testing.T) {
	content := []byte("verified-host-candidate")
	digestValue := sha256.Sum256(content)
	digest := hex.EncodeToString(digestValue[:])
	path := filepath.Join(t.TempDir(), "controller.exe")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var received []byte
	call := func(_ context.Context, method string, params, target any) error {
		switch method {
		case "controller.artifact.upload.begin":
			request := params.(artifacts.PeerUploadBeginRequest)
			if request.SHA256 != digest || request.Bytes != int64(len(content)) {
				t.Fatalf("begin request=%#v", request)
			}
			*target.(*artifacts.PeerUploadBeginResult) = artifacts.PeerUploadBeginResult{
				TransferID: "transfer", ChunkBytes: 4,
			}
		case "controller.artifact.upload.chunk":
			request := params.(artifacts.PeerUploadChunkRequest)
			if request.Offset != int64(len(received)) {
				t.Fatalf("chunk offset=%d received=%d", request.Offset, len(received))
			}
			received = append(received, request.Data...)
			*target.(*artifacts.PeerUploadChunkResult) = artifacts.PeerUploadChunkResult{
				TransferID: "transfer", NextOffset: int64(len(received)), BytesTotal: int64(len(content)),
			}
		case "controller.artifact.upload.finish":
			*target.(*artifacts.OperationResult) = artifacts.OperationResult{Artifact: &artifacts.Descriptor{
				Kind: artifacts.KindHostExecutable, SHA256: digest,
			}}
		case "controller.update.host":
			request := params.(artifacts.UpdateRequest)
			if request.ArtifactSHA256 != digest || !request.Authorized || request.IdempotencyKey != "intent" {
				t.Fatalf("update request=%#v", request)
			}
			*target.(*artifacts.OperationResult) = artifacts.OperationResult{Operation: artifacts.UpdateStatus{ID: "operation"}}
		case "controller.artifact.manifest":
			*target.(*artifacts.Manifest) = artifacts.Manifest{Current: artifacts.CurrentArtifacts{Host: &artifacts.Descriptor{SHA256: digest}}}
		default:
			t.Fatalf("unexpected method %q", method)
		}
		return nil
	}
	var output strings.Builder
	if err := delegatePrimaryHostUpdate(context.Background(), path, digest, "intent", &output, call); err != nil {
		t.Fatal(err)
	}
	if string(received) != string(content) {
		t.Fatalf("received %q want %q", received, content)
	}
	if !strings.Contains(output.String(), "terminal_verified=true") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestDelegatePrimaryHostUpdateRejectsExpectedDigestBeforeRPC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.exe")
	if err := os.WriteFile(path, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := delegatePrimaryHostUpdate(context.Background(), path, strings.Repeat("a", 64), "", nil,
		func(context.Context, string, any, any) error {
			called = true
			return nil
		})
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("RPC called for a digest-mismatched candidate")
	}
}

func TestHostUpdateRequestRemainsJSONSerializable(t *testing.T) {
	_, err := json.Marshal(artifacts.PeerUploadChunkRequest{TransferID: "transfer", Data: []byte("data")})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunHostUpdateAcceptsDocumentedFileBeforeFlags(t *testing.T) {
	var stderr strings.Builder
	err := runHostUpdate([]string{"host", "missing.exe", "--expected-sha256", strings.Repeat("a", 64)}, io.Discard, &stderr)
	if err == nil || !strings.Contains(err.Error(), "open host update candidate") {
		t.Fatalf("err=%v stderr=%q", err, stderr.String())
	}
}
