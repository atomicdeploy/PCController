package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"pccontroller.local/controller/internal/native"
)

var ErrBoardAutomationsUnsupported = errors.New("connected firmware does not advertise board-owned automations")

type BoardAutomationSnapshot struct {
	Generation uint16                    `json:"generation"`
	Records    []native.AutomationRecord `json:"records"`
}

type boardAutomationTransport interface {
	Request(context.Context, byte, []byte, ...byte) (native.Frame, error)
}

// BoardAutomationService serializes mutations and verifies their exact
// board-authoritative readback. Pagination restarts if a concurrent generation
// change would otherwise produce a mixed snapshot.
type BoardAutomationService struct {
	transport boardAutomationTransport
	support   func() bool
	mu        sync.Mutex
}

func NewBoardAutomationService(runtime *Runtime) *BoardAutomationService {
	return &BoardAutomationService{
		transport: runtime,
		support: func() bool {
			snapshot := runtime.Snapshot()
			return snapshot.Connected &&
				snapshot.Hello.Capabilities&native.CapabilityBoardAutomation != 0
		},
	}
}

func (service *BoardAutomationService) requireSupport() error {
	if service.support != nil && !service.support() {
		return ErrBoardAutomationsUnsupported
	}
	return nil
}

func (service *BoardAutomationService) List(ctx context.Context) (BoardAutomationSnapshot, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.requireSupport(); err != nil {
		return BoardAutomationSnapshot{}, err
	}
	return service.fetch(ctx)
}

func (service *BoardAutomationService) fetch(ctx context.Context) (BoardAutomationSnapshot, error) {
	for attempt := 0; attempt < 3; attempt++ {
		cursor := byte(0)
		var generation uint16
		var records []native.AutomationRecord
		for pageNumber := 0; pageNumber < native.AutomationCapacity; pageNumber++ {
			payload, _ := native.AutomationListPayload(cursor)
			frame, err := service.transport.Request(ctx, native.OpAutomationList, payload, native.OpAutomationListResp)
			if err != nil {
				return BoardAutomationSnapshot{}, err
			}
			page, err := native.ParseAutomationList(frame.Payload)
			if err != nil {
				return BoardAutomationSnapshot{}, err
			}
			if pageNumber == 0 {
				generation = page.Generation
			} else if page.Generation != generation {
				break
			}
			records = append(records, page.Records...)
			if page.NextCursor == native.AutomationAllocateID {
				if len(records) != int(page.Total) {
					return BoardAutomationSnapshot{}, fmt.Errorf("automation list declared %d records but returned %d", page.Total, len(records))
				}
				sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
				return BoardAutomationSnapshot{Generation: generation, Records: records}, nil
			}
			if page.NextCursor <= cursor {
				return BoardAutomationSnapshot{}, fmt.Errorf("automation list cursor did not advance from %d", cursor)
			}
			cursor = page.NextCursor
		}
	}
	return BoardAutomationSnapshot{}, errors.New("automation table changed during three consecutive paged reads")
}

func (service *BoardAutomationService) Put(ctx context.Context, record native.AutomationRecord) (native.AutomationRecordReadback, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.requireSupport(); err != nil {
		return native.AutomationRecordReadback{}, err
	}
	payload, err := native.AutomationPutPayload(record)
	if err != nil {
		return native.AutomationRecordReadback{}, err
	}
	frame, err := service.transport.Request(ctx, native.OpAutomationPut, payload, native.OpAutomationRecord)
	if err != nil {
		return native.AutomationRecordReadback{}, err
	}
	readback, err := native.ParseAutomationRecord(frame.Payload)
	if err != nil {
		return native.AutomationRecordReadback{}, err
	}
	snapshot, err := service.fetch(ctx)
	if err != nil {
		return native.AutomationRecordReadback{}, fmt.Errorf("verify automation %d: %w", readback.Record.ID, err)
	}
	for _, candidate := range snapshot.Records {
		if candidate.ID == readback.Record.ID {
			if candidate != readback.Record {
				return native.AutomationRecordReadback{}, fmt.Errorf("automation %d readback differs from committed list", candidate.ID)
			}
			return readback, nil
		}
	}
	return native.AutomationRecordReadback{}, fmt.Errorf("automation %d is absent after put", readback.Record.ID)
}

