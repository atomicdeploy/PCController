package artifacts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPeerDownloadPinsVerifiedImageBoundsRangesAndClosesHandles(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PutFile(path, PutOptions{Kind: KindHostExecutable, Name: "fixture.exe"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	begin, err := service.BeginPeerDownload(PeerDownloadBeginRequest{Kind: KindHostExecutable, SHA256: artifact.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	if begin.BytesTotal != artifact.Bytes || begin.SHA256 != artifact.SHA256 || begin.ChunkBytes != PeerUploadChunkBytes {
		t.Fatal("invalid download identity")
	}
	chunk, err := service.ReadPeerDownload(PeerDownloadChunkRequest{TransferID: begin.TransferID, Offset: 0, Length: 32})
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := os.ReadFile(path)
	if !bytes.Equal(chunk.Data, expected[:32]) || chunk.NextOffset != 32 || chunk.EOF {
		t.Fatal("incorrect download bytes")
	}
	for _, request := range []PeerDownloadChunkRequest{
		{TransferID: begin.TransferID, Length: PeerUploadChunkBytes + 1},
		{TransferID: begin.TransferID, Offset: artifact.Bytes + 1, Length: 1},
		{TransferID: "unknown", Length: 1},
	} {
		if _, err := service.ReadPeerDownload(request); err == nil {
			t.Fatal("invalid download accepted")
		}
	}
	last, err := service.ReadPeerDownload(PeerDownloadChunkRequest{TransferID: begin.TransferID, Offset: artifact.Bytes, Length: 1})
	if err != nil || !last.EOF || len(last.Data) != 0 {
		t.Fatal("incorrect EOF", err)
	}
	service.mu.Lock()
	service.peerDownloads[begin.TransferID].updated = time.Now().Add(-peerUploadTTL - time.Second)
	service.mu.Unlock()
	if _, err := service.ReadPeerDownload(PeerDownloadChunkRequest{TransferID: begin.TransferID, Length: 1}); err == nil {
		t.Fatal("expired download accepted")
	}
	begin, err = service.BeginPeerDownload(PeerDownloadBeginRequest{Kind: KindHostExecutable, SHA256: artifact.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ClosePeerDownload(begin.TransferID); err != nil {
		t.Fatal(err)
	}
	if len(service.peerDownloads) != 0 {
		t.Fatal("download handle retained")
	}
}
