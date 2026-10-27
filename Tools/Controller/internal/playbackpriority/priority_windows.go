//go:build windows

package playbackpriority

import "golang.org/x/sys/windows"

var processManager = manager{
	get: func() (uint32, error) { return windows.GetPriorityClass(windows.CurrentProcess()) },
	set: func(priority uint32) error { return windows.SetPriorityClass(windows.CurrentProcess(), priority) },
}

// Acquire raises only this process while at least one prepared serial owner
// holds the lease. It never requests realtime priority or changes OS policy.
func Acquire() (func() error, error) { return processManager.acquire() }
