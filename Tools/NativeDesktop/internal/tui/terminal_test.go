package tui

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pccontroller.local/desktop/internal/deviceterminal"
)

type fakeTerminalTransport struct {
	*fakeTransport

	terminalMutex sync.Mutex
	terminal      deviceterminal.Snapshot
	statusCalls   int
	connectCalls  int
	disconnects   int
	reads         []terminalReadCall
	writes        [][]byte
	readResult    deviceterminal.ReadResult
	readErr       error
	writeErr      error
}

type terminalReadCall struct {
	after uint64
	limit int
	wait  time.Duration
}

func newFakeTerminalTransport() *fakeTerminalTransport {
	return &fakeTerminalTransport{
		fakeTransport: &fakeTransport{},
		terminal: deviceterminal.Snapshot{
			State:   deviceterminal.StateDisconnected,
			Host:    "pccontroller-desktop.local",
			Port:    deviceterminal.DefaultPort,
			Session: 1,
		},
	}
}

func (f *fakeTerminalTransport) TerminalStatus(
	context.Context,
) (deviceterminal.Snapshot, error) {
	f.terminalMutex.Lock()
	defer f.terminalMutex.Unlock()
	f.statusCalls++
	return f.terminal, nil
}

func (f *fakeTerminalTransport) TerminalConnect(
	context.Context,
) (deviceterminal.Snapshot, error) {
	f.terminalMutex.Lock()
	defer f.terminalMutex.Unlock()
	f.connectCalls++
	f.terminal.State = deviceterminal.StateConnected
	f.terminal.Session++
	return f.terminal, nil
}

func (f *fakeTerminalTransport) TerminalDisconnect(
	context.Context,
) (deviceterminal.Snapshot, error) {
	f.terminalMutex.Lock()
	defer f.terminalMutex.Unlock()
	f.disconnects++
	f.terminal.State = deviceterminal.StateDisconnected
	return f.terminal, nil
}

func (f *fakeTerminalTransport) TerminalRead(
	_ context.Context,
	after uint64,
	limit int,
	wait time.Duration,
) (deviceterminal.ReadResult, error) {
	f.terminalMutex.Lock()
	defer f.terminalMutex.Unlock()
	f.reads = append(f.reads, terminalReadCall{after: after, limit: limit, wait: wait})
	result := f.readResult
	if result.Snapshot.State == "" {
		result.Snapshot = f.terminal
	}
	return result, f.readErr
}

func (f *fakeTerminalTransport) TerminalWrite(
	_ context.Context,
	data []byte,
) (deviceterminal.Snapshot, error) {
	f.terminalMutex.Lock()
	defer f.terminalMutex.Unlock()
	f.writes = append(f.writes, append([]byte(nil), data...))
	return f.terminal, f.writeErr
}