func (service *BoardAutomationService) Remove(ctx context.Context, id byte) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.requireSupport(); err != nil {
		return err
	}
	payload, err := native.AutomationRemovePayload(id)
	if err != nil {
		return err
	}
	if _, err = service.transport.Request(ctx, native.OpAutomationRemove, payload, native.OpACK); err != nil {
		return err
	}
	snapshot, err := service.fetch(ctx)
	if err != nil {
		return fmt.Errorf("verify automation removal: %w", err)
	}
	for _, record := range snapshot.Records {
		if record.ID == id {
			return fmt.Errorf("automation %d remains after remove", id)
		}
	}
	return nil
}

func (service *BoardAutomationService) Clear(ctx context.Context) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.requireSupport(); err != nil {
		return err
	}
	if _, err := service.transport.Request(ctx, native.OpAutomationClear, native.AutomationClearPayload(), native.OpACK); err != nil {
		return err
	}
	snapshot, err := service.fetch(ctx)
	if err != nil {
		return fmt.Errorf("verify automation clear: %w", err)
	}
	if len(snapshot.Records) != 0 {
		return fmt.Errorf("automation clear left %d records", len(snapshot.Records))
	}
	return nil
}

const boardAutomationUsage = "board-automation list | board-automation add EVENT EVENT_VALUE EVENT_MASK ACTION TARGET VALUE EXTRA [on|off] | board-automation edit ID EVENT EVENT_VALUE EVENT_MASK ACTION TARGET VALUE EXTRA [on|off] | board-automation remove ID | board-automation clear"

