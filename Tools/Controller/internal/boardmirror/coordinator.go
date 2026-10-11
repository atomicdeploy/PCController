// Package boardmirror coordinates volatile semantic state, not UART frames.
package boardmirror

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"pccontroller.local/controller/internal/fleet"
)

type State struct {
	BoardID    fleet.BoardID                  `json:"board_id"`
	Connected  bool                           `json:"connected"`
	Generation uint64                         `json:"generation"`
	Values     map[string]fleet.MirrorCommand `json:"values"`
	// Physical observations stay board-scoped. They are not synthetic readings
	// or raw key/I2C writes on another board.
	Observations map[string]any `json:"observations,omitempty"`
	Unsupported  []string       `json:"unsupported,omitempty"`
}

type Write struct {
	Route      fleet.RouteEnvelope `json:"route"`
	Command    fleet.MirrorCommand `json:"command"`
	Generation uint64              `json:"generation"`
}

type Result struct {
	RouteID      string        `json:"route_id"`
	BoardID      fleet.BoardID `json:"board_id"`
	Acknowledged bool          `json:"acknowledged"`
	Verified     bool          `json:"verified"`
	Error        string        `json:"error,omitempty"`
}

type Adapter interface {
	Inspect(context.Context, fleet.MirrorMember) (State, error)
	Apply(context.Context, fleet.MirrorMember, Write) (Result, error)
}

type GroupStatus struct {
	ID           string                   `json:"id"`
	Sequence     uint64                   `json:"sequence"`
	Members      map[fleet.BoardID]State  `json:"members"`
	Errors       map[fleet.BoardID]string `json:"errors"`
	Results      []Result                 `json:"last_results,omitempty"`
	Origin       fleet.BoardID            `json:"last_origin,omitempty"`
	DroppedNotes uint64                   `json:"dropped_notes"`
}

type tone struct {
	board    fleet.BoardID
	command  fleet.MirrorCommand
	observed time.Time
}
type expectedNote struct {
	command fleet.MirrorCommand
	expires time.Time
}
type groupRuntime struct {
	group          fleet.MirrorGroup
	status         GroupStatus
	previous       map[fleet.BoardID]State
	desired        map[string]fleet.MirrorCommand
	segmentOrigin  fleet.BoardID
	segmentRenewed time.Time
}

// Coordinator has one serialized owner. No retries or failover can silently
// re-energize a disconnected board. New generations establish a baseline;
// an explicit resync chooses the authoritative board afterward.
type Coordinator struct {
	mu          sync.Mutex
	adapter     Adapter
	config      fleet.MirrorConfig
	fingerprint string
	epoch       uint64
	groups      []*groupRuntime
	notes       chan tone
	expected    map[fleet.BoardID][]expectedNote
	echoMu      sync.Mutex
	dropped     atomic.Uint64
}

func New(adapter Adapter) *Coordinator {
	return &Coordinator{adapter: adapter, epoch: uint64(time.Now().UnixNano()), notes: make(chan tone, 64), expected: map[fleet.BoardID][]expectedNote{}}
}

func (coordinator *Coordinator) Configure(config fleet.MirrorConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	data, _ := json.Marshal(config)
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if string(data) == coordinator.fingerprint {
		return nil
	}
	coordinator.fingerprint = string(data)
	// Own a deep copy, independent of a caller's slice/map lifetime.
	_ = json.Unmarshal(data, &coordinator.config)
	coordinator.groups = nil
	coordinator.echoMu.Lock()
	coordinator.expected = map[fleet.BoardID][]expectedNote{}
	coordinator.echoMu.Unlock()
	coordinator.epoch = uint64(time.Now().UnixNano())
	for _, group := range coordinator.config.Groups {
		coordinator.groups = append(coordinator.groups, &groupRuntime{group: group, status: GroupStatus{ID: group.ID, Members: map[fleet.BoardID]State{}, Errors: map[fleet.BoardID]string{}}, previous: map[fleet.BoardID]State{}, desired: map[string]fleet.MirrorCommand{}})
	}
	return nil
}

