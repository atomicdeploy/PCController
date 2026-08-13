package ipcjson

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestPeerHostUpdateTransfersVerifiedArtifactThenQueuesRemoteCoordinator(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := store.PutFile(path, artifacts.PutOptions{
		Kind: artifacts.KindHostExecutable, Name: filepath.Base(path),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactService, err := artifacts.NewService(artifacts.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer artifactService.Close()
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	var transferID = "transfer-test"
	var received int64
	updateCalled := false
	service := &Service{Client: client, Artifacts: artifactService}
	service.BridgeCall = func(_ context.Context, peer string, request Request) (Response, error) {
		if peer != "edge" {
			t.Fatalf("peer=%q", peer)
		}
		response := Response{JSONRPC: Version, ID: request.ID}
		switch request.Method {
		case "controller.artifact.upload.begin":
			var value artifacts.PeerUploadBeginRequest
			if err := json.Unmarshal(request.Params, &value); err != nil {
				t.Fatal(err)
			}
			if value.SHA256 != descriptor.SHA256 || value.Bytes != descriptor.Bytes || value.Platform != descriptor.Platform {
				t.Fatalf("begin=%#v descriptor=%#v", value, descriptor)
			}
			response.Result = artifacts.PeerUploadBeginResult{TransferID: transferID, ChunkBytes: 32 << 10}
		case "controller.artifact.upload.chunk":
			var value artifacts.PeerUploadChunkRequest
			if err := json.Unmarshal(request.Params, &value); err != nil {
				t.Fatal(err)
			}
			if value.TransferID != transferID || value.Offset != received || len(value.Data) == 0 {
				t.Fatalf("chunk transfer=%q offset=%d received=%d bytes=%d", value.TransferID, value.Offset, received, len(value.Data))
			}
			received += int64(len(value.Data))
			response.Result = artifacts.PeerUploadChunkResult{TransferID: transferID, NextOffset: received, BytesTotal: descriptor.Bytes}
		case "controller.artifact.upload.finish":
			if received != descriptor.Bytes {
				t.Fatalf("finish after %d/%d bytes", received, descriptor.Bytes)
			}
			response.Result = artifacts.OperationResult{Artifact: &descriptor}
		case "controller.update.host":
			var value artifacts.UpdateRequest
			if err := json.Unmarshal(request.Params, &value); err != nil {
				t.Fatal(err)
			}
			if !value.Authorized || value.ArtifactSHA256 != descriptor.SHA256 {
				t.Fatalf("update=%#v", value)
			}
			updateCalled = true
			response.Result = artifacts.OperationResult{Operation: artifacts.UpdateStatus{ID: "remote-update", Kind: "host", State: "queued"}}
		case "controller.artifact.upload.abort":
			t.Fatal("successful transfer was aborted")
		default:
			t.Fatalf("method=%q", request.Method)
		}
		return response, nil
	}
	result, err := service.updatePeerHost(context.Background(), peerHostUpdateRequest{
		Peer: "edge", ArtifactSHA256: descriptor.SHA256, Authorized: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updateCalled || result.Peer != "edge" || result.Artifact.SHA256 != descriptor.SHA256 || result.Operation.ID != "remote-update" {
		t.Fatalf("result=%#v updateCalled=%t", result, updateCalled)
	}
}

func TestPeerHostUpdateRequiresExplicitAuthorization(t *testing.T) {
	_, err := (&Service{}).updatePeerHost(context.Background(), peerHostUpdateRequest{Peer: "edge"})
	if err == nil {
		t.Fatal("unauthorized peer host update accepted")
	}
}
