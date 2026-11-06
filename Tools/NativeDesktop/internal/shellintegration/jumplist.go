package shellintegration

import (
	"errors"
	"fmt"
	"strings"
)

// jumpTask is deliberately static. Jump List tasks remain available while the
// app is stopped and must not depend on transient device state.
type jumpTask struct {
	title       string
	arguments   string
	description string
}

var jumpTasks = []jumpTask{
	{
		title:       "Toggle device",
		arguments:   "/toggle",
		description: "Toggle the PCController",
	},
	{
		title:       "Turn device on",
		arguments:   "/on",
		description: "Turn the PCController on",
	},
	{
		title:       "Turn device off",
		arguments:   "/off",
		description: "Turn the PCController off",
	},
	{
		title:       "Show PCController",
		arguments:   "/show",
		description: "Show the PCController window",
	},
	{
		title:       "Open settings",
		arguments:   "/settings",
		description: "Open PCController settings",
	},
	{
		title:       "Reconnect",
		arguments:   "/reconnect",
		description: "Reconnect PCController to the device",
	},
}

// PartialError reports that an opt-in shell operation completed one durable
// part but could not complete another. Callers can distinguish this from a
// total failure with IsPartial.
type PartialError struct {
	Completed string
	Failed    string
	Err       error
}

func (e *PartialError) Error() string {
	if e == nil {
		return "shellintegration: partial operation"
	}
	completed := strings.TrimSpace(e.Completed)
	failed := strings.TrimSpace(e.Failed)
	switch {
	case completed != "" && failed != "":
		return fmt.Sprintf(
			"shellintegration: %s; %s failed: %v",
			completed,
			failed,
			e.Err,
		)
	case failed != "":
		return fmt.Sprintf("shellintegration: %s failed: %v", failed, e.Err)
	default:
		return fmt.Sprintf("shellintegration: operation partially completed: %v", e.Err)
	}
}

func (e *PartialError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsPartial reports whether err describes a partially completed shell
// integration operation.
func IsPartial(err error) bool {
	var partial *PartialError
	return errors.As(err, &partial)
}
