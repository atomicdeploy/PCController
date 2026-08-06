package artifacts

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	operationTimeout = 20 * time.Minute
	maxOperations    = 128
)

// Options supplies the immutable store, transfer adapters, execution owner,
// and live policy callbacks required by a Service.
type Options struct {
	Store                    *Store
	Downloader               *Downloader
	Executor                 Executor
	Events                   EventSink
	BoardIdentity            func() BoardIdentity
	RemoteProgrammingEnabled func() bool
}

// Service coordinates content-addressed artifacts and serialized update
// operations without opening hardware outside its injected Executor.
type Service struct {
	store         *Store
	downloader    *Downloader
	executor      Executor
	events        EventSink
	board         func() BoardIdentity
	remote        func() bool
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.RWMutex
	operations    map[string]UpdateStatus
	order         []string
	defaults      map[Kind]string
	idempotency   map[string]idempotencyRecord
	operationMeta map[string]operationJournal
	transaction   chan struct{}
}

// NewService validates its dependencies and restores durable operation state.
func NewService(options Options) (*Service, error) {
	if options.Store == nil {
		return nil, errors.New("artifact service requires a store")
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		store: options.Store, downloader: options.Downloader, executor: options.Executor,
		events: options.Events, board: options.BoardIdentity,
		remote: options.RemoteProgrammingEnabled, ctx: ctx, cancel: cancel,
		operations: make(map[string]UpdateStatus), defaults: make(map[Kind]string),
		idempotency: make(map[string]idempotencyRecord), operationMeta: make(map[string]operationJournal),
		transaction: make(chan struct{}, 1),
	}
	service.transaction <- struct{}{}
	if service.downloader == nil {
		service.downloader = NewDownloader(nil)
	}
	if err := service.loadOperationJournals(); err != nil {
		cancel()
		return nil, fmt.Errorf("load artifact operation journal: %w", err)
	}
	return service, nil
}

// Close cancels in-flight background operations owned by the service.
func (service *Service) Close() { service.cancel() }

// Store returns the immutable content-addressed store used by the service.
func (service *Service) Store() *Store { return service.store }

// SetDefault selects an existing firmware or EEPROM artifact as the bundled
// recovery candidate exposed through Manifest.
func (service *Service) SetDefault(kind Kind, digest string) error {
	if kind != KindFirmware && kind != KindEEPROM {
		return errors.New("only firmware and EEPROM artifacts can be defaults")
	}
	if _, err := service.store.Get(kind, digest); err != nil {
		return err
	}
	service.mu.Lock()
	service.defaults[kind] = strings.ToLower(strings.TrimSpace(digest))
	service.mu.Unlock()
	return nil
}

// Manifest returns current, default, board, policy, comparison, and latest
// operation state in one transport-neutral snapshot.
func (service *Service) Manifest() (Manifest, error) {
	currentFirmware, err := service.store.Current(KindFirmware)
	if err != nil {
		return Manifest{}, err
	}
	currentEEPROM, err := service.store.Current(KindEEPROM)
	if err != nil {
		return Manifest{}, err
	}
	currentReadback, err := service.store.Current(KindFlashBackup)
	if err != nil {
		return Manifest{}, err
	}
	currentHost, err := service.store.Current(KindHostExecutable)
	if err != nil {
		return Manifest{}, err
	}
	service.mu.RLock()
	defaultFirmwareHash := service.defaults[KindFirmware]
	defaultEEPROMHash := service.defaults[KindEEPROM]
	service.mu.RUnlock()
	defaultFirmware, err := service.optional(KindFirmware, defaultFirmwareHash)
	if err != nil {
		return Manifest{}, err
	}
	defaultEEPROM, err := service.optional(KindEEPROM, defaultEEPROMHash)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		Enabled: true, DefaultsEnabled: defaultFirmware != nil && defaultEEPROM != nil,
		Defaults: DefaultArtifacts{Firmware: defaultFirmware, EEPROM: defaultEEPROM},
		Current: CurrentArtifacts{
			Firmware: currentFirmware, EEPROM: currentEEPROM,
			FlashReadback: currentReadback, Host: currentHost,
		},
		Policy: Policy{ExplicitAuthorizationRequired: true},
	}
	if service.board != nil {
		manifest.Board = service.board()
	}
	if service.remote != nil {
		manifest.Policy.RemoteProgrammingEnabled = service.remote()
	}
	manifest.Comparison = compareDefaultFirmware(manifest.Board, defaultFirmware)
	if latest := service.LatestStatus(); latest != nil {
		manifest.Update = latest
	}
	decorateManifest(&manifest)
	return manifest, nil
}

