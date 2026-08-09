// Package hostfacts exposes bounded, read-only Windows host facts through a
// fixed catalog. Callers select a profile; they never submit WQL.
package hostfacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultQueryTimeout = 2500 * time.Millisecond
	MaxQueryTimeout     = 5 * time.Second
	CacheTTL            = 5 * time.Second
	MaxResultBytes      = 64 * 1024
	MaxCellBytes        = 512
	maxGlobalRows       = 32
)

// QueryDescriptor is safe to expose over IPC and documents the deliberately
// small diagnostics catalog without revealing implementation query strings.
type QueryDescriptor struct {
	Profile     string   `json:"profile"`
	Class       string   `json:"class"`
	Description string   `json:"description"`
	Columns     []string `json:"columns"`
	MaxRows     int      `json:"max_rows"`
}

// Result is a bounded, JSON-safe snapshot from one catalog profile.
type Result struct {
	Profile     string           `json:"profile"`
	Class       string           `json:"class"`
	Columns     []string         `json:"columns"`
	Rows        []map[string]any `json:"rows"`
	Truncated   bool             `json:"truncated"`
	Source      string           `json:"source"`
	CollectedAt time.Time        `json:"collected_at"`
	DurationMS  int64            `json:"duration_ms"`
}

// Provider is the narrow surface consumed by the terminal and authenticated
// IPC service. Implementations must reject unknown profiles.
type Provider interface {
	Query(context.Context, string) (Result, error)
}

type querySpec struct {
	QueryDescriptor
	wql string
}

var queryCatalog = map[string]querySpec{
	"computer": {
		QueryDescriptor: QueryDescriptor{
			Profile: "computer", Class: "Win32_ComputerSystem",
			Description: "computer manufacturer, model, memory, and processor topology",
			Columns:     []string{"Manufacturer", "Model", "SystemType", "TotalPhysicalMemory", "NumberOfLogicalProcessors"},
			MaxRows:     1,
		},
		wql: "SELECT Manufacturer, Model, SystemType, TotalPhysicalMemory, NumberOfLogicalProcessors FROM Win32_ComputerSystem",
	},
	"firmware": {
		QueryDescriptor: QueryDescriptor{
			Profile: "firmware", Class: "Win32_BIOS",
			Description: "firmware vendor, version, release date, and SMBIOS level",
			Columns:     []string{"Manufacturer", "SMBIOSBIOSVersion", "ReleaseDate", "SMBIOSMajorVersion", "SMBIOSMinorVersion"},
			MaxRows:     2,
		},
		wql: "SELECT Manufacturer, SMBIOSBIOSVersion, ReleaseDate, SMBIOSMajorVersion, SMBIOSMinorVersion FROM Win32_BIOS",
	},
	"serial": {
		QueryDescriptor: QueryDescriptor{
			Profile: "serial", Class: "Win32_SerialPort",
			Description: "serial ports and their current Windows device status",
			Columns:     []string{"DeviceID", "Name", "Description", "Status", "ConfigManagerErrorCode"},
			MaxRows:     32,
		},
		wql: "SELECT DeviceID, Name, Description, Status, ConfigManagerErrorCode FROM Win32_SerialPort",
	},
	"storage": {
		QueryDescriptor: QueryDescriptor{
			Profile: "storage", Class: "Win32_LogicalDisk",
			Description: "fixed-volume capacity and free space",
			Columns:     []string{"DeviceID", "DriveType", "FileSystem", "Size", "FreeSpace"},
			MaxRows:     16,
		},
		wql: "SELECT DeviceID, DriveType, FileSystem, Size, FreeSpace FROM Win32_LogicalDisk WHERE DriveType = 3",
	},
	"system": {
		QueryDescriptor: QueryDescriptor{
			Profile: "system", Class: "Win32_OperatingSystem",
			Description: "Windows version, architecture, boot time, and aggregate memory",
			Columns:     []string{"Caption", "Version", "BuildNumber", "OSArchitecture", "LastBootUpTime", "TotalVisibleMemorySize", "FreePhysicalMemory"},
			MaxRows:     1,
		},
		wql: "SELECT Caption, Version, BuildNumber, OSArchitecture, LastBootUpTime, TotalVisibleMemorySize, FreePhysicalMemory FROM Win32_OperatingSystem",
	},
}

