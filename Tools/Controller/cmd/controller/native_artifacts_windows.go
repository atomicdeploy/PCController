//go:build windows && !no_native_gui

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/desktop/desktop"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func (p nativePages) loadNativeArtifacts(ctx context.Context, state control.Snapshot) (desktop.Page, error) {
	page := desktop.Page{Title: "Artifact transfers & updates", Columns: []string{"SHA-256", "Name", "Kind", "Bytes"}, RowValues: map[string]map[string]string{}}
	var inventory artifacts.List
	var manifest artifacts.Manifest
	if !p.preview {
		if err := p.call(ctx, "controller.artifact.list", map[string]any{}, &inventory); err != nil {
			return page, err
		}
		if err := p.call(ctx, "controller.artifact.manifest", map[string]any{}, &manifest); err != nil {
			return page, err
		}
	}
	for _, item := range inventory.Artifacts {
		identity := string(item.Kind) + ":" + item.SHA256
		page.Rows = append(page.Rows, []string{identity, item.Name, string(item.Kind), fmt.Sprint(item.Bytes)})
		page.RowValues[identity] = map[string]string{"selected_kind": string(item.Kind), "selected_sha256": item.SHA256}
	}
	page.Fields = []desktop.Field{
		{Key: "selected_kind", Hidden: true},
		{Key: "selected_sha256", Hidden: true},
		{Key: "path", Label: "Local file", FileFilter: "Artifacts (*.hex;*.bin;*.exe)|*.hex;*.bin;*.exe|All files (*.*)|*.*", Section: "Import"},
		choice("kind", "Artifact kind", "firmware", "firmware", "eeprom", "flash-backup", "host-executable"), field("sha256", "Expected SHA-256 (optional)", ""), field("url", "Download URL", ""),
		choice("method", "Programming method", "urclock", "urclock", "usbasp"), choice("components", "Back up", "flash + eeprom", "flash", "eeprom", "flash + eeprom"),
		{Key: "export_path", Label: "Save selected artifact as", FileFilter: "Artifacts (*.hex;*.bin;*.exe)|*.hex;*.bin;*.exe|All files (*.*)|*.*", SaveFile: true, Section: "Export"},
	}
	for i := range page.Fields {
		switch page.Fields[i].Key {
		case "kind", "sha256":
			page.Fields[i].Section = "Transfers"
		case "path":
			page.Fields[i].Section = "Transfers"
		case "url":
			page.Fields[i].Section = "Transfers"
		case "method", "components":
			page.Fields[i].Section = "Board recovery"
		}
	}
	ready := !p.preview && manifest.Enabled && !state.Programming.Active && !state.EmergencyStop.Active
	page.Actions = []desktop.PageAction{{ID: "import", Label: "Import local file", Section: "Transfers", Enabled: !p.preview, TimeoutSeconds: 120}, {ID: "fetch", Label: "Download to host store", Section: "Transfers", Enabled: !p.preview}, {ID: "apply", Label: "Apply selected artifact", Section: "Board recovery", Enabled: ready, SelectionRequired: true, Confirm: true}, {ID: "capture", Label: "Back up board", Section: "Board recovery", Enabled: ready && state.Connected, Confirm: true}, {ID: "export", Label: "Save selected artifact", Enabled: !p.preview, Section: "Export", SelectionRequired: true, TimeoutSeconds: 300}}
	if manifest.Update != nil {
		encoded, _ := json.MarshalIndent(manifest.Update, "", "  ")
		page.Output = string(encoded)
	}
	return page, nil
}

