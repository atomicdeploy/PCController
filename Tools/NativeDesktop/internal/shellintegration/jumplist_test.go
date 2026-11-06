package shellintegration

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestJumpListTasksAreStaticBoundedCommands(t *testing.T) {
	t.Parallel()

	expected := []jumpTask{
		{"Toggle device", "/toggle", "Toggle the PCController"},
		{"Turn device on", "/on", "Turn the PCController on"},
		{"Turn device off", "/off", "Turn the PCController off"},
		{"Show PCController", "/show", "Show the PCController window"},
		{"Open settings", "/settings", "Open PCController settings"},
		{"Reconnect", "/reconnect", "Reconnect PCController to the device"},
	}
	if len(jumpTasks) != len(expected) {
		t.Fatalf("task count = %d, want %d", len(jumpTasks), len(expected))
	}
	seen := make(map[string]bool)
	for index, task := range jumpTasks {
		if task != expected[index] {
			t.Errorf("task %d = %+v, want %+v", index, task, expected[index])
		}
		if seen[task.arguments] {
			t.Errorf("duplicate task argument %q", task.arguments)
		}
		seen[task.arguments] = true
		if !strings.HasPrefix(task.arguments, "/") ||
			strings.ContainsAny(task.arguments, " \t\r\n\"") {
			t.Errorf("task has unsafe arguments: %+v", task)
		}
		if strings.TrimSpace(task.title) == "" ||
			strings.TrimSpace(task.description) == "" {
			t.Errorf("task lacks shell text: %+v", task)
		}
	}
}

func TestPartialErrorPreservesCauseAndClassification(t *testing.T) {
	t.Parallel()

	cause := errors.New("Jump Lists disabled")
	partial := &PartialError{
		Completed: "URI registered",
		Failed:    "Jump List installation",
		Err:       cause,
	}
	if !errors.Is(partial, cause) {
		t.Fatal("PartialError does not unwrap its cause")
	}
	if !IsPartial(partial) || !IsPartial(fmt.Errorf("wrapped: %w", partial)) {
		t.Fatal("partial failure was not classified through wrapping")
	}
	rendered := partial.Error()
	for _, text := range []string{"URI registered", "Jump List installation", cause.Error()} {
		if !strings.Contains(rendered, text) {
			t.Errorf("PartialError omitted %q: %s", text, rendered)
		}
	}
}