// Catalog returns a stable copy of the strict query allowlist.
func Catalog() []QueryDescriptor {
	result := make([]QueryDescriptor, 0, len(queryCatalog))
	for _, spec := range queryCatalog {
		descriptor := spec.QueryDescriptor
		descriptor.Columns = append([]string(nil), descriptor.Columns...)
		result = append(result, descriptor)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Profile < result[right].Profile
	})
	return result
}

type backend interface {
	query(context.Context, querySpec) ([]map[string]any, bool, error)
}

type cacheEntry struct {
	result  Result
	errText string
	expires time.Time
}

// CachedProvider serializes native queries. If an underlying COM call ignores
// cancellation, it retains the one worker slot so repeated requests cannot
// create an unbounded number of blocked OS threads.
type CachedProvider struct {
	backend backend
	now     func() time.Time
	worker  chan struct{}

	mu    sync.Mutex
	cache map[string]cacheEntry
}

func newCachedProvider(value backend) *CachedProvider {
	return &CachedProvider{
		backend: value,
		now:     time.Now,
		worker:  make(chan struct{}, 1),
		cache:   make(map[string]cacheEntry),
	}
}

var defaultProvider Provider = newCachedProvider(nativeBackend{})

// Default returns the process-wide provider. Its short cache is shared by the
// terminal, REST, raw IPC, and WebSocket RPC surfaces.
func Default() Provider { return defaultProvider }

func normalizeProfile(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "system"
	}
	return value
}

func (provider *CachedProvider) Query(ctx context.Context, profile string) (Result, error) {
	profile = normalizeProfile(profile)
	spec, allowed := queryCatalog[profile]
	if !allowed {
		return Result{}, fmt.Errorf(
			"unknown host-facts profile %q; allowed: %s",
			profile,
			strings.Join(profileNames(), ", "),
		)
	}
	if provider == nil || provider.backend == nil {
		return Result{}, errors.New("host facts are unavailable")
	}

	queryContext, cancel := boundedContext(ctx)
	defer cancel()
	for {
		if cached, ok := provider.cached(profile); ok {
			return cached.result, cached.err
		}
		select {
		case provider.worker <- struct{}{}:
			// A waiter may acquire the worker immediately after the preceding
			// request populated the cache. Recheck before starting COM.
			if cached, ok := provider.cached(profile); ok {
				<-provider.worker
				return cached.result, cached.err
			}
			return provider.run(queryContext, spec)
		case <-queryContext.Done():
			return Result{}, fmt.Errorf("host-facts query %s: %w", profile, queryContext.Err())
		}
	}
}

type cachedResponse struct {
	result Result
	err    error
}

func (provider *CachedProvider) cached(profile string) (cachedResponse, bool) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	entry, ok := provider.cache[profile]
	if !ok || !provider.now().Before(entry.expires) {
		delete(provider.cache, profile)
		return cachedResponse{}, false
	}
	response := cachedResponse{result: cloneResult(entry.result)}
	if entry.errText != "" {
		response.err = errors.New(entry.errText)
	}
	return response, true
}

func (provider *CachedProvider) run(ctx context.Context, spec querySpec) (Result, error) {
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	started := provider.now()
	go func() {
		value := outcome{}
		defer func() {
			if recovered := recover(); recovered != nil {
				value.result = Result{}
				value.err = fmt.Errorf("host-facts native worker failed: %v", recovered)
			}
			provider.store(spec.Profile, value.result, value.err)
			<-provider.worker
			done <- value
		}()
		rows, backendTruncated, err := provider.backend.query(ctx, spec)
		value.err = err
		if value.err == nil {
			var boundedTruncated bool
			rows, boundedTruncated = sanitizeRows(spec, rows)
			value.result = Result{
				Profile: spec.Profile, Class: spec.Class,
				Columns: append([]string(nil), spec.Columns...), Rows: rows,
				Truncated: backendTruncated || boundedTruncated,
				Source:    "wmi", CollectedAt: provider.now().UTC(),
				DurationMS: maxInt64(0, provider.now().Sub(started).Milliseconds()),
			}
			value.result = boundResult(value.result)
		}
	}()
	select {
	case value := <-done:
		return cloneResult(value.result), value.err
	case <-ctx.Done():
		return Result{}, fmt.Errorf("host-facts query %s: %w", spec.Profile, ctx.Err())
	}
}

