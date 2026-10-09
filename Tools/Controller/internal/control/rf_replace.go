package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

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

	mu sync.Mutex
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

// Support reports the current firmware's advertised full-record capability.
func (service *RFReplaceService) Support() RFReplaceSupport {
	_, capabilities, connected := service.capabilities()
	if !connected {
		return RFReplaceSupport{Known: true, Reason: "device is offline"}
	}
	if capabilities&native.CapabilityRFLearnReplace != 0 {
		return RFReplaceSupport{Known: true, Supported: true, Reason: "advertised by HELLO"}
	}
	return RFReplaceSupport{Known: true, Reason: "firmware does not advertise full-record RF replacement"}
}

// Probe is a read-only capability check retained for the TUI action surface; it
// never sends an invalid replacement request to current firmware.
func (service *RFReplaceService) Probe(ctx context.Context) (RFReplaceSupport, error) {
	if err := ctx.Err(); err != nil {
		return RFReplaceSupport{}, err
	}
	support := service.Support()
	if !support.Supported {
		return support, errors.New(support.Reason)
	}
	return support, nil
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
// validates every complete record, writes the desired snapshot, verifies
// readback, and restores the original snapshot after any failure or mismatch.
func (service *RFReplaceService) Replace(ctx context.Context, desired []native.RFEntry) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.replaceLocked(ctx, desired)
}

// UpdateMapping changes one learned record through the same verified full-list
// transaction used by reorder/import. Opcode 0x26 is intentionally never used.
func (service *RFReplaceService) UpdateMapping(
	ctx context.Context,
	id, actionKind, actionValue, behavior byte,
) error {
	if _, err := native.RFMappingPayload(id, actionKind, actionValue, behavior); err != nil {
		return err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if support := service.supportLocked(); !support.Supported {
		return fmt.Errorf("RF full-record update unavailable: %s", support.Reason)
	}
	original, err := service.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("snapshot learned remotes: %w", err)
	}
	desired := append([]native.RFEntry(nil), original...)
	found := false
	for index := range desired {
		if desired[index].ID != id {
			continue
		}
		desired[index].ActionKind = actionKind
		desired[index].ActionValue = actionValue
		desired[index].Behavior = behavior
		found = true
		break
	}
	if !found {
		return fmt.Errorf("learned RF entry %d does not exist", id)
	}
	return service.replaceSnapshotLocked(ctx, original, desired)
}

func (service *RFReplaceService) replaceLocked(
	ctx context.Context,
	desired []native.RFEntry,
) error {

	support := service.supportLocked()
	if !support.Supported {
		return fmt.Errorf("RF full-record update unavailable: %s", support.Reason)
	}
	original, err := service.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("snapshot learned remotes: %w", err)
	}
	return service.replaceSnapshotLocked(ctx, original, desired)
}

func (service *RFReplaceService) replaceSnapshotLocked(
	ctx context.Context,
	original, desired []native.RFEntry,
) error {
	desired = append([]native.RFEntry(nil), desired...)
	sortRFRecords(desired)
	if err := validateRFRecords(desired); err != nil {
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
	_, capabilities, connected := service.capabilities()
	if !connected {
		return RFReplaceSupport{Reason: "device is offline"}
	}
	if capabilities&native.CapabilityRFLearnReplace != 0 {
		return RFReplaceSupport{Known: true, Supported: true, Reason: "advertised by HELLO"}
	}
	return RFReplaceSupport{Known: true, Reason: "firmware does not advertise full-record RF replacement"}
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

func validateRFRecords(desired []native.RFEntry) error {
	if len(desired) > 20 {
		return fmt.Errorf("staged RF list has %d records; capacity is 20", len(desired))
	}
	ids := make(map[byte]struct{}, len(desired))
	tuples := make(map[string]struct{}, len(desired))
	for _, entry := range desired {
		if _, err := native.RFReplacePayload(entry); err != nil {
			return err
		}
		if _, duplicate := ids[entry.ID]; duplicate {
			return fmt.Errorf("staged RF ID %d is duplicated", entry.ID)
		}
		ids[entry.ID] = struct{}{}
		tuple := rfStableTuple(entry)
		if _, duplicate := tuples[tuple]; duplicate {
			return fmt.Errorf("staged RF code tuple %s is duplicated", tuple)
		}
		tuples[tuple] = struct{}{}
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
