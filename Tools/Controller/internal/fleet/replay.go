package fleet

import (
	"container/list"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const maximumReplayResult = 64 * 1024

type ReplayState string

const (
	ReplayInProgress ReplayState = "in_progress"
	ReplayComplete   ReplayState = "complete"
)

// ReplayRecord is the bounded cached disposition of one immutable route ID.
// Result is opaque host-level JSON and is copied on both write and read.
type ReplayRecord struct {
	RouteID string          `json:"route_id"`
	State   ReplayState     `json:"state"`
	Result  json.RawMessage `json:"result,omitempty"`
	Updated time.Time       `json:"updated"`
	Expires time.Time       `json:"expires"`
}

type replayEntry struct {
	record ReplayRecord
}

// ReplayCache is a concurrency-safe bounded TTL/LRU cache. Begin returns an
// existing in-progress or terminal record instead of redispatching a route.
type ReplayCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	order    *list.List
	entries  map[string]*list.Element
}

func NewReplayCache(capacity int, ttl time.Duration) (*ReplayCache, error) {
	if capacity <= 0 {
		return nil, errors.New("replay cache capacity must be positive")
	}
	if ttl <= 0 {
		return nil, errors.New("replay cache ttl must be positive")
	}
	return &ReplayCache{
		capacity: capacity,
		ttl:      ttl,
		order:    list.New(),
		entries:  make(map[string]*list.Element, capacity),
	}, nil
}

// Begin records a fresh route as in progress. Fresh is false when the route is
// a replay, in which case the cached record is returned unchanged.
func (cache *ReplayCache) Begin(routeID string, now time.Time) (record ReplayRecord, fresh bool, err error) {
	if err := validateRouteID(routeID); err != nil {
		return ReplayRecord{}, false, err
	}
	now = now.UTC()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.pruneLocked(now)
	if element, ok := cache.entries[routeID]; ok {
		cache.order.MoveToFront(element)
		return cloneReplayRecord(element.Value.(*replayEntry).record), false, nil
	}
	record = ReplayRecord{
		RouteID: routeID,
		State:   ReplayInProgress,
		Updated: now,
		Expires: now.Add(cache.ttl),
	}
	element := cache.order.PushFront(&replayEntry{record: record})
	cache.entries[routeID] = element
	cache.enforceCapacityLocked()
	return cloneReplayRecord(record), true, nil
}

// Complete stores the terminal result for a route that was admitted by Begin.
func (cache *ReplayCache) Complete(routeID string, result json.RawMessage, now time.Time) error {
	if err := validateRouteID(routeID); err != nil {
		return err
	}
	if len(result) != 0 && !json.Valid(result) {
		return errors.New("replay result is not valid JSON")
	}
	if len(result) > maximumReplayResult {
		return errors.New("replay result exceeds bounded size")
	}
	now = now.UTC()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.pruneLocked(now)
	element, ok := cache.entries[routeID]
	if !ok {
		return errors.New("route is not present in replay cache")
	}
	entry := element.Value.(*replayEntry)
	if entry.record.State == ReplayComplete {
		return errors.New("route already has a terminal replay result")
	}
	entry.record.State = ReplayComplete
	entry.record.Result = append(json.RawMessage(nil), result...)
	entry.record.Updated = now
	entry.record.Expires = now.Add(cache.ttl)
	cache.order.MoveToFront(element)
	return nil
}

func (cache *ReplayCache) Lookup(routeID string, now time.Time) (ReplayRecord, bool) {
	if validateRouteID(routeID) != nil {
		return ReplayRecord{}, false
	}
	now = now.UTC()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.pruneLocked(now)
	element, ok := cache.entries[routeID]
	if !ok {
		return ReplayRecord{}, false
	}
	cache.order.MoveToFront(element)
	return cloneReplayRecord(element.Value.(*replayEntry).record), true
}

func (cache *ReplayCache) pruneLocked(now time.Time) {
	for element := cache.order.Back(); element != nil; {
		previous := element.Prev()
		entry := element.Value.(*replayEntry)
		if !now.Before(entry.record.Expires) {
			delete(cache.entries, entry.record.RouteID)
			cache.order.Remove(element)
		}
		element = previous
	}
}

func (cache *ReplayCache) enforceCapacityLocked() {
	for cache.order.Len() > cache.capacity {
		element := cache.order.Back()
		entry := element.Value.(*replayEntry)
		delete(cache.entries, entry.record.RouteID)
		cache.order.Remove(element)
	}
}

func cloneReplayRecord(record ReplayRecord) ReplayRecord {
	record.Result = append(json.RawMessage(nil), record.Result...)
	return record
}