func compareDefaultFirmware(board BoardIdentity, candidate *Descriptor) Comparison {
	result := Comparison{DefaultFirmware: "unknown"}
	if candidate != nil {
		result.CandidateHash = candidate.BuildHash
		result.CandidateTimestamp = candidate.BuildTimestamp
	}
	result.BoardBuildHash = board.BuildHash
	result.BoardTimestamp = board.BuildTimestamp
	if !board.Connected || candidate == nil {
		return result
	}
	if board.BuildHash != "" && strings.EqualFold(board.BuildHash, candidate.BuildHash) {
		result.DefaultFirmware = "same"
		return result
	}
	if len(board.BuildTimestamp) == 12 && len(candidate.BuildTimestamp) == 12 {
		switch strings.Compare(candidate.BuildTimestamp, board.BuildTimestamp) {
		case 1:
			result.DefaultFirmware = "newer"
		case -1:
			result.DefaultFirmware = "older"
		default:
			result.DefaultFirmware = "different"
		}
		return result
	}
	result.DefaultFirmware = "different"
	return result
}

// List returns stored artifacts, optionally restricted to one kind.
func (service *Service) List(kind *Kind) (List, error) {
	values, err := service.store.List(kind)
	if err != nil {
		return List{}, err
	}
	for index := range values {
		decorateDescriptor(&values[index])
	}
	return List{Artifacts: values}, nil
}

// Upload validates and imports content into the immutable artifact store.
func (service *Service) Upload(input io.Reader, options PutOptions) (Descriptor, error) {
	options.Source = firstNonEmpty(options.Source, "upload")
	descriptor, err := service.store.Put(input, options)
	if err != nil {
		return Descriptor{}, err
	}
	decorateDescriptor(&descriptor)
	service.emit("artifact.uploaded", "artifact uploaded and verified", map[string]string{
		"kind": string(descriptor.Kind), "sha256": descriptor.SHA256,
		"bytes": strconv.FormatInt(descriptor.Bytes, 10),
	})
	return descriptor, nil
}

// UploadOperation imports content and returns operation-shaped telemetry for
// clients that use the common update workflow contract.
func (service *Service) UploadOperation(input io.Reader, options PutOptions) (OperationResult, error) {
	descriptor, err := service.Upload(input, options)
	if err != nil {
		return OperationResult{}, err
	}
	status, _, err := service.reserveOperation(
		"artifact-upload", descriptor.SHA256, "artifact upload verified",
		"", "", ProgrammingMethodNone,
	)
	if err != nil {
		return OperationResult{}, err
	}
	service.updateBytes(status.ID, descriptor.Bytes, descriptor.Bytes)
	service.updateStatus(status.ID, "completed", 100, "artifact upload verified", "", "unknown")
	status, _ = service.Status(status.ID)
	return OperationResult{Operation: status, Artifact: &descriptor}, nil
}

// Open resolves a stored artifact and opens its verified local content stream.
func (service *Service) Open(kind Kind, digest string) (Descriptor, *os.File, error) {
	descriptor, file, err := service.store.Open(kind, digest)
	if err == nil {
		decorateDescriptor(&descriptor)
	}
	return descriptor, file, err
}

