package tui

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

type synchronizedBuffer struct {
	mutex sync.Mutex
	data  bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(content []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.data.Write(content)
}

func (buffer *synchronizedBuffer) snapshot() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.data.String()
}

func (buffer *synchronizedBuffer) length() int {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.data.Len()
}

func TestProgramConsumesRealKeyBytesAcrossEveryWorkspace(t *testing.T) {
	model, _ := newWorkspaceModelAt(t, ScreenDashboard, 100, 30)
	model.options.Mouse = false
	input, writer := io.Pipe()
	output := &synchronizedBuffer{}
	program := tea.NewProgram(
		newRuntimeModel(model),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithoutSignalHandler(),
		tea.WithWindowSize(100, 30),
		tea.WithColorProfile(colorprofile.TrueColor),
	)
	finished := make(chan error, 1)
	go func() {
		_, err := program.Run()
		finished <- err
	}()
	t.Cleanup(func() {
		_ = writer.Close()
		program.Quit()
		select {
		case <-finished:
		case <-time.After(time.Second):
		}
	})

	waitForProgramText(t, output, 0, "RAYAN / LAMP")
	steps := []struct {
		name string
		key  string
		want string
	}{
		{name: "commands", key: ":", want: "COMMAND CENTER"},
		{name: "commands close", key: "\x1b", want: "RAYAN / LAMP"},
		{name: "settings", key: "s", want: "APPLICATION SETTINGS"},
		{name: "settings close", key: "\x1b", want: "RAYAN / LAMP"},
		{name: "device", key: "v", want: "DEVICE SETTINGS"},
		{name: "device close", key: "\x1b", want: "RAYAN / LAMP"},
		{name: "serving", key: "w", want: "SERVING & INTEGRATION"},
		{name: "serving categories", key: "\x1b", want: "APPLICATION SETTINGS"},
		{name: "serving close", key: "\x1b", want: "RAYAN / LAMP"},
		{name: "diagnostics", key: "d", want: "DIAGNOSTICS"},
		{name: "diagnostics close", key: "\x1b", want: "RAYAN / LAMP"},
		{name: "terminal", key: "x", want: "DEVICE TERMINAL"},
		{name: "terminal detach", key: "\x1b", want: "RAYAN / LAMP"},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			offset := output.length()
			if _, err := io.WriteString(writer, step.key); err != nil {
				t.Fatalf("write key bytes: %v", err)
			}
			waitForProgramText(t, output, offset, step.want)
		})
	}

	if _, err := io.WriteString(writer, "q"); err != nil {
		t.Fatalf("write quit key: %v", err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("program stopped with error: %v", err)
		}
	case <-time.After(3 * time.Second):
		program.Quit()
		t.Fatal("program did not respond to q within three seconds")
	}

	rendered := output.snapshot()
	for _, pair := range []struct {
		name    string
		enter   string
		restore string
	}{
		{
			name:    "alternate screen",
			enter:   "\x1b[?1049h",
			restore: "\x1b[?1049l",
		},
		{
			name:    "cursor visibility",
			enter:   "\x1b[?25l",
			restore: "\x1b[?25h",
		},
	} {
		if entered, restored := strings.Count(rendered, pair.enter),
			strings.Count(rendered, pair.restore); entered == 0 || entered != restored {
			t.Fatalf(
				"%s control balance = enter %d, restore %d",
				pair.name,
				entered,
				restored,
			)
		}
	}
	for _, legacyColor := range []string{
		"\x1b[30m",
		"\x1b[37m",
		"\x1b[40m",
		"\x1b[47m",
	} {
		if strings.Contains(rendered, legacyColor) {
			t.Fatalf("program emitted legacy black/white style %q", legacyColor)
		}
	}
}

func waitForProgramText(
	t *testing.T,
	output *synchronizedBuffer,
	offset int,
	want string,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rendered := output.snapshot()
		if offset < len(rendered) && strings.Contains(rendered[offset:], want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	rendered := output.snapshot()
	if offset > len(rendered) {
		offset = len(rendered)
	}
	t.Fatalf("program did not render %q; tail:\n%s", want, rendered[offset:])
}
