package artifacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

type PeerDownloadBeginRequest struct {
	Kind   Kind   `json:"kind"`
	SHA256 string `json:"sha256"`
}
type PeerDownloadChunkRequest struct {
	TransferID string `json:"transfer_id"`
	Offset     int64  `json:"offset"`
	Length     int    `json:"length"`
}
type PeerDownloadResult struct {
	TransferID string `json:"transfer_id"`
	SHA256     string `json:"sha256"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	BytesTotal int64  `json:"bytes_total"`
	ChunkBytes int    `json:"chunk_bytes"`
	Data       []byte `json:"data,omitempty"`
	EOF        bool   `json:"eof"`
}
type peerDownload struct {
	file       *os.File
	descriptor Descriptor
	updated    time.Time
}

func (service *Service) cleanupPeerDownloadsLocked(now time.Time) {
	for id, download := range service.peerDownloads {
		if now.Sub(download.updated) > peerUploadTTL {
			download.file.Close()
			delete(service.peerDownloads, id)
		}
	}
}

func (service *Service) BeginPeerDownload(request PeerDownloadBeginRequest) (PeerDownloadResult, error) {
	service.mu.Lock()
	service.cleanupPeerDownloadsLocked(time.Now())
	if service.peerUploadsClosed || len(service.peerDownloads)+service.peerDownloadPending >= peerUploadMaximumTransfers {
		service.mu.Unlock()
		return PeerDownloadResult{}, errors.New("artifact download capacity unavailable")
	}
	service.peerDownloadPending++
	service.peerUploadOps.Add(1)
	service.mu.Unlock()
	defer service.peerUploadOps.Done()
	// Open verifies the immutable file exactly once. Chunk reads retain this
	// handle instead of hashing the entire artifact again for every 384 KiB.
	descriptor, file, err := service.Open(request.Kind, request.SHA256)
	service.mu.Lock()
	defer service.mu.Unlock()
	service.peerDownloadPending--
	if err != nil {
		return PeerDownloadResult{}, err
	}
	if service.peerUploadsClosed {
		_ = file.Close()
		return PeerDownloadResult{}, errors.New("artifact service is closed")
	}
	if service.peerDownloads == nil {
		service.peerDownloads = make(map[string]*peerDownload)
	}
	id := newOperationID()
	service.peerDownloads[id] = &peerDownload{file: file, descriptor: descriptor, updated: time.Now()}
	return PeerDownloadResult{TransferID: id, SHA256: descriptor.SHA256, BytesTotal: descriptor.Bytes, ChunkBytes: PeerUploadChunkBytes}, nil
}

func (service *Service) ReadPeerDownload(request PeerDownloadChunkRequest) (PeerDownloadResult, error) {
	if request.Length < 1 || request.Length > PeerUploadChunkBytes || request.Offset < 0 {
		return PeerDownloadResult{}, errors.New("invalid artifact download range")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupPeerDownloadsLocked(time.Now())
	download := service.peerDownloads[request.TransferID]
	if download == nil || service.peerUploadsClosed {
		return PeerDownloadResult{}, os.ErrNotExist
	}
	if request.Offset > download.descriptor.Bytes {
		return PeerDownloadResult{}, errors.New("artifact offset exceeds size")
	}
	size := min(int64(request.Length), download.descriptor.Bytes-request.Offset)
	data := make([]byte, int(size))
	count, err := download.file.ReadAt(data, request.Offset)
	if err != nil && !(err == io.EOF && int64(count) == size) {
		return PeerDownloadResult{}, fmt.Errorf("read immutable artifact: %w", err)
	}
	if int64(count) != size {
		return PeerDownloadResult{}, io.ErrUnexpectedEOF
	}
	download.updated = time.Now()
	return PeerDownloadResult{TransferID: request.TransferID, SHA256: download.descriptor.SHA256, Offset: request.Offset,
		NextOffset: request.Offset + size, BytesTotal: download.descriptor.Bytes, ChunkBytes: PeerUploadChunkBytes, Data: data, EOF: request.Offset+size == download.descriptor.Bytes}, nil
}

func (service *Service) ClosePeerDownload(id string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	download := service.peerDownloads[id]
	if download == nil {
		return os.ErrNotExist
	}
	delete(service.peerDownloads, id)
	return download.file.Close()
}

func (service *Service) dispatchPeerDownload(method string, params json.RawMessage) (any, error) {
	switch method {
	case "controller.artifact.download.begin":
		var request PeerDownloadBeginRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, err
		}
		return service.BeginPeerDownload(request)
	case "controller.artifact.download.chunk":
		var request PeerDownloadChunkRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, err
		}
		return service.ReadPeerDownload(request)
	case "controller.artifact.download.close":
		var request PeerUploadFinishRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, err
		}
		err := service.ClosePeerDownload(request.TransferID)
		return map[string]bool{"closed": err == nil}, err
	}
	return nil, errors.New("unknown artifact download method")
}