// StartFetch queues a remote download after validating its kind, integrity
// expectations, and idempotency identity.
func (service *Service) StartFetch(request FetchRequest) (OperationResult, error) {
	if _, err := ParseKind(string(request.Kind)); err != nil {
		return OperationResult{}, err
	}
	if strings.TrimSpace(request.URL) == "" {
		return OperationResult{}, errors.New("artifact URL is required")
	}
	fingerprintRequest := request
	fingerprintRequest.BearerToken = ""
	fingerprint, err := requestFingerprint(fingerprintRequest)
	if err != nil {
		return OperationResult{}, err
	}
	status, reused, err := service.reserveOperation(
		"artifact-fetch", request.SHA256, "queued remote artifact download",
		request.IdempotencyKey, fingerprint, ProgrammingMethodNone,
	)
	if err != nil {
		return OperationResult{}, err
	}
	if reused {
		return OperationResult{Operation: status, Reused: true}, nil
	}
	if request.Bytes > 0 {
		service.updateBytes(status.ID, 0, request.Bytes)
	}
	go service.run(status.ID, func(ctx context.Context, progress ProgressFunc) (string, error) {
		descriptor, err := service.downloader.Fetch(ctx, service.store, request, progress)
		if err != nil {
			return "", err
		}
		service.updateBytes(status.ID, descriptor.Bytes, descriptor.Bytes)
		return descriptor.SHA256, nil
	})
	return OperationResult{Operation: status}, nil
}

// StartCapture queues an explicitly authorized device flash and/or EEPROM
// readback through the primary hardware owner.
func (service *Service) StartCapture(request CaptureRequest) (OperationResult, error) {
	if !request.Authorized {
		return OperationResult{}, errors.New("explicit authorization is required before reading device memory")
	}
	if service.executor == nil {
		return OperationResult{}, errors.New("primary hardware executor is unavailable")
	}
	components, err := normalizeComponents(request.Components)
	if err != nil {
		return OperationResult{}, err
	}
	request.Components = components
	method, err := service.resolveProgrammingMethod(request.Method)
	if err != nil {
		return OperationResult{}, err
	}
	fingerprint, err := requestFingerprint(request)
	if err != nil {
		return OperationResult{}, err
	}
	status, reused, err := service.reserveOperation(
		"device-capture", "", "queued verified device readback",
		request.IdempotencyKey, fingerprint, method,
	)
	if err != nil {
		return OperationResult{}, err
	}
	if reused {
		return OperationResult{Operation: status, Reused: true}, nil
	}
	go service.runTransaction(status.ID, func(ctx context.Context, progress ProgressFunc) (string, error) {
		captured, err := service.executor.Capture(ctx, request, progress)
		if err != nil {
			return "", err
		}
		return service.importCaptured(captured, components)
	})
	return OperationResult{Operation: status}, nil
}

// StartFirmwareUpdate queues a guarded firmware programming transaction.
func (service *Service) StartFirmwareUpdate(request UpdateRequest) (OperationResult, error) {
	return service.startUpdate("firmware", request)
}

// StartFlashRestore restores an immutable captured-flash artifact through the
// primary host's guarded backup/write/verify/reconnect transaction. It is
// intentionally separate from firmware update so artifact kinds cannot drift.
func (service *Service) StartFlashRestore(request UpdateRequest) (OperationResult, error) {
	return service.startUpdate("flash-restore", request)
}

// StartEEPROMUpdate queues an explicitly authorized EEPROM programming transaction.
func (service *Service) StartEEPROMUpdate(request UpdateRequest) (OperationResult, error) {
	return service.startUpdate("eeprom", request)
}

// StartHostUpdate queues staging of a verified host executable artifact.
func (service *Service) StartHostUpdate(request UpdateRequest) (OperationResult, error) {
	return service.startUpdate("host", request)
}