func (p nativePages) submitNativeArtifacts(ctx context.Context, action string, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot transfer artifacts")
	}
	var result any
	switch action {
	case "export":
		selected, err := p.selectedNativeArtifact(ctx, values)
		if err != nil {
			return desktop.Page{}, err
		}
		if err := p.downloadNativeArtifact(ctx, selected, values["export_path"]); err != nil {
			return desktop.Page{}, err
		}
		result = map[string]any{"saved": values["export_path"], "sha256": selected.SHA256, "bytes": selected.Bytes}
	case "import":
		kind, err := artifacts.ParseKind(values["kind"])
		if err != nil {
			return desktop.Page{}, err
		}
		artifact, err := p.uploadNativeArtifact(ctx, values["path"], kind, values["sha256"])
		if err != nil {
			return desktop.Page{}, err
		}
		result = artifact
	case "fetch":
		if strings.TrimSpace(values["url"]) == "" {
			return desktop.Page{}, errors.New("enter an artifact URL")
		}
		if err := p.call(ctx, "controller.artifact.fetch", artifacts.FetchRequest{URL: values["url"], Kind: artifacts.Kind(values["kind"]), SHA256: values["sha256"]}, &result); err != nil {
			return desktop.Page{}, err
		}
	case "capture":
		components := []string{values["components"]}
		if values["components"] == "flash + eeprom" {
			components = []string{"flash", "eeprom"}
		}
		if err := p.call(ctx, "controller.artifact.capture", artifacts.CaptureRequest{Components: components, Authorized: true, Method: values["method"]}, &result); err != nil {
			return desktop.Page{}, err
		}
	case "apply":
		selected, err := p.selectedNativeArtifact(ctx, values)
		if err != nil {
			return desktop.Page{}, err
		}
		method, ok := map[artifacts.Kind]string{artifacts.KindFirmware: "controller.update.firmware", artifacts.KindEEPROM: "controller.update.eeprom", artifacts.KindFlashBackup: "controller.restore.flash", artifacts.KindHostExecutable: "controller.update.host"}[selected.Kind]
		if !ok {
			return desktop.Page{}, errors.New("unsupported update kind")
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return desktop.Page{}, err
		}
		if err := p.call(ctx, method, artifacts.UpdateRequest{ArtifactSHA256: selected.SHA256, Authorized: true, Method: values["method"], IdempotencyKey: "desktop:" + hex.EncodeToString(nonce[:])}, &result); err != nil {
			return desktop.Page{}, err
		}
	default:
		return desktop.Page{}, errors.New("unknown artifact operation")
	}
	var state control.Snapshot
	if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
		return desktop.Page{}, fmt.Errorf("operation acknowledged; connection is recovering; do not repeat the update: %w", err)
	}
	page, err := p.loadNativeArtifacts(ctx, state)
	if err != nil {
		return page, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	page.Output = string(encoded)
	return page, nil
}

func (p nativePages) selectedNativeArtifact(ctx context.Context, values map[string]string) (artifacts.Descriptor, error) {
	var inventory artifacts.List
	if err := p.call(ctx, "controller.artifact.list", map[string]any{}, &inventory); err != nil {
		return artifacts.Descriptor{}, err
	}
	for _, item := range inventory.Artifacts {
		if values["selected"] == string(item.Kind)+":"+item.SHA256 && values["selected_sha256"] == item.SHA256 && values["selected_kind"] == string(item.Kind) {
			return item, nil
		}
	}
	return artifacts.Descriptor{}, errors.New("select a current stored artifact first")
}