func newTerminalTestModel(
	t *testing.T,
	transport Transport,
) *Model {
	t.Helper()
	result, err := NewModel(context.Background(), transport, ModelOptions{
		Poll:      time.Second,
		Timeout:   time.Second,
		Theme:     ThemeDark,
		AltScreen: true,
		Mouse:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.Update(tea.WindowSizeMsg{Width: 88, Height: 26})
	return result
}

func TestTerminalOverlayAutoConnectsAndDetachDoesNotDisconnect(t *testing.T) {
	transport := newFakeTerminalTransport()
	model := newTerminalTestModel(t, transport)

	_, command := model.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	if command == nil || !model.terminalActive() {
		t.Fatal("x did not open the terminal overlay")
	}
	message := command()
	_, readCommand := model.Update(message)
	if !model.terminalActive() ||
		model.terminal.snapshot.State != deviceterminal.StateConnected {
		t.Fatalf("terminal state = %+v", model.terminal.snapshot)
	}
	if transport.statusCalls != 1 || transport.connectCalls != 1 {
		t.Fatalf(
			"status/connect calls = %d/%d, want 1/1",
			transport.statusCalls,
			transport.connectCalls,
		)
	}
	if readCommand == nil || !model.terminal.readPending {
		t.Fatal("connected overlay did not begin its ordered output read")
	}

	view := model.View()
	if !strings.Contains(ansi.Strip(view.Content), "DEVICE TERMINAL") {
		t.Fatalf("terminal view is missing its identity: %q", ansi.Strip(view.Content))
	}
	plainView := ansi.Strip(view.Content)
	lines := strings.Split(plainView, "\n")
	if len(lines) > 26 {
		t.Errorf("terminal overlay height = %d, terminal height = 26", len(lines))
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > 88 {
			t.Fatalf("terminal overlay width = %d, terminal width = 88", width)
		}
	}
	_, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if model.terminalActive() {
		t.Fatal("Esc did not detach the terminal overlay")
	}
	if transport.disconnects != 0 {
		t.Fatalf("detach issued %d disconnect calls", transport.disconnects)
	}
}

func TestTerminalExplicitDisconnectAndInputQueue(t *testing.T) {
	transport := newFakeTerminalTransport()
	transport.terminal.State = deviceterminal.StateConnected
	model := newTerminalTestModel(t, transport)

	_, openCommand := model.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	_, _ = model.Update(openCommand())

	_, writeCommand := model.Update(tea.KeyPressMsg(tea.Key{Code: 'λ', Text: "λ"}))
	if writeCommand == nil {
		t.Fatal("printable key did not start a terminal write")
	}
	_, next := model.Update(writeCommand())
	if next != nil {
		t.Fatal("unexpected second input batch")
	}
	if len(transport.writes) != 1 || string(transport.writes[0]) != "λ" {
		t.Fatalf("writes = %q", transport.writes)
	}

	_, disconnectCommand := model.Update(tea.KeyPressMsg(tea.Key{
		Code: 'd',
		Mod:  tea.ModCtrl,
	}))
	if disconnectCommand == nil {
		t.Fatal("Ctrl+D did not start an explicit disconnect")
	}
	_, _ = model.Update(disconnectCommand())
	if transport.disconnects != 1 {
		t.Fatalf("disconnect calls = %d, want 1", transport.disconnects)
	}
	if !model.terminalActive() {
		t.Fatal("explicit disconnect unexpectedly closed the overlay")
	}
	if model.terminal.snapshot.State != deviceterminal.StateDisconnected {
		t.Fatalf("snapshot state = %q", model.terminal.snapshot.State)
	}
}

func TestReadOnlyTerminalSuppressesKeyForwarding(t *testing.T) {
	transport := newFakeTerminalTransport()
	transport.terminal.State = deviceterminal.StateConnected
	transport.terminal.ReadOnly = true
	model := newTerminalTestModel(t, transport)

	_, openCommand := model.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	_, _ = model.Update(openCommand())
	_, writeCommand := model.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	if writeCommand != nil {
		t.Fatal("read-only terminal started a write command")
	}
	if len(transport.writes) != 0 {
		t.Fatalf("read-only terminal forwarded %d writes", len(transport.writes))
	}
	if got := ansi.Strip(model.View().Content); !strings.Contains(
		got,
		"ShellOnSerial · output only",
	) {
		t.Fatalf("read-only notice missing from view: %q", got)
	}
	model.closeTerminal()
}

func TestTerminalReadOrdersChunksAndFencesSessions(t *testing.T) {
	transport := newFakeTerminalTransport()
	model := newTerminalTestModel(t, transport)
	model.terminal.active = true
	model.terminal.requestID = 9
	model.terminal.readPending = true
	model.terminal.session = 3
	model.terminal.after = 20
	model.terminal.raw = []byte("stale output")
	model.terminal.snapshot = deviceterminal.Snapshot{
		State:   deviceterminal.StateConnected,
		Session: 3,
	}

	message := terminalReadResultMsg{
		requestID: 9,
		session:   3,
		result: deviceterminal.ReadResult{
			Snapshot: deviceterminal.Snapshot{
				State:   deviceterminal.StateConnected,
				Session: 4,
			},
			Chunks: []deviceterminal.Chunk{
				{
					Sequence:   2,
					DataBase64: base64.StdEncoding.EncodeToString([]byte("second")),
				},
				{
					Sequence:   1,
					DataBase64: base64.StdEncoding.EncodeToString([]byte("first ")),
				},
				{
					Sequence:   2,
					DataBase64: base64.StdEncoding.EncodeToString([]byte("duplicate")),
				},
			},
		},
	}
	_, command := model.Update(message)
	defer model.closeTerminal()
	if command == nil {
		t.Fatal("connected read did not schedule the next long poll")
	}
	if got := string(model.terminal.raw); got != "first second" {
		t.Fatalf("ordered transcript = %q", got)
	}
	if model.terminal.after != 2 || model.terminal.session != 4 {
		t.Fatalf(
			"cursor/session = %d/%d, want 2/4",
			model.terminal.after,
			model.terminal.session,
		)
	}
	if strings.Contains(string(model.terminal.raw), "stale") {
		t.Fatal("previous session survived session fencing")
	}
}

func TestTerminalKeyEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  tea.Key
		want string
	}{
		{name: "printable", key: tea.Key{Code: 'A', Text: "A"}, want: "A"},
		{name: "enter", key: tea.Key{Code: tea.KeyEnter}, want: "\r\n"},
		{name: "backspace", key: tea.Key{Code: tea.KeyBackspace}, want: "\x08"},
		{name: "delete", key: tea.Key{Code: tea.KeyDelete}, want: "\x1b[3~"},
		{name: "up", key: tea.Key{Code: tea.KeyUp}, want: "\x1b[A"},
		{name: "home", key: tea.Key{Code: tea.KeyHome}, want: "\x1b[H"},
		{name: "end", key: tea.Key{Code: tea.KeyEnd}, want: "\x1b[F"},
		{
			name: "interrupt",
			key:  tea.Key{Code: 'c', Mod: tea.ModCtrl},
			want: "\x03",
		},
		{
			name: "clear",
			key:  tea.Key{Code: 'l', Mod: tea.ModCtrl},
			want: "\x0c",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := terminalKeyBytes(tea.KeyPressMsg(test.key))
			if string(got) != test.want {
				t.Errorf("encoded key = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSanitizeDeviceTerminalDropsActiveControls(t *testing.T) {
	setColorCapableTestTerminal(t)

	raw := []byte(
		"hello\x1b]52;c;clipboard-secret\a world" +
			"\x1b[2Jclear\x1b[31m red\x1b[0m" +
			"\x1bPmalicious-dcs\x1b\\\u202e!",
	)
	got := sanitizeDeviceTerminal(raw)
	for _, forbidden := range []string{
		"clipboard-secret",
		"malicious-dcs",
		"\x1b]52",
		"\x1b[2J",
		"\u202e",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("unsafe terminal content survived: %q in %q", forbidden, got)
		}
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("safe SGR styling was discarded: %q", got)
	}
	plain := ansi.Strip(got)
	for _, wanted := range []string{"hello world", "clear", "red", "!"} {
		if !strings.Contains(plain, wanted) {
			t.Errorf("readable text %q missing from %q", wanted, plain)
		}
	}
}

func TestSanitizeDeviceTerminalStripsSGRForNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	got := sanitizeDeviceTerminal([]byte("plain \x1b[1;31mred\x1b[0m"))
	if strings.Contains(got, "\x1b") {
		t.Fatalf("NO_COLOR terminal transcript contains escape bytes: %q", got)
	}
	if got != "plain red" {
		t.Fatalf("NO_COLOR terminal transcript = %q, want plain text", got)
	}
}

func TestAPITransportTerminalRoutes(t *testing.T) {
	t.Parallel()

	var (
		mutex    sync.Mutex
		requests []string
		input    string
	)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		mutex.Lock()
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/terminal/input" {
			body, _ := io.ReadAll(request.Body)
			input = string(body)
		}
		if request.URL.Path == "/terminal/output" {
			_, _ = io.WriteString(writer, `{"ok":true,"result":{
				"snapshot":{"state":"connected","host":"device","port":23,"session":7},
				"chunks":[]}}`)
			return
		}
		_, _ = io.WriteString(writer, `{"ok":true,"result":{
			"state":"connected","host":"device","port":23,"session":7}}`)
	}))
	defer server.Close()

	transport, err := newAPITransport(
		server.URL,
		transportTestToken,
		2*time.Second,
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := transport.TerminalStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.TerminalConnect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.TerminalRead(ctx, 41, 1024, 750*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.TerminalWrite(ctx, []byte("help\r\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.TerminalDisconnect(ctx); err != nil {
		t.Fatal(err)
	}

	mutex.Lock()
	gotRequests := append([]string(nil), requests...)
	mutex.Unlock()
	if len(gotRequests) != 5 {
		t.Fatalf("requests = %#v", gotRequests)
	}
	queryURL, err := url.Parse(gotRequests[2][len(http.MethodGet)+1:])
	if err != nil {
		t.Fatal(err)
	}
	if queryURL.Path != "/terminal/output" ||
		queryURL.Query().Get("after") != "41" ||
		queryURL.Query().Get("limit") != "1024" ||
		queryURL.Query().Get("wait_ms") != "750" {
		t.Errorf("terminal read request = %q", gotRequests[2])
	}
	if !strings.Contains(input, base64.StdEncoding.EncodeToString([]byte("help\r\n"))) {
		t.Errorf("terminal input body = %q", input)
	}
}
