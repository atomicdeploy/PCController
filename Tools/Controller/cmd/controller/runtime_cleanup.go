package main

import (
	"errors"
	"fmt"
	"sync"
)

type commandRuntimeCloser interface {
	Close() error
}

type retainedCommandRuntime struct {
	owner   commandRuntimeCloser
	close   func() error
	context string
}

var commandRuntimeCleanups struct {
	sync.Mutex
	owners []retainedCommandRuntime
}

// closeCommandRuntime keeps a retryable serial owner process-reachable when
// Close cannot cancel its pending I/O. A later command cleanup pass retries the
// exact owner; successful close removes it from quarantine.
func closeCommandRuntime(
	owner commandRuntimeCloser,
	close func() error,
	context string,
) error {
	if owner == nil || close == nil {
		return nil
	}
	err := close()
	commandRuntimeCleanups.Lock()
	defer commandRuntimeCleanups.Unlock()
	for index, retained := range commandRuntimeCleanups.owners {
		if retained.owner != owner {
			continue
		}
		if err == nil {
			commandRuntimeCleanups.owners = append(
				commandRuntimeCleanups.owners[:index],
				commandRuntimeCleanups.owners[index+1:]...,
			)
		} else {
			commandRuntimeCleanups.owners[index] = retainedCommandRuntime{
				owner: owner, close: close, context: context,
			}
		}
		return err
	}
	if err != nil {
		commandRuntimeCleanups.owners = append(
			commandRuntimeCleanups.owners,
			retainedCommandRuntime{owner: owner, close: close, context: context},
		)
	}
	return err
}

// drainCommandRuntimeCleanups deterministically retries every quarantined
// command-local Runtime before run returns. Owners whose close still fails stay
// retained until another drain or process termination releases the OS handle.
func drainCommandRuntimeCleanups() error {
	commandRuntimeCleanups.Lock()
	owners := append([]retainedCommandRuntime(nil), commandRuntimeCleanups.owners...)
	commandRuntimeCleanups.Unlock()

	var result error
	for _, retained := range owners {
		if err := closeCommandRuntime(retained.owner, retained.close, retained.context); err != nil {
			result = errors.Join(result, fmt.Errorf("%s: %w", retained.context, err))
		}
	}
	return result
}

func retainedCommandRuntimeCount() int {
	commandRuntimeCleanups.Lock()
	defer commandRuntimeCleanups.Unlock()
	return len(commandRuntimeCleanups.owners)
}