func (service *Service) startUpdate(operationKind string, request UpdateRequest) (OperationResult, error) {
	operationLabel := "update"
	queuedDetail := "queued explicit update"
	if operationKind == "flash-restore" {
		operationLabel = "captured-flash restore"
		queuedDetail = "queued explicit captured-flash restore"
	}
	if !request.Authorized {
		return OperationResult{}, fmt.Errorf("explicit authorization is required before applying the %s", operationLabel)
	}
	if service.executor == nil {
		return OperationResult{}, errors.New("primary update executor is unavailable")
	}
	digest, err := normalizeSHA256(request.ArtifactSHA256)
	if err != nil {
		return OperationResult{}, err
	}
	request.ArtifactSHA256 = digest
	artifact, err := service.updateArtifact(operationKind, digest)
	if err != nil {
		return OperationResult{}, err
	}
	if operationKind == "host" && artifact.Platform != "" && artifact.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return OperationResult{}, fmt.Errorf("host artifact platform %q does not match %q", artifact.Platform, runtime.GOOS+"/"+runtime.GOARCH)
	}
	method := ProgrammingMethodNone
	if operationKind != "host" {
		method, err = service.resolveProgrammingMethod(request.Method)
		if err != nil {
			return OperationResult{}, err
		}
	}
	fingerprint, err := requestFingerprint(request)
	if err != nil {
		return OperationResult{}, err
	}
	status, reused, err := service.reserveOperation(
		operationKind, artifact.SHA256, queuedDetail,
		request.IdempotencyKey, fingerprint, method,
	)
	if err != nil {
		return OperationResult{}, err
	}
	if reused {
		decorateDescriptor(&artifact)
		return OperationResult{Operation: status, Artifact: &artifact, Reused: true}, nil
	}
	service.updateBytes(status.ID, 0, artifact.Bytes)
	go service.runTransaction(status.ID, func(ctx context.Context, progress ProgressFunc) (string, error) {
		var updateErr error
		switch operationKind {
		case "firmware":
			updateErr = service.executor.ProgramFirmware(ctx, artifact, request, progress)
			if updateErr == nil {
				updateErr = service.store.SetCurrent(KindFirmware, artifact.SHA256)
			}
		case "flash-restore":
			updateErr = service.executor.RestoreFlash(ctx, artifact, request, progress)
			if updateErr == nil {
				updateErr = service.store.SetCurrent(KindFlashBackup, artifact.SHA256)
			}
		case "eeprom":
			progress("backing-up", 20, "capturing flash and EEPROM before EEPROM restore")
			captured, captureErr := service.executor.Capture(ctx, CaptureRequest{
				Authorized: true, Components: []string{"flash", "eeprom"},
				Method: request.Method, Port: request.Port,
			}, progress)
			if captureErr == nil {
				_, captureErr = service.importCaptured(captured, []string{"flash", "eeprom"})
			}
			if captureErr != nil {
				return artifact.SHA256, fmt.Errorf("pre-EEPROM verified backup: %w", captureErr)
			}
			updateErr = service.executor.ProgramEEPROM(ctx, artifact, request, progress)
			if updateErr == nil {
				updateErr = service.store.SetCurrent(KindEEPROM, artifact.SHA256)
			}
		case "host":
			updateErr = service.executor.StageHostUpdate(ctx, artifact, request, progress)
		}
		if updateErr == nil {
			service.updateBytes(status.ID, artifact.Bytes, artifact.Bytes)
		}
		return artifact.SHA256, updateErr
	})
	decorateDescriptor(&artifact)
	return OperationResult{Operation: status, Artifact: &artifact}, nil
}

func (service *Service) importCaptured(captured []CapturedFile, components []string) (string, error) {
	imported := 0
	lastHash := ""
	for _, file := range captured {
		if !componentRequested(components, file.Kind) {
			continue
		}
		if descriptor, err := service.store.PutFile(file.Path, PutOptions{
			Kind: file.Kind, Name: file.Name, Source: "device-readback",
			BuildHash: file.BuildHash, BuildTimestamp: file.BuildTimestamp,
			PackedTimestamp: file.PackedTimestamp, Current: true,
			VerifiedReadback: true,
		}); err != nil {
			return "", err
		} else {
			lastHash = descriptor.SHA256
		}
		imported++
	}
	if imported == 0 {
		return "", errors.New("device capture returned no requested verified artifacts")
	}
	return lastHash, nil
}

