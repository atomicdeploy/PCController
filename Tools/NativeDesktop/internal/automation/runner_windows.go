//go:build windows

package automation

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/model"
)

const (
	maxCapturedOutput = 64 << 10
	queueSize         = 32
)

type LogFunc func(message string)

// SystemEvent identifies Windows lifecycle changes that can trigger an
// explicitly configured automation script. Values are also exported through
// PCCONTROLLER_DESKTOP_EVENT and therefore form part of the automation contract.
type SystemEvent string

const (
	SystemEventLock          SystemEvent = "lock"
	SystemEventUnlock        SystemEvent = "unlock"
	SystemEventSuspend       SystemEvent = "suspend"
	SystemEventResume        SystemEvent = "resume"
	SystemEventNetworkChange SystemEvent = "network-change"
)

type Runner struct {
	prefix string
	mu     sync.RWMutex
	cfg    config.AutomationConfig
	log    LogFunc
	queue  chan job
	stop   chan struct{}
	done   chan struct{}
	closed sync.Once
}

type job struct {
	script   string
	event    string
	state    model.OutputState
	previous model.OutputState
	source   string
	message  string
	device   string
	at       time.Time
}

func New(cfg config.AutomationConfig, logger LogFunc) *Runner {
	return newRunner(cfg, logger, "PCCONTROLLER_DESKTOP")
}

// NewController adapts the donor queue to program modes and PCController's
// public event-variable identity, while retaining the standalone donor API.
func NewController(cfg config.AutomationConfig, logger LogFunc) *Runner {
	return newRunner(cfg, logger, "PCCONTROLLER")
}

