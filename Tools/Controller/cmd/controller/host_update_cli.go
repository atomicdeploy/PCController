package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/hostui"
)

const maximumHostUpdateBytes = 256 << 20

var hostUpdatePollInterval = 500 * time.Millisecond
var hostUpdateStableVerificationWindow = 12 * time.Second

// runHostUpdate routes a candidate through the already-running primary's
// bounded artifact transport. The primary owns activation, shutdown, restart,
// and the final content-addressed acknowledgement; this process never replaces
// a live installation directory itself.
func runHostUpdate(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || !strings.EqualFold(args[0], "host") {
		return errors.New("usage: controller update host FILE [--expected-sha256 SHA256] [--idempotency-key KEY]")
	}
	flags := flag.NewFlagSet("update host", flag.ContinueOnError)
	flags.SetOutput(stderr)
	expected := flags.String("expected-sha256", "", "expected candidate executable SHA-256")
	idempotencyKey := flags.String("idempotency-key", "", "stable retry key for this update intent")
	parseArgs := append([]string(nil), args[1:]...)
	// The public syntax puts FILE first. Go's flag package stops at the first
	// positional argument, so move that one value behind its flags while also
	// continuing to accept conventional flags-first invocations.
	if len(parseArgs) > 1 && !strings.HasPrefix(parseArgs[0], "-") {
		candidate := parseArgs[0]
		parseArgs = append(parseArgs[1:], candidate)
	}
	if err := flags.Parse(parseArgs); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: controller update host FILE [--expected-sha256 SHA256] [--idempotency-key KEY]")
	}
	candidate := flags.Arg(0)
	// Validate the documented FILE argument before looking up a running host.
	// Besides producing the useful local error first, this keeps the command's
	// parsing contract deterministic on clean machines that have no host record.
	file, err := os.Open(candidate)
	if err != nil {
		return fmt.Errorf("open host update candidate: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close host update candidate: %w", err)
	}
	ctx, cancel := lifecycleCommandContext()
	defer cancel()
	paths, err := defaultHostInstancePaths()
	if err != nil {
		return err
	}
	if err := selectHostUpdatePrimary(ctx, paths); err != nil {
		return err
	}
	return delegatePrimaryHostUpdate(ctx, candidate, *expected, *idempotencyKey, stdout, callPrimary)
}

// A running host may override its persisted listen address. Authenticate its
// published identity before sending an update; never fall back to a stale port.
func selectHostUpdatePrimary(ctx context.Context, paths hostInstancePaths) error {
	record, err := readHostInstanceRecord(paths.RecordPath)
	if err != nil {
		return fmt.Errorf("locate running host for update: %w", err)
	}
	if err := verifyHostInstanceRecord(ctx, record); err != nil {
		return fmt.Errorf("verify running host for update: %w", err)
	}
	primaryEndpoint.Store(recordPrimaryEndpoint(record))
	return nil
}

