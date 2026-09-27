//go:build linux

package ports

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func enrichPlatform(values []Info) []Info { return values }

func platformEnumerationSource() string {
	return "go.bug.st/serial detailed enumerator backed by Linux sysfs"
}

func watchPlatformChanges(ctx context.Context) (<-chan Change, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create Linux serial-device watcher: %w", err)
	}
	if err := watcher.Add("/dev"); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("watch /dev for serial devices: %w", err)
	}
	changes := make(chan Change, 1)
	go func() {
		defer close(changes)
		defer watcher.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
				// Enumeration remains authoritative. A watcher error triggers one
				// refresh while keeping the watch alive for recoverable inotify errors.
				emitLinuxPortChange(changes, "Linux /dev watcher reported an error")
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if linuxSerialDeviceEvent(event) {
					emitLinuxPortChange(changes, "Linux serial device topology changed")
				}
			}
		}
	}()
	return changes, nil
}

func linuxSerialDeviceEvent(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) == 0 {
		return false
	}
	name := strings.ToLower(filepath.Base(event.Name))
	for _, prefix := range []string{"ttyacm", "ttyusb", "ttys", "ttyama", "rfcomm"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return name == "serial"
}

func emitLinuxPortChange(changes chan<- Change, reason string) {
	select {
	case changes <- Change{At: time.Now(), Reason: reason}:
	default:
	}
}
