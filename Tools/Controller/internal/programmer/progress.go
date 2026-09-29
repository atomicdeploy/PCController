package programmer

import (
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Progress reports work observed at the current stage, never an estimate of
// whole-transaction completion. Percent is -1 when the stage is indeterminate.
type Progress struct {
	Stage   string
	Percent int
	Detail  string
}

type progressKey struct{}
type progressReceiver struct {
	mu     sync.Mutex
	notify func(Progress)
}

// WithProgress installs a serialized callback. Receivers must return promptly
// and must not call a programmer operation synchronously from the callback.
func WithProgress(ctx context.Context, notify func(Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, &progressReceiver{notify: notify})
}

func HasProgress(ctx context.Context) bool {
	receiver, _ := ctx.Value(progressKey{}).(*progressReceiver)
	return receiver != nil && receiver.notify != nil
}

func ReportProgress(ctx context.Context, progress Progress) {
	receiver, _ := ctx.Value(progressKey{}).(*progressReceiver)
	if receiver == nil || receiver.notify == nil {
		return
	}
	if progress.Percent < -1 || progress.Percent > 100 {
		progress.Percent = -1
	}
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	receiver.notify(progress)
}

const progressRecordLimit = 1024

var avrdudeBar = regexp.MustCompile(`(?i)^\s*(Reading|Writing|Verifying|Erasing)\s*\|`)
var avrdudePercent = regexp.MustCompile(`\|\s*([0-9]{1,3})%`)

// AVRDUDE's non-TTY renderer emits one # per two percent and explicitly
// disables buffering for GUI consumers. A PTY is not required. Its TTY
// renderer instead rewrites complete bars using CR; both formats are handled.
// https://github.com/avrdudes/avrdude/blob/v8.0/src/term.c#L2908-L2942
type progressOutput struct {
	mu       sync.Mutex
	ctx      context.Context
	output   io.Writer
	stage    string
	record   []byte
	discard  bool
	tty      bool
	last     Progress
	haveLast bool
}

func (writer *progressOutput) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	n, err := writer.output.Write(data)
	for _, value := range data[:n] {
		if value == '\r' || value == '\n' {
			writer.tty = value == '\r'
			writer.record = writer.record[:0]
			writer.discard = false
			continue
		}
		if writer.discard {
			continue
		}
		if len(writer.record) == progressRecordLimit {
			writer.record = writer.record[:0]
			writer.discard = true
			continue
		}
		writer.record = append(writer.record, value)
		// Parse as bytes arrive, not only on newline: redirected AVRDUDE sends
		// hashes incrementally and prints its numeric percentage only at EOF.
		if value == '#' || value == '%' {
			writer.observe()
		}
	}
	return n, err
}

func (writer *progressOutput) observe() {
	line := string(writer.record)
	header := avrdudeBar.FindStringSubmatch(line)
	if header == nil {
		return
	}
	stage := strings.ToLower(header[1])
	bar := line[strings.IndexByte(line, '|')+1:]
	percent := -1
	if match := avrdudePercent.FindStringSubmatch(bar); match != nil {
		percent, _ = strconv.Atoi(match[1])
	} else if !writer.tty && !strings.ContainsRune(bar, '|') {
		// Only an uninterrupted hash stream is a pipe progress bar. Ignore
		// errors, arbitrary log numbers and completed bars until numeric %.
		trimmed := strings.TrimSpace(bar)
		if trimmed != "" && strings.Trim(trimmed, "#") == "" {
			percent = len(trimmed) * 2
		}
	}
	if percent < 0 || percent > 100 {
		return
	}
	progress := Progress{Stage: writer.stage + ":" + stage, Percent: percent, Detail: header[1]}
	if writer.haveLast && writer.last == progress {
		return
	}
	writer.last, writer.haveLast = progress, true
	ReportProgress(writer.ctx, progress)
}
