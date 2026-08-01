package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

type HistoryOptions struct {
	Retention      time.Duration
	SampleInterval time.Duration
	TimelineLimit  int
	TimelinePath   string
}

type StatusSample struct {
	Time   time.Time     `json:"time"`
	Status native.Status `json:"status"`
}

type TimelineEntry struct {
	ID          uint64            `json:"id"`
	Time        time.Time         `json:"time"`
	Kind        string            `json:"kind"`
	Text        string            `json:"text"`
	Lifecycle   string            `json:"lifecycle,omitempty"`
	State       string            `json:"state,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Port        string            `json:"port,omitempty"`
	Gesture     string            `json:"gesture,omitempty"`
	Source      string            `json:"source,omitempty"`
	Target      string            `json:"target,omitempty"`
	MessageType string            `json:"message_type,omitempty"`
	Action      string            `json:"action,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	RFCode      uint32            `json:"rf_code,omitempty"`
	RFBits      byte              `json:"rf_bits,omitempty"`
	RFProtocol  byte              `json:"rf_protocol,omitempty"`
	RFID        *byte             `json:"rf_id,omitempty"`
	ResetCause  byte              `json:"reset_cause,omitempty"`
	ResetCount  uint32            `json:"reset_count,omitempty"`
}

func (runtime *Runtime) ConfigureHistory(options HistoryOptions) error {
	if options.Retention < 0 || options.Retention > 30*24*time.Hour {
		return errors.New("history retention must be 0..720h")
	}
	if options.SampleInterval <= 0 {
		options.SampleInterval = time.Second
	}
	if options.SampleInterval < 100*time.Millisecond ||
		options.SampleInterval > time.Minute {
		return errors.New("history sample interval must be 100ms..1m")
	}
	if options.TimelineLimit == 0 {
		options.TimelineLimit = 2000
	}
	if options.TimelineLimit < 50 || options.TimelineLimit > 100_000 {
		return errors.New("timeline limit must be 50..100000")
	}
	path := strings.TrimSpace(options.TimelinePath)
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve timeline path: %w", err)
		}
		path = absolute
	}

	loaded, err := loadTimeline(path, options.Retention, options.TimelineLimit)
	if err != nil {
		return err
	}
	runtime.historyMu.Lock()
	runtime.historyRetention = options.Retention
	runtime.historySampleEvery = options.SampleInterval
	runtime.timelineLimit = options.TimelineLimit
	runtime.timelinePath = path
	runtime.timeline = loaded
	runtime.pruneHistoryLocked(time.Now())
	runtime.historyMu.Unlock()
	if path != "" {
		runtime.historyWriteOnce.Do(func() {
			runtime.historyWrites = make(chan TimelineEntry, 256)
			go runtime.writeTimeline()
		})
	}
	return nil
}

func (runtime *Runtime) StatusHistory(since time.Time) []StatusSample {
	runtime.historyMu.RLock()
	defer runtime.historyMu.RUnlock()
	result := make([]StatusSample, 0, len(runtime.statusHistory))
	for _, sample := range runtime.statusHistory {
		if since.IsZero() || !sample.Time.Before(since) {
			result = append(result, sample)
		}
	}
	return result
}

func (runtime *Runtime) Timeline(since time.Time, limit int) []TimelineEntry {
	runtime.historyMu.RLock()
	defer runtime.historyMu.RUnlock()
	if limit <= 0 || limit > runtime.timelineLimit {
		limit = runtime.timelineLimit
	}
	result := make([]TimelineEntry, 0, limit)
	for _, entry := range runtime.timeline {
		if since.IsZero() || !entry.Time.Before(since) {
			result = append(result, cloneTimelineEntry(entry))
		}
	}
	if len(result) > limit {
		result = append([]TimelineEntry(nil), result[len(result)-limit:]...)
	}
	return result
}

func (runtime *Runtime) recordStatus(at time.Time, status native.Status) {
	runtime.historyMu.Lock()
	defer runtime.historyMu.Unlock()
	if runtime.historyRetention == 0 ||
		(!runtime.historyLastSample.IsZero() &&
			at.Sub(runtime.historyLastSample) < runtime.historySampleEvery) {
		return
	}
	runtime.historyLastSample = at
	runtime.statusHistory = append(runtime.statusHistory, StatusSample{
		Time: at, Status: status,
	})
	runtime.pruneHistoryLocked(at)
}