// Status returns one operation by ID, or the latest operation when ID is empty.
func (service *Service) Status(id string) (UpdateStatus, error) {
	id = strings.TrimSpace(id)
	service.mu.RLock()
	defer service.mu.RUnlock()
	if id == "" && len(service.order) != 0 {
		id = service.order[len(service.order)-1]
	}
	status, ok := service.operations[id]
	if !ok {
		return UpdateStatus{}, os.ErrNotExist
	}
	return status, nil
}

// LatestStatus returns a copy of the newest operation, or nil when none exists.
func (service *Service) LatestStatus() *UpdateStatus {
	status, err := service.Status("")
	if err != nil {
		return nil
	}
	return &status
}

// DispatchRPC handles artifact and update methods and reports whether the
// method belonged to this service.
func (service *Service) DispatchRPC(ctx context.Context, method string, params json.RawMessage) (any, bool, error) {
	method = strings.ToLower(strings.TrimSpace(method))
	switch method {
	case "controller.artifact.manifest":
		value, err := service.Manifest()
		return value, true, err
	case "controller.artifact.list":
		var request struct {
			Kind string `json:"kind,omitempty"`
		}
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		var kind *Kind
		if strings.TrimSpace(request.Kind) != "" {
			parsed, err := ParseKind(request.Kind)
			if err != nil {
				return nil, true, err
			}
			kind = &parsed
		}
		value, err := service.List(kind)
		return value, true, err
	case "controller.artifact.fetch":
		var request FetchRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.StartFetch(request)
		return value, true, err
	case "controller.artifact.upload":
		var request UploadRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.UploadOperation(bytes.NewReader(request.Data), PutOptions{
			Kind: request.Kind, Name: request.Name, Source: "secondary-ipc",
			ExpectedSHA256: request.SHA256, ExpectedBytes: request.Bytes,
			BuildHash: request.BuildHash, BuildTimestamp: request.BuildTimestamp,
			Platform: request.Platform,
		})
		return value, true, err
	case "controller.artifact.capture":
		var request CaptureRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.StartCapture(request)
		return value, true, err
	case "controller.update.firmware":
		var request UpdateRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.StartFirmwareUpdate(request)
		return value, true, err
	case "controller.restore.flash":
		var request UpdateRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.StartFlashRestore(request)
		return value, true, err
	case "controller.update.eeprom":
		var request UpdateRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.StartEEPROMUpdate(request)
		return value, true, err
	case "controller.update.host":
		var request UpdateRequest
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.StartHostUpdate(request)
		return value, true, err
	case "controller.update.status":
		var request struct {
			ID string `json:"id,omitempty"`
		}
		if err := decodeRPCParams(params, &request); err != nil {
			return nil, true, err
		}
		value, err := service.Status(request.ID)
		return value, true, err
	default:
		return nil, false, nil
	}
}

func (service *Service) run(id string, operation func(context.Context, ProgressFunc) (string, error)) {
	ctx, cancel := context.WithTimeout(service.ctx, operationTimeout)
	defer cancel()
	progress := func(state string, percent int, detail string) {
		service.updateStatus(id, state, percent, detail, "", "")
	}
	digest, err := operation(ctx, progress)
	if err != nil {
		service.failOperation(id, err)
		return
	}
	service.mu.Lock()
	status := service.operations[id]
	if digest != "" {
		status.ArtifactSHA256 = digest
	}
	status.State = "completed"
	status.ProgressPercent = 100
	status.Detail = "operation completed"
	if status.ProgrammingMethod == ProgrammingMethodUrclock {
		status.BootloaderOutcome = BootloaderSucceeded
	}
	status.UpdatedAt = time.Now().UTC()
	if err := service.persistTerminalOperationLocked(id, status); err != nil {
		status.State = "failed"
		status.Detail = fmt.Sprintf("persist completed operation journal: %v", err)
		status.ErrorCode = "operation_journal_failed"
		status.UpdatedAt = time.Now().UTC()
		_ = service.persistTerminalOperationLocked(id, status)
	}
	service.operations[id] = status
	service.mu.Unlock()
	service.publishStatus(status)
}

