package control

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

type HistoryOptions struct {
	Retention      time.Duration
	SampleInterval time.Duration
	TimelineLimit  int
	TimelinePath   string
	StatusPath     string
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
	RFPulseUS   uint16            `json:"rf_pulse_us,omitempty"`
	RFID        *byte             `json:"rf_id,omitempty"`
	ResetCause  byte              `json:"reset_cause,omitempty"`
	ResetCount  uint32            `json:"reset_count,omitempty"`
}

const (
	// Measurement history is intentionally bounded independently of retention.
	// The compact record is about 100 bytes, so the default 24h/1s history fits
	// comfortably while malformed or overly ambitious configurations stay safe.
	maxMeasurementHistoryBytes = 32 * 1024 * 1024
	maxMeasurementLineBytes    = 4096
	maxMeasurementSamples      = 250_000
	measurementStatusBytes     = 47
	measurementCompactEvery    = 15 * time.Minute
)

// measurementRecord is private storage, not an API schema. The status bytes
// preserve the current native Status value without names, messages, device
// identifiers, or other user data that does not belong in a measurement log.
type measurementRecord struct {
	TimeMS int64  `json:"t"`
	Status string `json:"s"`
}

type measurementWrite struct {
	Path    string
	Sample  *StatusSample
	Compact bool
	Done    chan error
}

func (runtime *Runtime) ConfigureHistory(options HistoryOptions) error {
	runtime.historyConfigureMu.Lock()
	defer runtime.historyConfigureMu.Unlock()
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
	path, err := resolveHistoryPath(options.TimelinePath, "timeline")
	if err != nil {
		return err
	}
	statusPath, err := resolveHistoryPath(options.StatusPath, "measurement history")
	if err != nil {
		return err
	}

	loaded, err := loadTimeline(path, options.Retention, options.TimelineLimit)
	if err != nil {
		return err
	}
	runtime.historyMu.RLock()
	previousStatusPath := runtime.statusHistoryPath
	runtime.historyMu.RUnlock()
	var loadedStatus []StatusSample
	if statusPath != previousStatusPath {
		loadedStatus, err = loadStatusHistory(statusPath, options.Retention)
		if err != nil {
			return err
		}
	}
	runtime.historyMu.Lock()
	runtime.historyRetention = options.Retention
	runtime.historySampleEvery = options.SampleInterval
	runtime.timelineLimit = options.TimelineLimit
	runtime.timelinePath = path
	runtime.timeline = loaded
	if statusPath != previousStatusPath {
		runtime.statusHistory = loadedStatus
	}
	runtime.statusHistoryPath = statusPath
	runtime.pruneHistoryLocked(time.Now())
	runtime.deduplicateStatusHistoryLocked()
	if len(runtime.statusHistory) > maxMeasurementSamples {
		runtime.statusHistory = append(
			[]StatusSample(nil),
			runtime.statusHistory[len(runtime.statusHistory)-maxMeasurementSamples:]...,
		)
	}
	if len(runtime.statusHistory) != 0 {
		runtime.historyLastSample = runtime.statusHistory[len(runtime.statusHistory)-1].Time
	} else {
		runtime.historyLastSample = time.Time{}
	}
	runtime.historyMu.Unlock()
	if path != "" {
		runtime.historyWriteOnce.Do(func() {
			runtime.historyWrites = make(chan TimelineEntry, 256)
			go runtime.writeTimeline()
		})
	}
	if statusPath != "" {
		runtime.statusHistoryWriteOnce.Do(func() {
			runtime.statusHistoryWrites = make(chan measurementWrite, 1024)
			go runtime.writeStatusHistory()
		})
		request := measurementWrite{
			Path: statusPath, Compact: true, Done: make(chan error, 1),
		}
		runtime.statusHistoryWrites <- request
		if err := <-request.Done; err != nil {
			return fmt.Errorf("compact measurement history: %w", err)
		}
	}
	return nil
}

