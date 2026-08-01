package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

// RFReplaceSupport describes whether the connected firmware can safely replace
// complete learned-remote records without using the legacy live EEPROM path.
type RFReplaceSupport struct {
	Known     bool
	Supported bool
	Reason    string
}

type rfReplaceTransport interface {
	Request(context.Context, byte, []byte, ...byte) (native.Frame, error)
	Command(context.Context, byte, []byte) error
}

// RFReplaceService owns capability discovery and verified snapshot replacement.
type RFReplaceService struct {
	transport    rfReplaceTransport
	capabilities func() (string, uint32, bool)

	mu       sync.Mutex
	probeKey string
	probeOK  bool
}

// NewRFReplaceService binds transactional RF management to the live runtime.
func NewRFReplaceService(runtime *Runtime) *RFReplaceService {
	return &RFReplaceService{
		transport: runtime,
		capabilities: func() (string, uint32, bool) {
			snapshot := runtime.Snapshot()
			hello := snapshot.Hello
			key := fmt.Sprintf("%s/%08X/%08X/%08X", hello.Name, hello.BuildHash, hello.BuildTimestamp, hello.Capabilities)
			return key, hello.Capabilities, snapshot.Connected && hello.IsPCController()
		},
	}
}

// Support reports capability-advertised or safely-probed support for this exact
// connected firmware identity.
func (service *RFReplaceService) Support() RFReplaceSupport {
	key, capabilities, connected := service.capabilities()
	if !connected {
		return RFReplaceSupport{Reason: "device is offline"}
	}
	if capabilities&native.CapabilityRFLearnReplace != 0 {
		return RFReplaceSupport{Known: true, Supported: true, Reason: "advertised by HELLO"}
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.probeKey != key {
		return RFReplaceSupport{Reason: "safe opcode probe required"}
	}
	if service.probeOK {
		return RFReplaceSupport{Known: true, Supported: true, Reason: "confirmed by safe opcode probe"}
	}
	return RFReplaceSupport{Known: true, Reason: "firmware rejected the optional opcode"}
}

// Probe sends an intentionally invalid zero-length request. Supporting firmware
// answers BadPayload; older firmware answers Unsupported. Neither response can
// modify EEPROM.
func (service *RFReplaceService) Probe(ctx context.Context) (RFReplaceSupport, error) {
	key, capabilities, connected := service.capabilities()
	if !connected {
		return RFReplaceSupport{Reason: "device is offline"}, errors.New("device is not connected")
	}
	if capabilities&native.CapabilityRFLearnReplace != 0 {
		return RFReplaceSupport{Known: true, Supported: true, Reason: "advertised by HELLO"}, nil
	}
	err := service.transport.Command(ctx, native.OpRFLearnReplace, nil)
	var remote *link.RemoteError
	if !errors.As(err, &remote) || remote.RequestOpcode != native.OpRFLearnReplace {
		if err == nil {
			err = errors.New("unsafe probe response: empty RF record was acknowledged")
		}
		return RFReplaceSupport{Reason: "safe opcode probe was inconclusive"}, err
	}
	service.mu.Lock()
	service.probeKey = key
	service.probeOK = remote.Code == native.ErrorBadPayload
	service.mu.Unlock()
	switch remote.Code {
	case native.ErrorBadPayload:
		return RFReplaceSupport{Known: true, Supported: true, Reason: "confirmed by safe opcode probe"}, nil
	case native.ErrorUnsupported:
		return RFReplaceSupport{Known: true, Reason: "firmware does not implement RF record replacement"}, nil
	default:
		return RFReplaceSupport{Reason: "safe opcode probe was inconclusive"}, err
	}
}

// Fetch returns the complete board-authoritative learned-remote snapshot.
func (service *RFReplaceService) Fetch(ctx context.Context) ([]native.RFEntry, error) {
	cursor := byte(0)
	entries := make([]native.RFEntry, 0, 20)
	for page := 0; page < 20; page++ {
		frame, err := service.transport.Request(ctx, native.OpRFLearnList, []byte{cursor}, native.OpRFEntries)
		if err != nil {
			return nil, err
		}
		parsed, err := native.ParseRFEntries(frame.Payload)
		if err != nil {
			return nil, err
		}
		entries = append(entries, parsed.Entries...)
		if parsed.NextCursor == 0xFF {
			sortRFRecords(entries)
			return entries, nil
		}
		if parsed.NextCursor <= cursor {
			return nil, fmt.Errorf("RF list cursor did not advance from %d", cursor)
		}
		cursor = parsed.NextCursor
	}
	return nil, errors.New("RF list exceeded pagination safety limit")
}

// Replace atomically from the user's perspective: it snapshots the board,
// validates a pure reorder, writes all records, verifies readback, and restores
// the original snapshot automatically after any failure or mismatch.
func (service *RFReplaceService) Replace(ctx context.Context, desired []native.RFEntry) error {
	service.mu.Lock()
	defer service.mu.Unlock()

	support := service.supportLocked()
	if !support.Supported {
		return fmt.Errorf("RF reorder unavailable: %s", support.Reason)
	}
	original, err := service.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("snapshot learned remotes: %w", err)
	}
	desired = append([]native.RFEntry(nil), desired...)
	sortRFRecords(desired)
	if err := validatePureRFReorder(original, desired); err != nil {
		return err
	}
	if err := service.writeSnapshot(ctx, original, desired); err == nil {
		readback, readErr := service.Fetch(ctx)
		if readErr == nil && equalRFRecords(readback, desired) {
			return nil
		}
		if readErr != nil {
			err = fmt.Errorf("read back learned remotes: %w", readErr)
		} else {
			err = errors.New("learned-remote readback differs from staged snapshot")
		}
		return service.rollback(ctx, desired, original, err)
	} else {
		return service.rollback(ctx, desired, original, err)
	}
}