// persistTerminalOperationLocked makes the terminal journal durable before the
// corresponding in-memory state becomes observable. The caller must hold mu.
func (service *Service) persistTerminalOperationLocked(id string, status UpdateStatus) error {
	journal := service.operationMeta[id]
	journal.Status = status
	return writeJSONAtomic(service.operationJournalPath(id), journal)
}

func (service *Service) runTransaction(id string, operation func(context.Context, ProgressFunc) (string, error)) {
	service.run(id, func(ctx context.Context, progress ProgressFunc) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-service.transaction:
		}
		defer func() { service.transaction <- struct{}{} }()
		return operation(ctx, progress)
	})
}

func (service *Service) updateStatus(id, state string, percent int, detail, errorCode, _ string) {
	service.mu.Lock()
	status, ok := service.operations[id]
	if !ok {
		service.mu.Unlock()
		return
	}
	if state != "" {
		status.State = state
	}
	if percent >= 0 {
		if percent > 100 {
			percent = 100
		}
		status.ProgressPercent = percent
	}
	if detail != "" {
		status.Detail = detail
	}
	status.ErrorCode = errorCode
	status.UpdatedAt = time.Now().UTC()
	service.operations[id] = status
	service.mu.Unlock()
	_ = service.persistOperation(id)
	service.publishStatus(status)
}

func (service *Service) updateBytes(id string, done, total int64) {
	service.mu.Lock()
	status, ok := service.operations[id]
	if ok {
		status.BytesDone = done
		status.BytesTotal = total
		status.UpdatedAt = time.Now().UTC()
		service.operations[id] = status
	}
	service.mu.Unlock()
	_ = service.persistOperation(id)
}

func (service *Service) publishStatus(status UpdateStatus) {
	metadata := map[string]string{
		"operation_id": status.ID, "kind": status.Kind, "state": status.State,
		"progress_percent": strconv.Itoa(status.ProgressPercent),
	}
	if status.ArtifactSHA256 != "" {
		metadata["sha256"] = status.ArtifactSHA256
	}
	if status.ErrorCode != "" {
		metadata["error_code"] = status.ErrorCode
	}
	if status.ProgrammingMethod != "" {
		metadata["programming_method"] = string(status.ProgrammingMethod)
	}
	if status.BootloaderOutcome != "" {
		metadata["bootloader_outcome"] = string(status.BootloaderOutcome)
	}
	if status.ISPFallbackSuggested {
		metadata["isp_fallback_suggested"] = "true"
	}
	service.emit("update."+status.State, status.Detail, metadata)
}

func (service *Service) emit(kind, text string, metadata map[string]string) {
	if service.events != nil {
		service.events(kind, text, metadata)
	}
}

func (service *Service) updateArtifact(kind, digest string) (Descriptor, error) {
	switch kind {
	case "firmware":
		return service.store.Get(KindFirmware, digest)
	case "flash-restore":
		return service.store.Get(KindFlashBackup, digest)
	case "eeprom":
		return service.store.Get(KindEEPROM, digest)
	case "host":
		return service.store.Get(KindHostExecutable, digest)
	default:
		return Descriptor{}, errors.New("unknown update kind")
	}
}

func (service *Service) optional(kind Kind, digest string) (*Descriptor, error) {
	if digest == "" {
		return nil, nil
	}
	descriptor, err := service.store.Get(kind, digest)
	if err != nil {
		return nil, err
	}
	value := publicDescriptor(descriptor)
	decorateDescriptor(&value)
	return &value, nil
}

