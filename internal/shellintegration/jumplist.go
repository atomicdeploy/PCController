package shellintegration

import (
	"errors"
	"fmt"
	"strings"
)

// jumpTask is deliberately static. Jump List tasks remain available while the
// app is stopped and must not depend on transient lamp state.
type jumpTask struct {
	title       string
	arguments   string
	description string
}

var jumpTasks = []jumpTask{
	{
		title:       "Toggle lamp",
		arguments:   "/toggle",
		description: "Toggle the Rayan Lamp",
	},
	{
		title:       "Turn lamp on",
		arguments:   "/on",
		description: "Turn the Rayan Lamp on",
	},
	{
		title:       "Turn lamp off",
		arguments:   "/off",
		description: "Turn the Rayan Lamp off",
	},
	{
		title:       "Show Rayan Lamp",
		arguments:   "/show",
		description: "Show the Rayan Lamp window",
	},
	{
		title:       "Open settings",
		arguments:   "/settings",
		description: "Open Rayan Lamp settings",
	},
	{
		title:       "Reconnect",
		arguments:   "/reconnect",
		description: "Reconnect Rayan Lamp to the device",
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