func (coordinator *Coordinator) Status() []GroupStatus {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	result := make([]GroupStatus, 0, len(coordinator.groups))
	for _, group := range coordinator.groups {
		status := group.status
		status.DroppedNotes += coordinator.dropped.Load()
		result = append(result, status)
	}
	data, _ := json.Marshal(result)
	_ = json.Unmarshal(data, &result)
	return result
}

// ObserveNote never blocks the board event consumer. Overload is explicit.
func (coordinator *Coordinator) ObserveNote(board fleet.BoardID, command fleet.MirrorCommand) bool {
	if command.Kind != "buzzer" || command.Validate() != nil {
		return true
	}
	if coordinator.consumeEcho(tone{board, command, time.Now()}) {
		return false
	}
	select {
	case coordinator.notes <- tone{board, command, time.Now()}:
	default:
		coordinator.dropped.Add(1)
	}
	return true
}

func (coordinator *Coordinator) Run(ctx context.Context, config func() fleet.MirrorConfig) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if coordinator.Configure(config()) != nil {
				continue
			}
			coordinator.Poll(ctx)
		case note := <-coordinator.notes:
			coordinator.mu.Lock()
			coordinator.note(ctx, note)
			coordinator.mu.Unlock()
		}
	}
}

func (coordinator *Coordinator) Poll(ctx context.Context) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	for _, group := range coordinator.groups {
		coordinator.poll(ctx, group)
	}
}

func (coordinator *Coordinator) poll(ctx context.Context, group *groupRuntime) {
	// Read every member before fanout, so an ACK in this pass cannot be
	// mistaken for another board's new user input later in the same pass.
	states := map[fleet.BoardID]State{}
	for _, member := range group.group.Members {
		request, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		state, err := coordinator.adapter.Inspect(request, member)
		cancel()
		if err == nil && state.BoardID != member.BoardID {
			err = errors.New("board identity mismatch; member quarantined")
		}
		if err != nil || !state.Connected {
			if err == nil {
				err = errors.New("board disconnected; explicit resync required after reconnect")
			}
			group.status.Errors[member.BoardID] = err.Error()
			delete(group.previous, member.BoardID)
			delete(group.status.Members, member.BoardID)
			// Do not renew a disconnected presentation on reconnect.
			if member.BoardID == group.segmentOrigin {
				group.segmentOrigin = ""
			}
			continue
		}
		delete(group.status.Errors, member.BoardID)
		states[member.BoardID], group.status.Members[member.BoardID] = state, state
	}
	for _, member := range group.group.Members {
		state, ok := states[member.BoardID]
		if !ok {
			continue
		}
		previous, initialized := group.previous[member.BoardID]
		if initialized && previous.Generation == state.Generation {
			keys := make([]string, 0, len(state.Values))
			for key := range state.Values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				command := state.Values[key]
				if command.Kind == "buzzer" || !group.group.Policy.Allows(command) {
					continue
				}
				old, known := previous.Values[key]
				if !known || command == old || command == group.desired[key] {
					continue
				}
				coordinator.fanout(ctx, group, member.BoardID, command)
			}
		}
		group.previous[member.BoardID] = state
	}
	// Segment mirrors are leased, not permanent capture of the second panel.
	// Renew only while every member is still the same connected generation
	// and readback agrees. Stop renewal on drift/reconnect rather than fight it.
	command, owned := group.desired["segments"]
	if owned && group.segmentOrigin != "" && time.Since(group.segmentRenewed) >= 750*time.Millisecond {
		renew := len(states) == len(group.group.Members)
		for _, member := range group.group.Members {
			state := states[member.BoardID]
			if state.Values["segments"] != command {
				renew = false
			}
		}
		if renew {
			coordinator.fanout(ctx, group, group.segmentOrigin, command)
		}
	}
}

func (coordinator *Coordinator) consumeEcho(note tone) bool {
	coordinator.echoMu.Lock()
	defer coordinator.echoMu.Unlock()
	remaining := []expectedNote{}
	echo := false
	for _, expected := range coordinator.expected[note.board] {
		if !note.observed.Before(expected.expires) {
			continue
		}
		if !echo && expected.command == note.command {
			echo = true
			continue
		}
		remaining = append(remaining, expected)
	}
	coordinator.expected[note.board] = remaining
	return echo
}