func decorateDescriptor(value *Descriptor) {
	if value == nil {
		return
	}
	value.DownloadURL = "/api/artifacts/" + string(value.Kind) + "/" + value.SHA256
}

func decorateManifest(value *Manifest) {
	for _, descriptor := range []*Descriptor{
		value.Defaults.Firmware, value.Defaults.EEPROM, value.Current.Firmware,
		value.Current.EEPROM, value.Current.FlashReadback, value.Current.Host,
	} {
		decorateDescriptor(descriptor)
	}
}

func normalizeComponents(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"flash", "eeprom"}, nil
	}
	result := make([]string, 0, 2)
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "flash" && value != "eeprom" {
			return nil, fmt.Errorf("unsupported capture component %q", value)
		}
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result, nil
}

func componentRequested(components []string, kind Kind) bool {
	want := "eeprom"
	if kind == KindFlashBackup {
		want = "flash"
	}
	for _, component := range components {
		if component == want {
			return true
		}
	}
	return false
}

func (service *Service) failOperation(id string, err error) {
	service.mu.Lock()
	status, ok := service.operations[id]
	if !ok {
		service.mu.Unlock()
		return
	}
	status.State = "failed"
	status.Detail = err.Error()
	status.ErrorCode = "operation_failed"
	var failure *ExecutionFailure
	switch {
	case errors.As(err, &failure):
		if failure.Code != "" {
			status.ErrorCode = failure.Code
		}
		if failure.Method != "" {
			status.ProgrammingMethod = failure.Method
		}
		if failure.BootloaderOutcome != "" {
			status.BootloaderOutcome = failure.BootloaderOutcome
		}
		status.ISPFallbackSuggested = failure.ISPFallbackSuggested
	case errors.Is(err, context.DeadlineExceeded):
		status.ErrorCode = "deadline_exceeded"
		if status.ProgrammingMethod == ProgrammingMethodUrclock {
			status.BootloaderOutcome = BootloaderTimedOut
			status.ISPFallbackSuggested = true
			status.Detail += "; UART bootloader timed out, so ISP method usbasp is available as recovery"
		}
	case errors.Is(err, context.Canceled):
		status.ErrorCode = "cancelled"
	case strings.Contains(strings.ToLower(status.Detail), "sha-256") ||
		strings.Contains(strings.ToLower(status.Detail), "checksum"):
		status.ErrorCode = "integrity_check_failed"
	default:
		if status.ProgrammingMethod == ProgrammingMethodUrclock {
			status.BootloaderOutcome = BootloaderFailed
		}
	}
	status.UpdatedAt = time.Now().UTC()
	if persistErr := service.persistTerminalOperationLocked(id, status); persistErr != nil {
		status.Detail = fmt.Sprintf("%s; persist failed operation journal: %v", status.Detail, persistErr)
		status.ErrorCode = "operation_journal_failed"
		status.UpdatedAt = time.Now().UTC()
		_ = service.persistTerminalOperationLocked(id, status)
	}
	service.operations[id] = status
	service.mu.Unlock()
	service.publishStatus(status)
}

type programmingMethodResolver interface {
	ResolveProgrammingMethod(string) (ProgrammingMethod, error)
}

func (service *Service) resolveProgrammingMethod(value string) (ProgrammingMethod, error) {
	if resolver, ok := service.executor.(programmingMethodResolver); ok {
		return resolver.ResolveProgrammingMethod(value)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ProgrammingMethodUrclock):
		return ProgrammingMethodUrclock, nil
	case string(ProgrammingMethodUSBasp):
		return ProgrammingMethodUSBasp, nil
	default:
		return "", fmt.Errorf("unsupported programming method %q", value)
	}
}

func decodeRPCParams(raw json.RawMessage, destination any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("params contain trailing JSON")
		}
		return err
	}
	return nil
}

func newOperationID() string {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("op-%d", time.Now().UnixNano())
	}
	return "op-" + hex.EncodeToString(value)
}