func (service *RFReplaceService) supportLocked() RFReplaceSupport {
	key, capabilities, connected := service.capabilities()
	if !connected {
		return RFReplaceSupport{Reason: "device is offline"}
	}
	if capabilities&native.CapabilityRFLearnReplace != 0 {
		return RFReplaceSupport{Known: true, Supported: true, Reason: "advertised by HELLO"}
	}
	if service.probeKey == key && service.probeOK {
		return RFReplaceSupport{Known: true, Supported: true, Reason: "confirmed by safe opcode probe"}
	}
	return RFReplaceSupport{Reason: "safe opcode probe has not confirmed support"}
}

func (service *RFReplaceService) writeSnapshot(ctx context.Context, previous, next []native.RFEntry) error {
	for _, entry := range next {
		payload, err := native.RFReplacePayload(entry)
		if err != nil {
			return err
		}
		if err := service.transport.Command(ctx, native.OpRFLearnReplace, payload); err != nil {
			return fmt.Errorf("replace RF record %d: %w", entry.ID, err)
		}
	}
	nextIDs := make(map[byte]struct{}, len(next))
	for _, entry := range next {
		nextIDs[entry.ID] = struct{}{}
	}
	for _, entry := range previous {
		if _, retained := nextIDs[entry.ID]; retained {
			continue
		}
		if err := service.transport.Command(ctx, native.OpRFLearnRemove, []byte{entry.ID}); err != nil {
			return fmt.Errorf("remove stale RF record %d: %w", entry.ID, err)
		}
	}
	return nil
}

func (service *RFReplaceService) rollback(ctx context.Context, current, original []native.RFEntry, cause error) error {
	if rollbackErr := service.writeSnapshot(ctx, current, original); rollbackErr != nil {
		return fmt.Errorf("%v; automatic rollback failed: %w", cause, rollbackErr)
	}
	readback, rollbackErr := service.Fetch(ctx)
	if rollbackErr != nil {
		return fmt.Errorf("%v; automatic rollback readback failed: %w", cause, rollbackErr)
	}
	if !equalRFRecords(readback, original) {
		return fmt.Errorf("%v; automatic rollback readback differs from original snapshot", cause)
	}
	return fmt.Errorf("%w; original learned-remote snapshot restored", cause)
}

func validatePureRFReorder(original, desired []native.RFEntry) error {
	if len(original) != len(desired) {
		return fmt.Errorf("staged RF list has %d records; board snapshot has %d", len(desired), len(original))
	}
	originalByTuple := make(map[string]native.RFEntry, len(original))
	ids := make(map[byte]struct{}, len(desired))
	for _, entry := range original {
		key := rfStableTuple(entry)
		if _, duplicate := originalByTuple[key]; duplicate {
			return fmt.Errorf("board contains duplicate stable RF tuple %s", key)
		}
		originalByTuple[key] = entry
	}
	for _, entry := range desired {
		if _, duplicate := ids[entry.ID]; duplicate {
			return fmt.Errorf("staged RF ID %d is duplicated", entry.ID)
		}
		ids[entry.ID] = struct{}{}
		originalEntry, found := originalByTuple[rfStableTuple(entry)]
		if !found {
			return fmt.Errorf("staged RF record %s is not in the board snapshot", rfStableTuple(entry))
		}
		originalEntry.ID = entry.ID
		if originalEntry != entry {
			return fmt.Errorf("staged RF record %s changes data other than its ID", rfStableTuple(entry))
		}
		delete(originalByTuple, rfStableTuple(entry))
	}
	return nil
}

func rfStableTuple(entry native.RFEntry) string {
	return fmt.Sprintf("%08X/%d/%d", entry.Code, entry.Bits, entry.Protocol)
}

func sortRFRecords(entries []native.RFEntry) {
	sort.SliceStable(entries, func(left, right int) bool { return entries[left].ID < entries[right].ID })
}

func equalRFRecords(left, right []native.RFEntry) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]native.RFEntry(nil), left...)
	right = append([]native.RFEntry(nil), right...)
	sortRFRecords(left)
	sortRFRecords(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