func (coordinator *Coordinator) note(ctx context.Context, note tone) {
	for _, group := range coordinator.groups {
		for _, member := range group.group.Members {
			if member.BoardID != note.board || !group.group.Policy.Allows(note.command) {
				continue
			}
			if time.Since(note.observed) > 200*time.Millisecond {
				group.status.DroppedNotes++
				break
			}
			coordinator.fanout(ctx, group, note.board, note.command)
		}
	}
}

func (coordinator *Coordinator) fanout(ctx context.Context, group *groupRuntime, origin fleet.BoardID, command fleet.MirrorCommand) {
	group.status.Sequence++
	group.status.Origin = origin
	group.status.Results = nil
	if command.Kind != "buzzer" {
		group.desired[command.Key()] = command
	}
	if command.Kind == "segments" {
		group.segmentOrigin, group.segmentRenewed = origin, time.Now()
	}
	var routeBytes [16]byte
	if _, err := rand.Read(routeBytes[:]); err != nil {
		group.status.Errors[origin] = err.Error()
		return
	}
	routeID := hex.EncodeToString(routeBytes[:])
	encoded, _ := json.Marshal(command)
	for _, member := range group.group.Members {
		if member.BoardID == origin {
			continue
		}
		state, ok := group.status.Members[member.BoardID]
		result := Result{BoardID: member.BoardID, RouteID: routeID}
		if !ok || !state.Connected {
			result.Error = "member unavailable; no deferred actuation"
			group.status.Results = append(group.status.Results, result)
			continue
		}
		if command.Kind != "buzzer" {
			if _, capable := state.Values[command.Key()]; !capable {
				result.Error = "domain unavailable on target"
				group.status.Results = append(group.status.Results, result)
				continue
			}
		}
		request, cancel := context.WithTimeout(ctx, 2*time.Second)
		write := Write{Generation: state.Generation, Command: command, Route: fleet.RouteEnvelope{
			RouteID: routeID, OperationID: routeID, TraceID: routeID, OriginInstanceID: coordinator.config.CoordinatorID, OriginBoardID: origin,
			Target: fleet.Destination{BoardID: member.BoardID}, Coordinator: fleet.CoordinatorPosition{ID: coordinator.config.CoordinatorID, Epoch: coordinator.epoch, Sequence: group.status.Sequence}, HopCount: 1, HopLimit: 1, Deadline: time.Now().Add(2 * time.Second),
			Intent: fleet.Intent{Domain: command.Kind, Name: "mirror", Arguments: map[string]json.RawMessage{"command": encoded}},
		}}
		if command.Kind == "buzzer" {
			coordinator.echoMu.Lock()
			coordinator.expected[member.BoardID] = append(coordinator.expected[member.BoardID], expectedNote{command, time.Now().Add(2 * time.Second)})
			if len(coordinator.expected[member.BoardID]) > 64 {
				coordinator.expected[member.BoardID] = coordinator.expected[member.BoardID][1:]
			}
			coordinator.echoMu.Unlock()
		}
		applied, err := coordinator.adapter.Apply(request, member, write)
		cancel()
		if err != nil {
			result.Error = err.Error()
		} else {
			result = applied
		}
		group.status.Results = append(group.status.Results, result)
	}
}

func (coordinator *Coordinator) Resync(ctx context.Context, groupID string, origin fleet.BoardID) ([]Result, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	for _, group := range coordinator.groups {
		if group.group.ID != groupID {
			continue
		}
		coordinator.poll(ctx, group)
		state, ok := group.status.Members[origin]
		if !ok {
			return nil, errors.New("resync source is unavailable or not a member")
		}
		keys := []string{}
		for key := range state.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		results := []Result{}
		for _, key := range keys {
			command := state.Values[key]
			if command.Kind != "buzzer" && group.group.Policy.Allows(command) {
				coordinator.fanout(ctx, group, origin, command)
				results = append(results, group.status.Results...)
			}
		}
		return results, nil
	}
	return nil, fmt.Errorf("unknown mirror group %q", groupID)
}