func resolveHistoryPath(value, label string) (string, error) {
	path := strings.TrimSpace(value)
	if path == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s path: %w", label, err)
	}
	return absolute, nil
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
	if runtime.historyRetention == 0 ||
		(!runtime.historyLastSample.IsZero() &&
			at.Sub(runtime.historyLastSample) < runtime.historySampleEvery) {
		runtime.historyMu.Unlock()
		return
	}
	if len(runtime.statusHistory) != 0 {
		last := runtime.statusHistory[len(runtime.statusHistory)-1]
		if at.Equal(last.Time) && status == last.Status {
			runtime.historyMu.Unlock()
			return
		}
	}
	runtime.historyLastSample = at
	sample := StatusSample{
		Time: at, Status: status,
	}
	runtime.statusHistory = append(runtime.statusHistory, sample)
	runtime.pruneHistoryLocked(at)
	if len(runtime.statusHistory) > maxMeasurementSamples {
		runtime.statusHistory = append(
			[]StatusSample(nil),
			runtime.statusHistory[len(runtime.statusHistory)-maxMeasurementSamples:]...,
		)
	}
	path := runtime.statusHistoryPath
	writes := runtime.statusHistoryWrites
	runtime.historyMu.Unlock()
	if path != "" && writes != nil {
		select {
		case writes <- measurementWrite{Path: path, Sample: &sample}:
		default:
			runtime.publishHistoryWriteError("measurement writer queue is full")
		}
	}
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
		RFProtocol: event.RFProtocol, RFPulseUS: event.RFPulseUS,
		ResetCause: event.ResetCause,
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

func (runtime *Runtime) deduplicateStatusHistoryLocked() {
	if len(runtime.statusHistory) < 2 {
		return
	}
	sort.SliceStable(runtime.statusHistory, func(left, right int) bool {
		return runtime.statusHistory[left].Time.Before(runtime.statusHistory[right].Time)
	})
	result := runtime.statusHistory[:0]
	seen := make(map[struct {
		TimeMS int64
		Status native.Status
	}]struct{}, len(runtime.statusHistory))
	for _, sample := range runtime.statusHistory {
		key := struct {
			TimeMS int64
			Status native.Status
		}{TimeMS: sample.Time.UnixMilli(), Status: sample.Status}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, sample)
	}
	runtime.statusHistory = result
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
		Text: "history: " + message,
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

func (runtime *Runtime) writeStatusHistory() {
	lastPersisted := make(map[string]time.Time)
	lastCompaction := make(map[string]time.Time)
	for request := range runtime.statusHistoryWrites {
		var err error
		if request.Compact {
			var samples []StatusSample
			samples, _ = runtime.measurementHistorySnapshot(request.Path)
			err = rewriteMeasurementHistory(request.Path, samples)
			if err == nil {
				lastPersisted[request.Path] = latestStatusSampleTime(samples)
				lastCompaction[request.Path] = time.Now()
			}
		} else if request.Sample != nil {
			if !runtime.measurementHistoryPathIsCurrent(request.Path) {
				continue
			}
			sample := *request.Sample
			if !sample.Time.After(lastPersisted[request.Path]) {
				continue
			}
			line, encodeErr := marshalMeasurementSample(sample)
			if encodeErr != nil {
				err = encodeErr
			} else if measurementHistoryNeedsCompaction(
				request.Path,
				int64(len(line)),
				lastCompaction[request.Path],
			) {
				var samples []StatusSample
				samples, _ = runtime.measurementHistorySnapshot(request.Path)
				err = rewriteMeasurementHistory(request.Path, samples)
				if err == nil {
					lastPersisted[request.Path] = latestStatusSampleTime(samples)
					lastCompaction[request.Path] = time.Now()
				}
			} else {
				err = appendMeasurementSample(request.Path, line)
				if err == nil {
					lastPersisted[request.Path] = sample.Time
				}
			}
		}
		if request.Done != nil {
			request.Done <- err
			close(request.Done)
		} else if err != nil {
			runtime.publishHistoryWriteError("measurements: " + err.Error())
		}
	}
}

func (runtime *Runtime) measurementHistorySnapshot(path string) ([]StatusSample, bool) {
	runtime.historyMu.RLock()
	defer runtime.historyMu.RUnlock()
	if path == "" || path != runtime.statusHistoryPath {
		return nil, false
	}
	return append([]StatusSample(nil), runtime.statusHistory...), true
}

