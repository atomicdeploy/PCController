package ipcjson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/artifacts"
)

type peerHostUpdateRequest struct {
	Peer           string `json:"peer"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	Authorized     bool   `json:"authorized"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type peerHostUpdateResult struct {
	Peer      string                 `json:"peer"`
	Artifact  artifacts.Descriptor   `json:"artifact"`
	Operation artifacts.UpdateStatus `json:"operation"`
}

func (service *Service) updatePeerHost(ctx context.Context, request peerHostUpdateRequest) (result peerHostUpdateResult, err error) {
	peer := strings.TrimSpace(request.Peer)
	if peer == "" {
		return result, errors.New("peer is required")
	}
	if !request.Authorized {
		return result, errors.New("peer host update requires authorized=true")
	}
	if service.Artifacts == nil || service.BridgeCall == nil {
		return result, errors.New("artifact service and host bridge manager are required")
	}
	descriptor, file, err := service.Artifacts.Open(artifacts.KindHostExecutable, request.ArtifactSHA256)
	if err != nil {
		return result, fmt.Errorf("open verified host artifact: %w", err)
	}
	defer file.Close()
	operationID := "peer-host-" + strings.ToLower(strings.ReplaceAll(peer, " ", "-")) + "-" + descriptor.SHA256[:12]
	service.emitPeerUpdate(operationID, peer, descriptor, "queued", 0, "peer host update queued")
	defer func() {
		if err != nil {
			service.emitPeerUpdate(operationID, peer, descriptor, "failed", 0, err.Error())
		}
	}()

	var begin artifacts.PeerUploadBeginResult
	err = service.callPeer(ctx, peer, "controller.artifact.upload.begin", artifacts.PeerUploadBeginRequest{
		Kind: descriptor.Kind, Name: descriptor.Name, SHA256: descriptor.SHA256,
		Bytes: descriptor.Bytes, BuildHash: descriptor.BuildHash,
		BuildTimestamp: descriptor.BuildTimestamp, PackedTimestamp: descriptor.PackedTimestamp,
		Platform: descriptor.Platform, Metadata: descriptor.Metadata,
	}, &begin)
	if err != nil {
		return result, fmt.Errorf("begin peer artifact transfer: %w", err)
	}
	abort := true
	defer func() {
		if abort {
			abortContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = service.callPeer(abortContext, peer, "controller.artifact.upload.abort", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, nil)
		}
	}()
	chunkBytes := begin.ChunkBytes
	if chunkBytes < 1 || chunkBytes > artifacts.PeerUploadChunkBytes {
		return result, fmt.Errorf("peer selected invalid chunk size %d", chunkBytes)
	}
	buffer := make([]byte, chunkBytes)
	offset, lastPercent := int64(0), -1
	for offset < descriptor.Bytes {
		want := int64(len(buffer))
		if remaining := descriptor.Bytes - offset; remaining < want {
			want = remaining
		}
		count, readErr := io.ReadFull(file, buffer[:want])
		if readErr != nil {
			return result, fmt.Errorf("read verified host artifact at %d: %w", offset, readErr)
		}
		var chunk artifacts.PeerUploadChunkResult
		err = service.callPeer(ctx, peer, "controller.artifact.upload.chunk", artifacts.PeerUploadChunkRequest{
			TransferID: begin.TransferID, Offset: offset, Data: buffer[:count],
		}, &chunk)
		if err != nil {
			return result, fmt.Errorf("transfer host artifact at %d: %w", offset, err)
		}
		offset = chunk.NextOffset
		percent := int(offset * 80 / descriptor.Bytes)
		if percent/10 != lastPercent/10 {
			lastPercent = percent
			service.emitPeerUpdate(operationID, peer, descriptor, "downloading", percent, "transferring verified host artifact to peer")
		}
	}
	var uploaded artifacts.OperationResult
	if err = service.callPeer(ctx, peer, "controller.artifact.upload.finish", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, &uploaded); err != nil {
		return result, fmt.Errorf("verify peer artifact: %w", err)
	}
	abort = false
	if uploaded.Artifact == nil || uploaded.Artifact.SHA256 != descriptor.SHA256 {
		return result, errors.New("peer returned a different artifact identity")
	}
	service.emitPeerUpdate(operationID, peer, descriptor, "downloaded", 85, "peer verified the transferred host artifact")
	var update artifacts.OperationResult
	if err = service.callPeer(ctx, peer, "controller.update.host", artifacts.UpdateRequest{
		ArtifactSHA256: descriptor.SHA256, Authorized: true,
		IdempotencyKey: strings.TrimSpace(request.IdempotencyKey),
	}, &update); err != nil {
		return result, fmt.Errorf("queue peer host replacement: %w", err)
	}
	service.emitPeerUpdate(operationID, peer, descriptor, "programming", 95, "peer coordinator accepted graceful host replacement")
	result = peerHostUpdateResult{Peer: peer, Artifact: *uploaded.Artifact, Operation: update.Operation}
	return result, nil
}

func (service *Service) callPeer(ctx context.Context, peer, method string, params, target any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	response, err := service.BridgeCall(ctx, peer, Request{JSONRPC: Version, Method: method, Params: encoded})
	if err != nil {
		return err
	}
	if response.Error != nil {
		return response.Error
	}
	if target == nil {
		return nil
	}
	encoded, err = json.Marshal(response.Result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

func (service *Service) emitPeerUpdate(operationID, peer string, descriptor artifacts.Descriptor, state string, percent int, detail string) {
	if service.Client == nil {
		return
	}
	service.Client.EmitHostActionEvent("update."+state, detail, "bridge", "peer-host-update", map[string]string{
		"operation_id": operationID, "peer": peer, "kind": "host",
		"state": state, "progress_percent": strconv.Itoa(percent),
		"sha256": descriptor.SHA256, "bytes_total": strconv.FormatInt(descriptor.Bytes, 10),
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
}