func (p nativePages) downloadNativeArtifact(ctx context.Context, artifact artifacts.Descriptor, path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("choose an absolute export filename")
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("export destination already exists; choose a new filename")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	limit := int64(2 << 20)
	if artifact.Kind == artifacts.KindHostExecutable {
		limit = maximumHostUpdateBytes
	}
	if _, err := artifacts.ParseKind(string(artifact.Kind)); err != nil {
		return err
	}
	digest, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(digest) != sha256.Size || artifact.Bytes < 1 || artifact.Bytes > limit {
		return errors.New("invalid artifact identity or size")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pccontroller-export-*")
	if err != nil {
		return err
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	var begin artifacts.PeerDownloadResult
	if err := p.call(ctx, "controller.artifact.download.begin", artifacts.PeerDownloadBeginRequest{Kind: artifact.Kind, SHA256: artifact.SHA256}, &begin); err != nil {
		return err
	}
	if begin.TransferID != "" {
		defer func() {
			cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			_ = p.call(cleanup, "controller.artifact.download.close", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, nil)
		}()
	}
	if begin.TransferID == "" || begin.SHA256 != artifact.SHA256 || begin.BytesTotal != artifact.Bytes || begin.Offset != 0 || begin.NextOffset != 0 || len(begin.Data) != 0 || begin.ChunkBytes < 1 || begin.ChunkBytes > artifacts.PeerUploadChunkBytes {
		return errors.New("invalid artifact download contract")
	}
	hash := sha256.New()
	for offset := int64(0); offset < artifact.Bytes; {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := int(min(int64(begin.ChunkBytes), artifact.Bytes-offset))
		var chunk artifacts.PeerDownloadResult
		if err := p.call(ctx, "controller.artifact.download.chunk", artifacts.PeerDownloadChunkRequest{TransferID: begin.TransferID, Offset: offset, Length: length}, &chunk); err != nil {
			return err
		}
		next := offset + int64(length)
		if chunk.TransferID != begin.TransferID || chunk.SHA256 != artifact.SHA256 || chunk.BytesTotal != artifact.Bytes || chunk.Offset != offset || chunk.NextOffset != next || len(chunk.Data) != length || chunk.EOF != (next == artifact.Bytes) {
			return errors.New("artifact download acknowledgement mismatch")
		}
		if _, err := file.Write(chunk.Data); err != nil {
			return err
		}
		hash.Write(chunk.Data)
		offset = next
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), artifact.SHA256) {
		return errors.New("downloaded artifact SHA-256 mismatch")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(file.Name())
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// No REPLACE_EXISTING flag: an intervening file is never overwritten.
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func (p nativePages) uploadNativeArtifact(ctx context.Context, path string, kind artifacts.Kind, expected string) (artifacts.Descriptor, error) {
	file, err := os.Open(path)
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return artifacts.Descriptor{}, err
	}
	limit := int64(2 << 20)
	if kind == artifacts.KindHostExecutable {
		limit = maximumHostUpdateBytes
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return artifacts.Descriptor{}, fmt.Errorf("artifact must be a 1..%d byte regular file", limit)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return artifacts.Descriptor{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if expected = strings.TrimSpace(expected); expected != "" && !strings.EqualFold(expected, digest) {
		return artifacts.Descriptor{}, errors.New("local artifact SHA-256 does not match")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return artifacts.Descriptor{}, err
	}
	var begin artifacts.PeerUploadBeginResult
	request := artifacts.PeerUploadBeginRequest{Kind: kind, Name: filepath.Base(path), SHA256: digest, Bytes: info.Size()}
	if kind == artifacts.KindHostExecutable {
		request.Platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	if err := p.call(ctx, "controller.artifact.upload.begin", request, &begin); err != nil {
		return artifacts.Descriptor{}, err
	}
	finished := false
	defer func() {
		if !finished && begin.TransferID != "" {
			abort, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = p.call(abort, "controller.artifact.upload.abort", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, nil)
		}
	}()
	if begin.TransferID == "" || begin.NextOffset != 0 || begin.ChunkBytes < 1 || begin.ChunkBytes > artifacts.PeerUploadChunkBytes {
		return artifacts.Descriptor{}, errors.New("invalid artifact transfer contract")
	}
	buffer := make([]byte, begin.ChunkBytes)
	offset := int64(0)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			var ack artifacts.PeerUploadChunkResult
			if err := p.call(ctx, "controller.artifact.upload.chunk", artifacts.PeerUploadChunkRequest{TransferID: begin.TransferID, Offset: offset, Data: buffer[:count]}, &ack); err != nil {
				return artifacts.Descriptor{}, err
			}
			offset += int64(count)
			if ack.TransferID != begin.TransferID || ack.NextOffset != offset || ack.BytesTotal != info.Size() {
				return artifacts.Descriptor{}, errors.New("invalid artifact chunk acknowledgement")
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return artifacts.Descriptor{}, readErr
		}
	}
	if offset != info.Size() {
		return artifacts.Descriptor{}, errors.New("local artifact changed during transfer")
	}
	var result artifacts.OperationResult
	if err := p.call(ctx, "controller.artifact.upload.finish", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, &result); err != nil {
		return artifacts.Descriptor{}, err
	}
	if result.Artifact == nil || result.Artifact.SHA256 != digest || result.Artifact.Kind != kind || result.Artifact.Bytes != offset {
		return artifacts.Descriptor{}, errors.New("host did not acknowledge the exact artifact")
	}
	finished = true
	return *result.Artifact, nil
}