func (runtime *Runtime) measurementHistoryPathIsCurrent(path string) bool {
	runtime.historyMu.RLock()
	defer runtime.historyMu.RUnlock()
	return path != "" && path == runtime.statusHistoryPath
}

func measurementHistoryNeedsCompaction(
	path string,
	nextBytes int64,
	last time.Time,
) bool {
	if last.IsZero() || time.Since(last) >= measurementCompactEvery {
		return true
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return err != nil || info.Size()+nextBytes > maxMeasurementHistoryBytes
}

func appendMeasurementSample(path string, line []byte) error {
	if len(line) == 0 || len(line) > maxMeasurementLineBytes {
		return errors.New("encoded measurement record is outside the size limit")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create measurement history directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open measurement history: %w", err)
	}
	var written int
	if written, err = file.Write(line); err == nil && written != len(line) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("append measurement history: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close measurement history: %w", closeErr)
	}
	return nil
}

func rewriteMeasurementHistory(path string, samples []StatusSample) error {
	content, err := encodeBoundedMeasurementHistory(samples, maxMeasurementHistoryBytes)
	if err != nil {
		return err
	}
	if err := writeHistoryFileAtomic(path, content, 0o600); err != nil {
		return fmt.Errorf("replace measurement history: %w", err)
	}
	return nil
}

func encodeBoundedMeasurementHistory(
	samples []StatusSample,
	maxBytes int,
) ([]byte, error) {
	if maxBytes < 0 {
		return nil, errors.New("measurement history byte limit must not be negative")
	}
	lines := make([][]byte, 0, len(samples))
	total := 0
	for index := len(samples) - 1; index >= 0; index-- {
		line, err := marshalMeasurementSample(samples[index])
		if err != nil {
			return nil, err
		}
		if len(line) > maxBytes || total+len(line) > maxBytes {
			break
		}
		lines = append(lines, line)
		total += len(line)
	}
	var result bytes.Buffer
	result.Grow(total)
	for index := len(lines) - 1; index >= 0; index-- {
		_, _ = result.Write(lines[index])
	}
	return result.Bytes(), nil
}

func marshalMeasurementSample(sample StatusSample) ([]byte, error) {
	if sample.Time.IsZero() {
		return nil, errors.New("measurement sample has a zero timestamp")
	}
	status := sample.Status
	payload := make([]byte, measurementStatusBytes)
	binary.LittleEndian.PutUint32(payload[0:4], status.UptimeMS)
	binary.LittleEndian.PutUint32(payload[4:8], uint32(status.SupplyMV))
	binary.LittleEndian.PutUint32(payload[8:12], uint32(status.BusMV))
	binary.LittleEndian.PutUint32(payload[12:16], uint32(status.CurrentMA))
	binary.LittleEndian.PutUint32(payload[16:20], uint32(status.PowerMW))
	binary.LittleEndian.PutUint16(payload[20:22], uint16(status.TLEDCenti))
	binary.LittleEndian.PutUint16(payload[22:24], uint16(status.TBTCenti))
	binary.LittleEndian.PutUint16(payload[24:26], status.Flags)
	if status.ProgramRunning {
		payload[26] |= 1 << 0
	}
	if status.HostOffline {
		payload[26] |= 1 << 1
	}
	if status.Hot {
		payload[26] |= 1 << 2
	}
	if status.DoorOpen {
		payload[26] |= 1 << 3
	}
	if status.PWMAvailable {
		payload[26] |= 1 << 4
	}
	payload[27] = status.RawInputs
	payload[28] = status.ActiveKeys
	payload[29] = status.ActiveRelays
	payload[30] = status.MenuPage
	payload[31] = status.ProgramMode
	payload[32] = status.BluetoothState
	payload[33] = status.PWMChannel
	binary.LittleEndian.PutUint16(payload[34:36], status.PWMValue)
	payload[36] = status.LCDAddress
	payload[37] = status.PWMErrors
	binary.LittleEndian.PutUint16(payload[38:40], status.FramingErrors)
	binary.LittleEndian.PutUint16(payload[40:42], status.CRCErrors)
	payload[42] = status.ResetCause
	binary.LittleEndian.PutUint32(payload[43:47], status.ResetCount)
	record := measurementRecord{
		TimeMS: sample.Time.UnixMilli(),
		Status: base64.RawStdEncoding.EncodeToString(payload),
	}
	line, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode measurement history: %w", err)
	}
	return append(line, '\n'), nil
}