func delegatePrimaryHostUpdate(
	ctx context.Context,
	path, expected, idempotencyKey string,
	output io.Writer,
	call primaryCallFunc,
) error {
	if call == nil {
		return errors.New("host update requires a primary RPC client")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open host update candidate: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect host update candidate: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximumHostUpdateBytes {
		return fmt.Errorf("host update candidate must be a 1..%d byte regular file", maximumHostUpdateBytes)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash host update candidate: %w", err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if value := strings.ToLower(strings.TrimSpace(expected)); value != "" && value != digest {
		return fmt.Errorf("host update candidate SHA-256 is %s, expected %s", digest, value)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind host update candidate: %w", err)
	}
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		key = "local-host:" + digest
	}
	if output != nil {
		fmt.Fprintf(output, "sending host update %s (%d bytes) to the running primary\n", digest, info.Size())
	}
	beginRequest := artifacts.PeerUploadBeginRequest{
		Kind: artifacts.KindHostExecutable, Name: filepath.Base(path),
		SHA256: digest, Bytes: info.Size(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
	var begin artifacts.PeerUploadBeginResult
	if err := call(ctx, "controller.artifact.upload.begin", beginRequest, &begin); err != nil {
		return fmt.Errorf("begin primary host transfer: %w", err)
	}
	if strings.TrimSpace(begin.TransferID) == "" || begin.ChunkBytes < 1 || begin.ChunkBytes > artifacts.PeerUploadChunkBytes {
		return errors.New("primary returned an invalid host transfer contract")
	}
	finished := false
	defer func() {
		if !finished {
			abortContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = call(abortContext, "controller.artifact.upload.abort", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, nil)
		}
	}()
	buffer := make([]byte, begin.ChunkBytes)
	offset := int64(0)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			var chunk artifacts.PeerUploadChunkResult
			err := call(ctx, "controller.artifact.upload.chunk", artifacts.PeerUploadChunkRequest{
				TransferID: begin.TransferID, Offset: offset, Data: buffer[:count],
			}, &chunk)
			if err != nil {
				return fmt.Errorf("send primary host update at byte %d: %w", offset, err)
			}
			offset += int64(count)
			if chunk.NextOffset != offset || chunk.BytesTotal != info.Size() {
				return errors.New("primary returned an invalid host transfer acknowledgement")
			}
			if output != nil {
				fmt.Fprintf(output, "transmitted %s\n", hostui.FormatByteProgress(offset, info.Size()))
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read host update candidate: %w", readErr)
		}
	}
	var uploaded artifacts.OperationResult
	if err := call(ctx, "controller.artifact.upload.finish", artifacts.PeerUploadFinishRequest{TransferID: begin.TransferID}, &uploaded); err != nil {
		return fmt.Errorf("finish primary host transfer: %w", err)
	}
	finished = true
	if uploaded.Artifact == nil || uploaded.Artifact.Kind != artifacts.KindHostExecutable ||
		!strings.EqualFold(uploaded.Artifact.SHA256, digest) {
		return errors.New("primary did not acknowledge the exact uploaded host executable")
	}
	if output != nil {
		fmt.Fprintln(output, "primary verified the candidate; scheduling restart")
	}
	var operation artifacts.OperationResult
	if err := call(ctx, "controller.update.host", artifacts.UpdateRequest{
		ArtifactSHA256: digest, Authorized: true, IdempotencyKey: key,
	}, &operation); err != nil {
		return fmt.Errorf("start primary host update: %w", err)
	}
	if operation.Operation.ID == "" {
		return errors.New("primary returned a host update without an operation ID")
	}
	return waitForPrimaryHostDigest(ctx, call, operation.Operation.ID, digest, key, output)
}

func waitForPrimaryHostDigest(
	ctx context.Context,
	call primaryCallFunc,
	operationID, digest, idempotencyKey string,
	output io.Writer,
) error {
	ticker := time.NewTicker(hostUpdatePollInterval)
	defer ticker.Stop()
	var candidateObservedAt time.Time
	for {
		var manifest artifacts.Manifest
		if err := call(ctx, "controller.artifact.manifest", map[string]any{}, &manifest); err == nil &&
			manifest.Current.Host != nil && strings.EqualFold(manifest.Current.Host.SHA256, digest) {
			if candidateObservedAt.IsZero() {
				candidateObservedAt = time.Now()
				if output != nil && hostUpdateStableVerificationWindow > 0 {
					fmt.Fprintln(output, "candidate is running; waiting for the self-update health commit")
				}
			}
			if hostUpdateStableVerificationWindow <= 0 || time.Since(candidateObservedAt) >= hostUpdateStableVerificationWindow {
				if output != nil {
					fmt.Fprintf(output, "host update acknowledged after restart: operation=%s sha256=%s terminal_verified=true\n", operationID, digest)
				}
				return nil
			}
		} else {
			candidateObservedAt = time.Time{}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("host update outcome is uncertain: %w; idempotency_key=%s", ctx.Err(), idempotencyKey)
		case <-ticker.C:
		}
	}
}
