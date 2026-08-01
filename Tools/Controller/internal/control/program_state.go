package control

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProgramMode string

const (
	ProgramIdle    ProgramMode = "Idle"
	ProgramRunning ProgramMode = "Running"
)

type ProgramStateOwner struct {
	Name       string `json:"name"`
	Reason     string `json:"reason,omitempty"`
	References int    `json:"references"`
	Persistent bool   `json:"persistent,omitempty"`
}

type ProgramStateSnapshot struct {
	Mode      ProgramMode         `json:"mode"`
	Reason    string              `json:"reason,omitempty"`
	Owners    []ProgramStateOwner `json:"owners,omitempty"`
	Revision  uint64              `json:"revision"`
	ChangedAt time.Time           `json:"changed_at"`
}

type programStateClaim struct {
	owner  string
	reason string
	order  uint64
}

// ProgramStateManager owns the application-level Idle/Running state. Durable
// consumer claims and transient activity leases compose by owner/refcount;
// hardware conditions such as the enclosure door never mutate this state.
type ProgramStateManager struct {
	mu         sync.RWMutex
	persistent map[string]programStateClaim
	leases     map[uint64]programStateClaim
	nextToken  uint64
	nextOrder  uint64
	revision   uint64
	changedAt  time.Time
	onChange   func(ProgramStateSnapshot)
}

func NewProgramStateManager(onChange func(ProgramStateSnapshot)) *ProgramStateManager {
	return &ProgramStateManager{
		persistent: make(map[string]programStateClaim),
		leases:     make(map[uint64]programStateClaim),
		changedAt:  time.Now(),
		onChange:   onChange,
	}
}

func (manager *ProgramStateManager) Snapshot() ProgramStateSnapshot {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.snapshotLocked()
}

// Set creates or clears one durable consumer-owned Running claim.
func (manager *ProgramStateManager) Set(owner string, mode ProgramMode, reason string) (ProgramStateSnapshot, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return ProgramStateSnapshot{}, fmt.Errorf("program-state owner is required")
	}
	if mode != ProgramIdle && mode != ProgramRunning {
		return ProgramStateSnapshot{}, fmt.Errorf("program state %q must be Idle or Running", mode)
	}
	manager.mu.Lock()
	if mode == ProgramIdle {
		delete(manager.persistent, owner)
	} else {
		manager.nextOrder++
		manager.persistent[owner] = programStateClaim{owner: owner, reason: strings.TrimSpace(reason), order: manager.nextOrder}
	}
	snapshot := manager.changedLocked()
	manager.mu.Unlock()
	manager.notify(snapshot)
	return snapshot, nil
}

// Acquire returns an idempotently releasable transient Running lease.
func (manager *ProgramStateManager) Acquire(owner, reason string) (*ProgramStateLease, ProgramStateSnapshot, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, ProgramStateSnapshot{}, fmt.Errorf("program-state owner is required")
	}
	manager.mu.Lock()
	manager.nextToken++
	if manager.nextToken == 0 {
		manager.nextToken++
	}
	manager.nextOrder++
	token := manager.nextToken
	manager.leases[token] = programStateClaim{owner: owner, reason: strings.TrimSpace(reason), order: manager.nextOrder}
	snapshot := manager.changedLocked()
	manager.mu.Unlock()
	manager.notify(snapshot)
	return &ProgramStateLease{manager: manager, token: token}, snapshot, nil
}

type ProgramStateLease struct {
	manager *ProgramStateManager
	token   uint64
	once    sync.Once
}

func (lease *ProgramStateLease) Release() {
	if lease == nil || lease.manager == nil {
		return
	}
	lease.once.Do(func() { lease.manager.release(lease.token) })
}

func (manager *ProgramStateManager) release(token uint64) {
	manager.mu.Lock()
	if _, exists := manager.leases[token]; !exists {
		manager.mu.Unlock()
		return
	}
	delete(manager.leases, token)
	snapshot := manager.changedLocked()
	manager.mu.Unlock()
	manager.notify(snapshot)
}

func (manager *ProgramStateManager) changedLocked() ProgramStateSnapshot {
	manager.revision++
	manager.changedAt = time.Now()
	return manager.snapshotLocked()
}

func (manager *ProgramStateManager) snapshotLocked() ProgramStateSnapshot {
	type aggregate struct {
		ProgramStateOwner
		order uint64
	}
	owners := make(map[string]*aggregate)
	latest := programStateClaim{}
	add := func(claim programStateClaim, persistent bool) {
		value := owners[claim.owner]
		if value == nil {
			value = &aggregate{ProgramStateOwner: ProgramStateOwner{Name: claim.owner}}
			owners[claim.owner] = value
		}
		value.References++
		value.Persistent = value.Persistent || persistent
		if claim.order >= value.order {
			value.order, value.Reason = claim.order, claim.reason
		}
		if claim.order >= latest.order {
			latest = claim
		}
	}
	for _, claim := range manager.persistent {
		add(claim, true)
	}
	for _, claim := range manager.leases {
		add(claim, false)
	}
	result := ProgramStateSnapshot{Mode: ProgramIdle, Revision: manager.revision, ChangedAt: manager.changedAt}
	if len(owners) != 0 {
		result.Mode, result.Reason = ProgramRunning, latest.reason
	}
	for _, owner := range owners {
		result.Owners = append(result.Owners, owner.ProgramStateOwner)
	}
	sort.Slice(result.Owners, func(i, j int) bool { return result.Owners[i].Name < result.Owners[j].Name })
	return result
}

func (manager *ProgramStateManager) notify(snapshot ProgramStateSnapshot) {
	if manager.onChange != nil {
		manager.onChange(snapshot)
	}
}
