package artifacts

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const PeerUploadChunkBytes = 384 << 10
const peerUploadTTL = 15 * time.Minute

type peerUpload struct {
	file     *os.File
	path     string
	options  PutOptions
	received int64
	created  time.Time
}

func (service *Service) BeginPeerUpload(request PeerUploadBeginRequest) (PeerUploadBeginResult, error) {
	if service == nil || service.store == nil {
		return PeerUploadBeginResult{}, errors.New("artifact service is unavailable")
	}
	if !ValidKind(request.Kind) {
		return PeerUploadBeginResult{}, fmt.Errorf("unsupported artifact kind %q", request.Kind)
	}
	if request.Bytes < 1 || request.Bytes > maxBytes(request.Kind) {
		return PeerUploadBeginResult{}, fmt.Errorf("%s peer upload size must be 1..%d bytes", request.Kind, maxBytes(request.Kind))
	}
	digest, err := normalizeSHA256(request.SHA256)
	if err != nil {
		return PeerUploadBeginResult{}, err
	}
	if _, err := safeArtifactName(request.Name, request.Kind); err != nil {
		return PeerUploadBeginResult{}, err
	}
	if _, err := validateDescriptorMetadata(request.Metadata); err != nil {
		return PeerUploadBeginResult{}, err
	}
	file, err := os.CreateTemp(service.store.Root(), ".peer-artifact-*.upload")
	if err != nil {
		return PeerUploadBeginResult{}, fmt.Errorf("create peer artifact staging file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return PeerUploadBeginResult{}, err
	}
	id := newOperationID()
	upload := &peerUpload{file: file, path: file.Name(), created: time.Now(), options: PutOptions{
		Kind: request.Kind, Name: request.Name, Source: "authenticated-peer",
		ExpectedSHA256: digest, ExpectedBytes: request.Bytes,
		BuildHash: request.BuildHash, BuildTimestamp: request.BuildTimestamp,
		PackedTimestamp: request.PackedTimestamp, Platform: request.Platform,
		Metadata: request.Metadata,
	}}
	service.mu.Lock()
	for transferID, existing := range service.peerUploads {
		if time.Since(existing.created) <= peerUploadTTL {
			continue
		}
		delete(service.peerUploads, transferID)
		_ = existing.file.Close()
		_ = os.Remove(existing.path)
	}
	if len(service.peerUploads) >= 8 {
		service.mu.Unlock()
		_ = file.Close()
		_ = os.Remove(file.Name())
		return PeerUploadBeginResult{}, errors.New("too many concurrent peer artifact transfers")
	}
	service.peerUploads[id] = upload
	service.mu.Unlock()
	return PeerUploadBeginResult{TransferID: id, ChunkBytes: PeerUploadChunkBytes}, nil
}

func (service *Service) AppendPeerUpload(request PeerUploadChunkRequest) (PeerUploadChunkResult, error) {
	id := strings.TrimSpace(request.TransferID)
	if id == "" {
		return PeerUploadChunkResult{}, errors.New("transfer_id is required")
	}
	if len(request.Data) == 0 || len(request.Data) > PeerUploadChunkBytes {
		return PeerUploadChunkResult{}, fmt.Errorf("peer upload chunk must be 1..%d bytes", PeerUploadChunkBytes)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	upload := service.peerUploads[id]
	if upload == nil {
		return PeerUploadChunkResult{}, os.ErrNotExist
	}
	if request.Offset != upload.received {
		return PeerUploadChunkResult{}, fmt.Errorf("peer upload offset is %d; expected %d", request.Offset, upload.received)
	}
	if upload.received+int64(len(request.Data)) > upload.options.ExpectedBytes {
		return PeerUploadChunkResult{}, errors.New("peer upload exceeds declared size")
	}
	written, err := upload.file.Write(request.Data)
	if err != nil {
		return PeerUploadChunkResult{}, fmt.Errorf("write peer upload: %w", err)
	}
	if written != len(request.Data) {
		return PeerUploadChunkResult{}, io.ErrShortWrite
	}
	upload.received += int64(written)
	return PeerUploadChunkResult{TransferID: id, NextOffset: upload.received, BytesTotal: upload.options.ExpectedBytes}, nil
}

func (service *Service) FinishPeerUpload(request PeerUploadFinishRequest) (OperationResult, error) {
	id := strings.TrimSpace(request.TransferID)
	service.mu.Lock()
	upload := service.peerUploads[id]
	delete(service.peerUploads, id)
	service.mu.Unlock()
	if upload == nil {
		return OperationResult{}, os.ErrNotExist
	}
	defer os.Remove(upload.path)
	if upload.received != upload.options.ExpectedBytes {
		_ = upload.file.Close()
		return OperationResult{}, fmt.Errorf("peer upload received %d of %d bytes", upload.received, upload.options.ExpectedBytes)
	}
	if err := upload.file.Sync(); err != nil {
		_ = upload.file.Close()
		return OperationResult{}, err
	}
	if err := upload.file.Close(); err != nil {
		return OperationResult{}, err
	}
	file, err := os.Open(upload.path)
	if err != nil {
		return OperationResult{}, err
	}
	defer file.Close()
	return service.UploadOperation(file, upload.options)
}

func (service *Service) AbortPeerUpload(request PeerUploadFinishRequest) error {
	id := strings.TrimSpace(request.TransferID)
	service.mu.Lock()
	upload := service.peerUploads[id]
	delete(service.peerUploads, id)
	service.mu.Unlock()
	if upload == nil {
		return nil
	}
	return errors.Join(upload.file.Close(), os.Remove(upload.path))
}