func unmarshalMeasurementSample(line []byte) (StatusSample, error) {
	var record measurementRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return StatusSample{}, err
	}
	if record.TimeMS <= 0 {
		return StatusSample{}, errors.New("measurement timestamp must be positive")
	}
	payload, err := base64.RawStdEncoding.DecodeString(record.Status)
	if err != nil || len(payload) != measurementStatusBytes {
		return StatusSample{}, errors.New("measurement status payload has invalid encoding or length")
	}
	if payload[26]&^byte(0x1F) != 0 {
		return StatusSample{}, errors.New("measurement boolean flags contain reserved bits")
	}
	status := native.Status{
		UptimeMS:       binary.LittleEndian.Uint32(payload[0:4]),
		SupplyMV:       int32(binary.LittleEndian.Uint32(payload[4:8])),
		BusMV:          int32(binary.LittleEndian.Uint32(payload[8:12])),
		CurrentMA:      int32(binary.LittleEndian.Uint32(payload[12:16])),
		PowerMW:        int32(binary.LittleEndian.Uint32(payload[16:20])),
		TLEDCenti:      int16(binary.LittleEndian.Uint16(payload[20:22])),
		TBTCenti:       int16(binary.LittleEndian.Uint16(payload[22:24])),
		Flags:          binary.LittleEndian.Uint16(payload[24:26]),
		ProgramRunning: payload[26]&(1<<0) != 0,
		HostOffline:    payload[26]&(1<<1) != 0,
		Hot:            payload[26]&(1<<2) != 0,
		DoorOpen:       payload[26]&(1<<3) != 0,
		PWMAvailable:   payload[26]&(1<<4) != 0,
		RawInputs:      payload[27],
		ActiveKeys:     payload[28],
		ActiveRelays:   payload[29],
		MenuPage:       payload[30],
		ProgramMode:    payload[31],
		BluetoothState: payload[32],
		PWMChannel:     payload[33],
		PWMValue:       binary.LittleEndian.Uint16(payload[34:36]),
		LCDAddress:     payload[36],
		PWMErrors:      payload[37],
		FramingErrors:  binary.LittleEndian.Uint16(payload[38:40]),
		CRCErrors:      binary.LittleEndian.Uint16(payload[40:42]),
		ResetCause:     payload[42],
		ResetCount:     binary.LittleEndian.Uint32(payload[43:47]),
	}
	return StatusSample{Time: time.UnixMilli(record.TimeMS).UTC(), Status: status}, nil
}

func loadStatusHistory(path string, retention time.Duration) ([]StatusSample, error) {
	if path == "" || retention == 0 {
		return nil, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open measurement history: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect measurement history: %w", err)
	}
	offset := info.Size() - maxMeasurementHistoryBytes
	if offset < 0 {
		offset = 0
	}
	if offset != 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek measurement history: %w", err)
		}
	}
	reader := bufio.NewReaderSize(file, maxMeasurementLineBytes)
	if offset != 0 {
		_, _ = readBoundedMeasurementLine(reader)
	}
	cutoff := time.Now().Add(-retention)
	result := make([]StatusSample, 0, 1024)
	for {
		line, readErr := readBoundedMeasurementLine(reader)
		line = bytes.TrimSpace(line)
		if len(line) != 0 {
			sample, decodeErr := unmarshalMeasurementSample(line)
			if decodeErr == nil && !sample.Time.Before(cutoff) {
				result = append(result, sample)
				if len(result) > maxMeasurementSamples {
					result = append(
						[]StatusSample(nil),
						result[len(result)-maxMeasurementSamples:]...,
					)
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read measurement history: %w", readErr)
		}
	}
	return result, nil
}

func readBoundedMeasurementLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 256)
	overflow := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if !overflow {
			if len(line)+len(fragment) <= maxMeasurementLineBytes {
				line = append(line, fragment...)
			} else {
				overflow = true
				line = nil
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}

func latestStatusSampleTime(samples []StatusSample) time.Time {
	if len(samples) == 0 {
		return time.Time{}
	}
	return samples[len(samples)-1].Time
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