func newRunner(cfg config.AutomationConfig, logger LogFunc, prefix string) *Runner {
	r := &Runner{
		prefix: prefix,
		cfg:    cfg,
		log:    logger,
		queue:  make(chan job, queueSize),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go r.run()
	return r
}

func (r *Runner) ApplyConfig(cfg config.AutomationConfig) {
	r.mu.Lock()
	r.cfg = cfg
	r.mu.Unlock()
}

func (r *Runner) Handle(event model.Event) {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()
	if !cfg.Enabled {
		return
	}

	var pending job
	switch event.Kind {
	case model.EventSnapshot:
		if event.InitialSync || event.Snapshot.State == model.StateUnknown ||
			event.PreviousState == event.Snapshot.State {
			return
		}
		pending.event = "state"
		if r.prefix == "PCCONTROLLER" {
			pending.event = "mode"
		}
		pending.state = event.Snapshot.State
		pending.previous = event.PreviousState
		pending.source = event.Snapshot.LastSource
		pending.device = event.Snapshot.DeviceURL
		if event.Snapshot.State == model.StateOn {
			pending.script = cfg.OnScript
		} else {
			pending.script = cfg.OffScript
		}
	case model.EventMessage:
		pending.event = "message"
		pending.script = cfg.MessageScript
		pending.message = event.Message
		pending.state = event.Snapshot.State
		pending.source = "websocket"
		if r.prefix == "PCCONTROLLER" {
			pending.source = "pccontroller"
		}
		pending.device = event.Snapshot.DeviceURL
	default:
		return
	}

	pending.script = strings.TrimSpace(pending.script)
	if pending.script == "" {
		return
	}
	pending.at = time.Now().UTC()

	r.enqueue(pending)
}

// HandleSystemEvent queues an opt-in script for a Windows session, power, or
// network-interface transition. It deliberately receives a snapshot instead
// of reaching back into the core, keeping the runner independent and making
// every script invocation describe the state observed with the event.
func (r *Runner) HandleSystemEvent(event SystemEvent, snapshot model.Snapshot) {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()
	if !cfg.Enabled {
		return
	}

	script, known := systemEventScript(cfg, event)
	if !known {
		r.logf("automation ignored unknown Windows event %q", event)
		return
	}
	script = strings.TrimSpace(script)
	if script == "" {
		return
	}

	r.enqueue(job{
		script:   script,
		event:    string(event),
		state:    snapshot.State,
		previous: snapshot.State,
		source:   "windows",
		device:   snapshot.DeviceURL,
		at:       time.Now().UTC(),
	})
}

func systemEventScript(
	cfg config.AutomationConfig,
	event SystemEvent,
) (string, bool) {
	switch event {
	case SystemEventLock:
		return cfg.LockScript, true
	case SystemEventUnlock:
		return cfg.UnlockScript, true
	case SystemEventSuspend:
		return cfg.SuspendScript, true
	case SystemEventResume:
		return cfg.ResumeScript, true
	case SystemEventNetworkChange:
		return cfg.NetworkChangeScript, true
	default:
		return "", false
	}
}

func (r *Runner) enqueue(pending job) {
	select {
	case r.queue <- pending:
	default:
		r.logf("automation queue is full; skipped %s script", pending.event)
	}
}

func (r *Runner) Close() {
	r.closed.Do(func() {
		close(r.stop)
		<-r.done
	})
}

func (r *Runner) run() {
	defer close(r.done)
	var lastKey string
	var lastRun time.Time

	for {
		select {
		case <-r.stop:
			return
		case pending := <-r.queue:
			key := pending.script + "\x00" + pending.event + "\x00" + pending.state.String()
			if key == lastKey && time.Since(lastRun) < 750*time.Millisecond {
				continue
			}
			lastKey, lastRun = key, time.Now()
			r.execute(pending)
		}
	}
}

func (r *Runner) execute(pending job) {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()

	timeout := cfg.Timeout.Value()
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	command, err := scriptCommand(ctx, pending.script)
	if err != nil {
		r.logf("automation: %v", err)
		return
	}
	if cfg.WorkingDirectory != "" {
		command.Dir = cfg.WorkingDirectory
	}
	command.Env = append(os.Environ(), r.eventEnvironment(pending)...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var output *limitedWriter
	if cfg.CaptureOutput {
		output = &limitedWriter{remaining: maxCapturedOutput}
		command.Stdout = output
		command.Stderr = output
	} else {
		command.Stdout = io.Discard
		command.Stderr = io.Discard
	}

	started := time.Now()
	err = command.Run()
	elapsed := time.Since(started).Round(time.Millisecond)
	if ctx.Err() == context.DeadlineExceeded {
		r.logf("automation %s script timed out after %s", pending.event, timeout)
		return
	}
	if err != nil {
		r.logf("automation %s script failed after %s: %v%s",
			pending.event, elapsed, err, formatOutput(output))
		return
	}
	r.logf("automation %s script completed in %s%s",
		pending.event, elapsed, formatOutput(output))
}

func (r *Runner) eventEnvironment(pending job) []string {
	prefix := r.prefix
	if prefix == "" {
		prefix = "PCCONTROLLER_DESKTOP"
	}
	stateName := func(state model.OutputState) string {
		if r.prefix == "PCCONTROLLER" {
			if state == model.StateOn {
				return "running"
			}
			if state == model.StateOff {
				return "idle"
			}
		}
		return state.String()
	}
	return []string{
		prefix + "_EVENT=" + pending.event,
		prefix + "_STATE=" + stateName(pending.state),
		prefix + "_PREVIOUS_STATE=" + stateName(pending.previous),
		prefix + "_SOURCE=" + pending.source,
		prefix + "_MESSAGE=" + pending.message,
		prefix + "_DEVICE_URL=" + pending.device,
		prefix + "_EVENT_TIME=" + pending.at.Format(time.RFC3339Nano),
	}
}

func scriptCommand(ctx context.Context, script string) (*exec.Cmd, error) {
	path, err := filepath.Abs(os.ExpandEnv(script))
	if err != nil {
		return nil, fmt.Errorf("resolve script path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open script %q: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("script %q is a directory", path)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".ps1":
		return exec.CommandContext(
			ctx,
			"powershell.exe",
			"-NoLogo",
			"-NoProfile",
			"-NonInteractive",
			"-ExecutionPolicy", "Bypass",
			"-File", path,
		), nil
	case ".cmd", ".bat":
		commandProcessor := os.Getenv("ComSpec")
		if commandProcessor == "" {
			commandProcessor = "cmd.exe"
		}
		return exec.CommandContext(ctx, commandProcessor, "/d", "/s", "/c", path), nil
	case ".exe", ".com":
		return exec.CommandContext(ctx, path), nil
	default:
		return nil, fmt.Errorf(
			"unsupported script type %q (use .ps1, .cmd, .bat, .exe, or .com)",
			filepath.Ext(path),
		)
	}
}

type limitedWriter struct {
	data      strings.Builder
	remaining int
	truncated bool
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	original := len(data)
	if len(data) > w.remaining {
		data = data[:w.remaining]
		w.truncated = true
	}
	_, _ = w.data.Write(data)
	w.remaining -= len(data)
	return original, nil
}

func formatOutput(output *limitedWriter) string {
	if output == nil {
		return ""
	}
	text := strings.TrimSpace(output.data.String())
	if text == "" && !output.truncated {
		return ""
	}
	if output.truncated {
		text += "\n[output truncated]"
	}
	return "\n" + text
}

func (r *Runner) logf(format string, args ...any) {
	if r.log != nil {
		r.log(fmt.Sprintf(format, args...))
	}
}
