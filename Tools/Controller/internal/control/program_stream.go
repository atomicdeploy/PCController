package control

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/programmer"
)

type programOperationContextKey struct{}

// WithProgramOperationID correlates a typed caller with the ordered progress
// events emitted by the shared CLI/TUI/API programming command.
func WithProgramOperationID(ctx context.Context, operationID string) context.Context {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return ctx
	}
	return context.WithValue(ctx, programOperationContextKey{}, operationID)
}

func programOperationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	operationID, _ := ctx.Value(programOperationContextKey{}).(string)
	return strings.TrimSpace(operationID)
}

func nextProgramOperationID(runtime *Runtime) string {
	return fmt.Sprintf(
		"program-%d-%d",
		time.Now().UTC().UnixMilli(),
		runtime.LatestEventID()+1,
	)
}

type programEventWriter struct {
	mu           sync.Mutex
	runtime      *Runtime
	operationID  string
	operation    string
	method       string
	replacements []programLogReplacement
	buffer       []byte
	sequence     uint64
}

type programLogReplacement struct {
	value string
	label string
}

func newProgramEventWriter(
	runtime *Runtime,
	operationID string,
	options programmer.Options,
) *programEventWriter {
	replacements := make([]programLogReplacement, 0, 6)
	add := func(value, label string) {
		value = strings.TrimSpace(value)
		if value == "" || value == "." {
			return
		}
		for _, existing := range replacements {
			if strings.EqualFold(existing.value, value) {
				return
			}
		}
		replacements = append(replacements, programLogReplacement{value: value, label: label})
	}
	add(options.CompileSourceRoot, "<firmware-source>")
	add(options.SketchPath, "<project>")
	add(options.BuildPath, "<build>")
	add(options.OutputDir, "<output>")
	add(options.OutputPath, "<output>")
	if home, err := os.UserHomeDir(); err == nil {
		add(home, "~")
	}
	sort.SliceStable(replacements, func(left, right int) bool {
		return len(replacements[left].value) > len(replacements[right].value)
	})
	return &programEventWriter{
		runtime: runtime, operationID: operationID,
		operation: string(options.Operation), method: string(options.Method),
		replacements: replacements,
	}
}

func (writer *programEventWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	written := len(data)
	writer.buffer = append(writer.buffer, data...)
	for {
		index := -1
		for current, value := range writer.buffer {
			if value == '\n' {
				index = current
				break
			}
		}
		if index < 0 {
			break
		}
		writer.publishLine(string(writer.buffer[:index]))
		writer.buffer = writer.buffer[index+1:]
	}
	return written, nil
}

func (writer *programEventWriter) Close() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.buffer) != 0 {
		writer.publishLine(string(writer.buffer))
		writer.buffer = nil
	}
}

func (writer *programEventWriter) publishLine(line string) {
	line = normalizeProgramLogLine(line, writer.replacements)
	if line == "" || isProgramLogNoise(line) {
		return
	}
	writer.sequence++
	writer.runtime.PublishStructuredEvent(Event{
		Kind: "program.output", Stream: EventStreamActivity, Text: line,
		Metadata: map[string]string{
			"operation_id": writer.operationID,
			"operation":    writer.operation,
			"method":       writer.method,
			"sequence":     strconv.FormatUint(writer.sequence, 10),
		},
	})
}

func normalizeProgramLogLine(line string, replacements []programLogReplacement) string {
	line = stripTerminalControls(strings.TrimSuffix(line, "\r"))
	for _, replacement := range replacements {
		line = strings.ReplaceAll(line, replacement.value, replacement.label)
		line = strings.ReplaceAll(
			line,
			strings.ReplaceAll(replacement.value, "\\", "/"),
			replacement.label,
		)
	}
	return strings.TrimSpace(line)
}

func stripTerminalControls(value string) string {
	var result strings.Builder
	for index := 0; index < len(value); {
		if value[index] == 0x1b {
			index++
			if index < len(value) && value[index] == '[' {
				index++
				for index < len(value) {
					current := value[index]
					index++
					if current >= 0x40 && current <= 0x7e {
						break
					}
				}
				continue
			}
			if index < len(value) {
				index++
			}
			continue
		}
		current := value[index]
		index++
		if current == '\t' || current >= 0x20 {
			result.WriteByte(current)
		}
	}
	return result.String()
}

func isProgramLogNoise(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, prefix := range []string{
		"alternatives for ",
		"detecting libraries used",
		"resolve library(",
		"using board ",
		"using cached library dependencies",
		"using core ",
		"using precompiled core",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// NormalizeProgramOutput applies the same bounded, machine-independent view
// used by remote progress events to the final typed RPC response. The ordinary
// local command still retains its complete diagnostic output.
func NormalizeProgramOutput(output string, paths ...string) string {
	replacements := make([]programLogReplacement, 0, len(paths)+1)
	for _, value := range paths {
		value = strings.TrimSpace(value)
		if value != "" && value != "." {
			replacements = append(replacements, programLogReplacement{
				value: value, label: "<project>",
			})
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		replacements = append(replacements, programLogReplacement{value: home, label: "~"})
	}
	sort.SliceStable(replacements, func(left, right int) bool {
		return len(replacements[left].value) > len(replacements[right].value)
	})
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = normalizeProgramLogLine(line, replacements)
		if line == "" || isProgramLogNoise(line) {
			continue
		}
		normalized = append(normalized, line)
	}
	return strings.Join(normalized, "\n")
}

func publishProgramPhase(
	runtime *Runtime,
	kind, operationID string,
	options programmer.Options,
	err error,
) {
	state := strings.TrimPrefix(kind, "program.")
	text := fmt.Sprintf("%s %s %s", options.Method, options.Operation, state)
	metadata := map[string]string{
		"operation_id": operationID,
		"operation":    string(options.Operation),
		"method":       string(options.Method),
		"state":        state,
	}
	if err != nil {
		metadata["error"] = normalizeProgramLogLine(err.Error(), nil)
	}
	runtime.PublishStructuredEvent(Event{
		Kind: kind, Stream: EventStreamActivity, Text: text, Metadata: metadata,
	})
}