func boardAutomationCommand(ctx context.Context, runtime *Runtime, args []string) (string, error) {
	service := NewBoardAutomationService(runtime)
	if len(args) == 1 && strings.EqualFold(args[0], "list") {
		snapshot, err := service.List(ctx)
		if err != nil {
			return "", err
		}
		return formatBoardAutomations(snapshot), nil
	}
	if len(args) == 2 && strings.EqualFold(args[0], "remove") {
		id, err := parseAutomationByte(args[1], "ID")
		if err != nil {
			return "", err
		}
		if err = service.Remove(ctx, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("board automation %d removed and verified", id), nil
	}
	if len(args) == 1 && strings.EqualFold(args[0], "clear") {
		if err := service.Clear(ctx); err != nil {
			return "", err
		}
		return "board automation table cleared and verified", nil
	}
	if len(args) >= 8 && (strings.EqualFold(args[0], "add") || strings.EqualFold(args[0], "edit")) {
		position := 1
		record := native.AutomationRecord{ID: native.AutomationAllocateID, Flags: native.AutomationEnabled}
		if strings.EqualFold(args[0], "edit") {
			if len(args) < 9 {
				return "", fmt.Errorf("usage: %s", boardAutomationUsage)
			}
			var err error
			record.ID, err = parseAutomationByte(args[position], "ID")
			if err != nil {
				return "", err
			}
			position++
		}
		if len(args) < position+7 || len(args) > position+8 {
			return "", fmt.Errorf("usage: %s", boardAutomationUsage)
		}
		var err error
		if record.EventKind, err = parseAutomationEvent(args[position]); err != nil {
			return "", err
		}
		if record.EventValue, err = parseAutomationByte(args[position+1], "event value"); err != nil {
			return "", err
		}
		if record.EventMask, err = parseAutomationByte(args[position+2], "event mask"); err != nil {
			return "", err
		}
		if record.ActionKind, err = parseAutomationAction(args[position+3]); err != nil {
			return "", err
		}
		if record.ActionTarget, err = parseAutomationByte(args[position+4], "action target"); err != nil {
			return "", err
		}
		if record.Value, err = parseAutomationUint16(args[position+5], "action value"); err != nil {
			return "", err
		}
		if record.Extra, err = parseAutomationUint16(args[position+6], "action extra"); err != nil {
			return "", err
		}
		if len(args) == position+8 {
			switch strings.ToLower(args[position+7]) {
			case "on", "enabled":
			case "off", "disabled":
				record.Flags = 0
			default:
				return "", errors.New("automation state must be on or off")
			}
		}
		readback, err := service.Put(ctx, record)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("board automation %d committed at generation %d and verified", readback.Record.ID, readback.Generation), nil
	}
	return "", fmt.Errorf("usage: %s", boardAutomationUsage)
}

func parseAutomationEvent(value string) (byte, error) {
	names := map[string]byte{"door": native.AutomationEventDoor, "bluetooth": native.AutomationEventBluetooth, "host": native.AutomationEventHost, "relay": native.AutomationEventRelay, "rf": native.AutomationEventLearnedRF, "key": native.AutomationEventKey, "alert": native.AutomationEventAlert, "boot": native.AutomationEventBoot}
	if result, ok := names[strings.ToLower(value)]; ok {
		return result, nil
	}
	return parseAutomationByte(value, "event kind")
}

func parseAutomationAction(value string) (byte, error) {
	names := map[string]byte{"safe-stop": native.AutomationActionSafeStop, "motion-stop": native.AutomationActionMotionStop, "relay": native.AutomationActionRelay, "pwm": native.AutomationActionPWM, "cue": native.AutomationActionStatusCue, "buzzer": native.AutomationActionBuzzer, "rf": native.AutomationActionRFTransmit, "macro": native.AutomationActionHostMacroRequest}
	if result, ok := names[strings.ToLower(value)]; ok {
		return result, nil
	}
	return parseAutomationByte(value, "action kind")
}

func parseAutomationByte(value, field string) (byte, error) {
	parsed, err := strconv.ParseUint(value, 0, 8)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned byte: %w", field, err)
	}
	return byte(parsed), nil
}

func parseAutomationUint16(value, field string) (uint16, error) {
	parsed, err := strconv.ParseUint(value, 0, 16)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned 16-bit value: %w", field, err)
	}
	return uint16(parsed), nil
}

func formatBoardAutomations(snapshot BoardAutomationSnapshot) string {
	if len(snapshot.Records) == 0 {
		return fmt.Sprintf("Board automations generation %d: empty", snapshot.Generation)
	}
	var output strings.Builder
	fmt.Fprintf(&output, "Board automations generation %d\nID  EN  EVENT       VALUE/MASK  ACTION       TARGET  VALUE  EXTRA\n", snapshot.Generation)
	for _, record := range snapshot.Records {
		fmt.Fprintf(&output, "%2d  %-3t %-11s 0x%02X/0x%02X %-12s %6d %6d %6d\n", record.ID, record.Enabled(), automationEventName(record.EventKind), record.EventValue, record.EventMask, automationActionName(record.ActionKind), record.ActionTarget, record.Value, record.Extra)
	}
	return strings.TrimRight(output.String(), "\n")
}

func automationEventName(kind byte) string {
	names := [...]string{"", "door", "bluetooth", "host", "relay", "learned-rf", "key", "alert", "boot"}
	if int(kind) < len(names) {
		return names[kind]
	}
	return strconv.Itoa(int(kind))
}

func automationActionName(kind byte) string {
	names := [...]string{"", "safe-stop", "motion-stop", "relay", "pwm", "status-cue", "buzzer", "rf-transmit", "host-macro"}
	if int(kind) < len(names) {
		return names[kind]
	}
	return strconv.Itoa(int(kind))
}
