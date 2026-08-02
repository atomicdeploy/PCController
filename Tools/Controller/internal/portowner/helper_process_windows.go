//go:build windows

package portowner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	ownerHelperFoundTTL    = 750 * time.Millisecond
	ownerHelperNegativeTTL = 2 * time.Second
	ownerHelperWaitDelay   = 250 * time.Millisecond
	maxOwnerHelperCache    = 32
)

type ownerHelperQuery interface {
	FindOwner(context.Context, string) (Owner, bool, error)
}

type ownerHelperCommand func(context.Context, string, string) ([]byte, []byte, error)

type helperCacheEntry struct {
	owner   Owner
	found   bool
	err     error
	expires time.Time
}

type processOwnerHelper struct {
	executable func() (string, error)
	command    ownerHelperCommand
	gate       chan struct{}

	mu    sync.Mutex
	cache map[string]helperCacheEntry
}

func newProcessOwnerHelper() *processOwnerHelper {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return &processOwnerHelper{
		executable: os.Executable,
		command:    runOwnerHelperCommand,
		gate:       gate,
		cache:      make(map[string]helperCacheEntry),
	}
}

func (helper *processOwnerHelper) FindOwner(ctx context.Context, port string) (Owner, bool, error) {
	if helper == nil {
		return Owner{}, false, errors.New("serial-owner helper is unavailable")
	}
	port = normalizeHelperPort(port)
	if !validHelperPort(port) {
		return Owner{}, false, errors.New("serial-owner helper requires an exact COM number")
	}
	if cached, ok := helper.cached(port); ok {
		return cached.owner, cached.found, cached.err
	}
	select {
	case <-ctx.Done():
		return Owner{}, false, ctx.Err()
	case <-helper.gate:
	}
	defer func() { helper.gate <- struct{}{} }()
	if cached, ok := helper.cached(port); ok {
		return cached.owner, cached.found, cached.err
	}
	executable, err := helper.executable()
	if err != nil {
		return Owner{}, false, fmt.Errorf("resolve canonical controller executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return Owner{}, false, fmt.Errorf("resolve canonical controller path: %w", err)
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = errors.New("path is not a regular file")
		}
		return Owner{}, false, fmt.Errorf("validate canonical controller executable: %w", err)
	}
	command := helper.command
	if command == nil {
		command = runOwnerHelperCommand
	}
	stdout, stderr, commandErr := command(ctx, executable, port)
	if ctxErr := ctx.Err(); ctxErr != nil {
		commandErr = ctxErr
	}
	if commandErr != nil {
		detail := strings.TrimSpace(truncateUTF8(sanitizeOwnerText(string(stderr)), maxOwnerHelperError))
		if detail != "" {
			commandErr = fmt.Errorf("%w: %s", commandErr, detail)
		}
		return Owner{}, false, fmt.Errorf("bounded serial-owner helper: %w", commandErr)
	}
	if strings.TrimSpace(string(stderr)) != "" {
		return Owner{}, false, errors.New("serial-owner helper wrote unexpected diagnostic output")
	}
	owner, found, decodeErr := decodeOwnerHelperResult(port, stdout)
	result := helperCacheEntry{owner: owner, found: found, err: decodeErr}
	ttl := ownerHelperNegativeTTL
	if found {
		ttl = ownerHelperFoundTTL
	}
	helper.store(port, result, ttl)
	return result.owner, result.found, result.err
}

func (helper *processOwnerHelper) cached(port string) (helperCacheEntry, bool) {
	helper.mu.Lock()
	defer helper.mu.Unlock()
	entry, ok := helper.cache[port]
	if !ok || time.Now().After(entry.expires) {
		delete(helper.cache, port)
		return helperCacheEntry{}, false
	}
	return entry, true
}

func (helper *processOwnerHelper) store(port string, entry helperCacheEntry, ttl time.Duration) {
	helper.mu.Lock()
	if len(helper.cache) >= maxOwnerHelperCache {
		var oldestPort string
		var oldestExpiry time.Time
		for candidate, cached := range helper.cache {
			if oldestPort == "" || cached.expires.Before(oldestExpiry) {
				oldestPort = candidate
				oldestExpiry = cached.expires
			}
		}
		delete(helper.cache, oldestPort)
	}
	entry.expires = time.Now().Add(ttl)
	helper.cache[port] = entry
	helper.mu.Unlock()
}

type cappedCommandBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *cappedCommandBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = buffer.buffer.Write(value)
	}
	if written > remaining {
		buffer.overflow = true
	}
	return written, nil
}

func runOwnerHelperCommand(
	ctx context.Context,
	executable, port string,
) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, executable, ownerHelperArgument, port)
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	command.WaitDelay = ownerHelperWaitDelay
	stdout := &cappedCommandBuffer{limit: maxOwnerHelperOutput}
	stderr := &cappedCommandBuffer{limit: maxOwnerHelperError}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.overflow || stderr.overflow {
		return stdout.buffer.Bytes(), stderr.buffer.Bytes(), errors.New("serial-owner helper exceeded its output limit")
	}
	return stdout.buffer.Bytes(), stderr.buffer.Bytes(), err
}