func (runtime *Runtime) recordTimeline(event Event) {
	if !importantTimelineKind(event.Kind) {
		return
	}
	entry := TimelineEntry{
		ID: event.ID, Time: event.Time, Kind: event.Kind, Text: event.Text,
		Lifecycle: event.Lifecycle, State: event.State, Reason: event.Reason,
		Port: event.Port.Name, Gesture: event.Gesture, Source: event.Source,
		Target: event.Target, MessageType: event.MessageType, Action: event.Action,
		Metadata: cloneStringValues(event.Metadata),
		RFCode:   event.RFCode, RFBits: event.RFBits,
		RFProtocol: event.RFProtocol, ResetCause: event.ResetCause,
		ResetCount: event.ResetCount,
	}
	if event.HaveRFID {
		value := event.RFID
		entry.RFID = &value
	}
	runtime.historyMu.Lock()
	runtime.timeline = append(runtime.timeline, entry)
	runtime.pruneHistoryLocked(event.Time)
	path := runtime.timelinePath
	writes := runtime.historyWrites
	runtime.historyMu.Unlock()
	if path != "" && writes != nil {
		select {
		case writes <- entry:
		default:
			runtime.publishHistoryWriteError("timeline writer queue is full")
		}
	}
}

func (runtime *Runtime) pruneHistoryLocked(now time.Time) {
	if runtime.historyRetention == 0 {
		runtime.statusHistory = nil
	} else {
		cutoff := now.Add(-runtime.historyRetention)
		statusStart := 0
		for statusStart < len(runtime.statusHistory) &&
			runtime.statusHistory[statusStart].Time.Before(cutoff) {
			statusStart++
		}
		if statusStart != 0 {
			runtime.statusHistory = append(
				[]StatusSample(nil),
				runtime.statusHistory[statusStart:]...,
			)
		}
		timelineStart := 0
		for timelineStart < len(runtime.timeline) &&
			runtime.timeline[timelineStart].Time.Before(cutoff) {
			timelineStart++
		}
		if timelineStart != 0 {
			runtime.timeline = append(
				[]TimelineEntry(nil),
				runtime.timeline[timelineStart:]...,
			)
		}
	}
	if runtime.timelineLimit > 0 && len(runtime.timeline) > runtime.timelineLimit {
		runtime.timeline = append(
			[]TimelineEntry(nil),
			runtime.timeline[len(runtime.timeline)-runtime.timelineLimit:]...,
		)
	}
}

func importantTimelineKind(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	return kind != "" && kind != "telemetry" && kind != "rx" && kind != "tx"
}

func (runtime *Runtime) writeTimeline() {
	for entry := range runtime.historyWrites {
		runtime.historyMu.RLock()
		path := runtime.timelinePath
		runtime.historyMu.RUnlock()
		if path == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			runtime.publishHistoryWriteError(err.Error())
			continue
		}
		// Open for one append and close immediately. Important events are sparse,
		// and this guarantees durability while avoiding a process-wide Windows
		// file handle that blocks configuration backups or retention cleanup.
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			runtime.publishHistoryWriteError(err.Error())
			continue
		}
		if err := json.NewEncoder(file).Encode(entry); err != nil {
			runtime.publishHistoryWriteError(err.Error())
		}
		if err := file.Close(); err != nil {
			runtime.publishHistoryWriteError(err.Error())
		}
	}
}

func (runtime *Runtime) publishHistoryWriteError(message string) {
	// Do not call publish here: that would attempt another durable write and
	// recurse. The in-memory event stream receives a diagnostic directly.
	runtime.eventMu.Lock()
	runtime.nextEventID++
	event := Event{
		ID: runtime.nextEventID, Time: time.Now(), Kind: "error",
		Text: "timeline: " + message,
	}
	runtime.eventLog = append(runtime.eventLog, event)
	if len(runtime.eventLog) > 512 {
		runtime.eventLog = append(
			[]Event(nil),
			runtime.eventLog[len(runtime.eventLog)-512:]...,
		)
	}
	close(runtime.eventNotify)
	runtime.eventNotify = make(chan struct{})
	runtime.eventMu.Unlock()
	select {
	case runtime.events <- event:
	default:
	}
}

func loadTimeline(
	path string,
	retention time.Duration,
	limit int,
) ([]TimelineEntry, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open timeline: %w", err)
	}
	defer file.Close()
	cutoff := time.Time{}
	if retention > 0 {
		cutoff = time.Now().Add(-retention)
	}
	var result []TimelineEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var entry TimelineEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if !cutoff.IsZero() && entry.Time.Before(cutoff) {
			continue
		}
		result = append(result, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read timeline: %w", err)
	}
	if limit > 0 && len(result) > limit {
		result = append([]TimelineEntry(nil), result[len(result)-limit:]...)
	}
	return result, nil
}

func cloneTimelineEntry(entry TimelineEntry) TimelineEntry {
	entry.Metadata = cloneStringValues(entry.Metadata)
	if entry.RFID != nil {
		value := *entry.RFID
		entry.RFID = &value
	}
	return entry
}

func cloneStringValues(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