func (provider *CachedProvider) store(profile string, result Result, err error) {
	entry := cacheEntry{result: cloneResult(result), expires: provider.now().Add(CacheTTL)}
	if err != nil {
		entry.errText = err.Error()
		entry.expires = provider.now().Add(time.Second)
	}
	provider.mu.Lock()
	provider.cache[profile] = entry
	provider.mu.Unlock()
}

func boundedContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	timeout := DefaultQueryTimeout
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		} else if remaining <= MaxQueryTimeout {
			timeout = remaining
		} else {
			timeout = MaxQueryTimeout
		}
	}
	if timeout <= 0 {
		contextValue, cancel := context.WithCancel(parent)
		cancel()
		return contextValue, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

func profileNames() []string {
	values := make([]string, 0, len(queryCatalog))
	for profile := range queryCatalog {
		values = append(values, profile)
	}
	sort.Strings(values)
	return values
}

func sanitizeRows(spec querySpec, input []map[string]any) ([]map[string]any, bool) {
	rowLimit := spec.MaxRows
	if rowLimit <= 0 || rowLimit > maxGlobalRows {
		rowLimit = maxGlobalRows
	}
	result := make([]map[string]any, 0, minInt(len(input), rowLimit))
	truncated := len(input) > rowLimit
	totalBytes := 0
	for _, source := range input {
		if len(result) >= rowLimit {
			truncated = true
			break
		}
		row := make(map[string]any, len(spec.Columns))
		for _, column := range spec.Columns {
			value, exists := source[column]
			if !exists {
				continue
			}
			if sensitiveColumn(column) {
				row[column] = "[redacted]"
				continue
			}
			row[column] = normalizeValue(value)
		}
		encoded, err := json.Marshal(row)
		if err != nil || totalBytes+len(encoded) > MaxResultBytes {
			truncated = true
			break
		}
		totalBytes += len(encoded)
		result = append(result, row)
	}
	return result, truncated
}

func boundResult(result Result) Result {
	for {
		encoded, err := json.Marshal(result)
		if err == nil && len(encoded) <= MaxResultBytes {
			return result
		}
		if len(result.Rows) == 0 {
			return result
		}
		result.Rows = result.Rows[:len(result.Rows)-1]
		result.Truncated = true
	}
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case nil, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return typed
	case string:
		return sanitizeText(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return nil
	}
}

func sanitizeText(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToValidUTF8(value, "") {
		if unicode.IsControl(character) || bidiControl(character) {
			if character == '\t' || character == '\n' || character == '\r' {
				builder.WriteByte(' ')
			}
			continue
		}
		if builder.Len()+utf8.RuneLen(character) > MaxCellBytes {
			break
		}
		builder.WriteRune(character)
	}
	return strings.TrimSpace(builder.String())
}

func sensitiveColumn(column string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(column))
	for _, fragment := range []string{
		"serialnumber", "uuid", "identifyingnumber", "productkey",
		"macaddress", "ipaddress", "username", "domain", "commandline",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func bidiControl(character rune) bool {
	return (character >= 0x202A && character <= 0x202E) ||
		(character >= 0x2066 && character <= 0x2069) || character == 0x200E || character == 0x200F
}

func cloneResult(value Result) Result {
	value.Columns = append([]string(nil), value.Columns...)
	rows := make([]map[string]any, len(value.Rows))
	for index, source := range value.Rows {
		row := make(map[string]any, len(source))
		for key, item := range source {
			row[key] = item
		}
		rows[index] = row
	}
	value.Rows = rows
	return value
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
