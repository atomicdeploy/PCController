package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestValidateDiagnosticEndpointRequiresCleanAbsolutePath(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{
		"/info",
		"/status/full",
		"/api/v1/device-info",
		"/metrics.json",
	} {
		if err := validateDiagnosticEndpoint(endpoint); err != nil {
			t.Errorf("valid endpoint %q rejected: %v", endpoint, err)
		}
	}

	for _, test := range []struct {
		endpoint string
		want     string
	}{
		{"", "absolute endpoint"},
		{" info", "whitespace"},
		{"/info ", "whitespace"},
		{"info", "absolute path"},
		{"//server/info", "one /"},
		{"/info?full=1", "query or fragment"},
		{"/info#raw", "query or fragment"},
		{"/diagnostics/../info", "already be clean"},
		{"/diagnostics/./info", "already be clean"},
		{"/info\\raw", "clean URL path"},
		{"/info raw", "clean URL path"},
	} {
		test := test
		t.Run(test.endpoint, func(t *testing.T) {
			t.Parallel()
			err := validateDiagnosticEndpoint(test.endpoint)
			if err == nil {
				t.Fatalf("unsafe endpoint %q was accepted", test.endpoint)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestDiagnosticDocumentFormatsJSONAndSanitizesContent(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("CLICOLOR", "")
	t.Setenv("FORCE_COLOR", "")

	dark, err := renderDiagnosticDocument(
		`{"device":{"state":"on"},"reachable":true}`,
		64,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	light, err := renderDiagnosticDocument(
		`{"device":{"state":"on"},"reachable":true}`,
		64,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if dark == light {
		t.Error("dark and light diagnostic themes rendered identically")
	}
	if !strings.Contains(dark, "\x1b[") || !strings.Contains(light, "\x1b[") {
		t.Error("colored diagnostic render omitted terminal styling")
	}
	rendered := ansi.Strip(dark)
	for _, fragment := range []string{"device", "state", "reachable", "true"} {
		if !strings.Contains(rendered, fragment) {
			t.Errorf("rendered JSON omitted %q:\n%s", fragment, rendered)
		}
	}
	if !strings.Contains(rendered, "\"device\": {\n") ||
		!strings.Contains(rendered, "\n  \"reachable\": true") {
		t.Errorf("JSON was not pretty-printed:\n%s", rendered)
	}

	hostile := "\x1b]52;c;payload\a\r\u0085safe"
	sanitized := sanitizeDiagnosticText(hostile)
	if strings.ContainsAny(sanitized, "\x1b\a\r\u0085") {
		t.Errorf("terminal controls survived sanitization: %q", sanitized)
	}
	for _, visible := range []string{`\x1b`, `\x07`, `\x0d`, `\u0085`, "safe"} {
		if !strings.Contains(sanitized, visible) {
			t.Errorf("sanitized output omitted %q: %q", visible, sanitized)
		}
	}

	hostileRendered, err := renderDiagnosticDocument(hostile, 32, true)
	if err != nil {
		t.Fatal(err)
	}
	hostilePlain := ansi.Strip(hostileRendered)
	if strings.ContainsAny(hostilePlain, "\x1b\a\r\u0085") {
		t.Errorf("terminal controls survived rendering: %q", hostilePlain)
	}
	hostileUnwrapped := strings.ReplaceAll(hostilePlain, "\n", "")
	for _, visible := range []string{`\x1b`, `\x07`, `\x0d`, `\u0085`, "safe"} {
		if !strings.Contains(hostileUnwrapped, visible) {
			t.Errorf("rendered output omitted %q: %q", visible, hostilePlain)
		}
	}

	longRendered, err := renderDiagnosticDocument(
		`{"long":"`+strings.Repeat("abcdef", 20)+`"}`,
		12,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(longRendered, "\n") {
		if width := lipgloss.Width(line); width > 12 {
			t.Errorf("wrapped line width = %d, want <= 12: %q", width, line)
		}
	}

	backticks, err := renderDiagnosticDocument("before\n```\nafter", 32, true)
	if err != nil {
		t.Fatal(err)
	}
	if plain := ansi.Strip(backticks); plain != "before\n```\nafter" {
		t.Errorf("plain text changed during rendering: %q", plain)
	}

	invalidUTF8 := string([]byte{'o', 'k', 0xff, 'x'})
	validRendered, err := renderDiagnosticDocument(invalidUTF8, 32, true)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(validRendered) ||
		!strings.Contains(ansi.Strip(validRendered), "ok�x") {
		t.Errorf("invalid UTF-8 was not repaired: %q", validRendered)
	}
}

func TestDiagnosticDocumentTruncatesAndHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	content := strings.Repeat("界", maxDiagnosticDisplayBytes)
	rendered, err := renderDiagnosticDocument(content, 48, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "\x1b") {
		t.Fatalf("NO_COLOR diagnostic contains escape bytes: %q", rendered[:64])
	}
	if !strings.Contains(rendered, "response display truncated at 256 KiB") {
		t.Error("large diagnostic response omitted truncation marker")
	}
	if !utf8.ValidString(rendered) {
		t.Error("truncated diagnostic response is not valid UTF-8")
	}

	empty, err := renderDiagnosticDocument(" \n\t ", 48, false)
	if err != nil {
		t.Fatal(err)
	}
	if empty != "The endpoint returned an empty response." {
		t.Errorf("empty diagnostic response = %q", empty)
	}
}

func TestDiagnosticsOverlayStateAndEscapeSemantics(t *testing.T) {
	model, transport := newTestModel(t)
	transport.diagnosticResult = DiagnosticResult{
		Content: `{"state":"on","source":"test"}`,
	}

	updated, command := model.Update(keyMessage('d', "d"))
	if updated != model || command == nil {
		t.Fatal("d did not initialize the embedded Huh form")
	}
	if model.diagnostics.mode != diagnosticForm || model.diagnostics.form == nil {
		t.Fatalf("diagnostics state = %+v", model.diagnostics)
	}

	updated, command = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if updated != model || command != nil {
		t.Fatal("Esc from the diagnostics form should close it without a quit command")
	}
	if model.diagnostics.mode != diagnosticClosed {
		t.Fatalf("Esc left diagnostics mode %d active", model.diagnostics.mode)
	}

	_ = model.openDiagnostics()
	fetch := model.beginDiagnosticFetch()
	if fetch == nil || model.diagnostics.mode != diagnosticLoading {
		t.Fatal("diagnostics fetch did not enter loading state")
	}
	resultMessage := fetch()
	_, _ = model.Update(resultMessage)
	if model.diagnostics.mode != diagnosticResult {
		t.Fatalf("completed fetch mode = %d, want result", model.diagnostics.mode)
	}
	if model.diagnostics.result.Endpoint != defaultDiagnosticEndpoint {
		t.Errorf("result endpoint = %q", model.diagnostics.result.Endpoint)
	}
	if !strings.Contains(model.diagnostics.viewport.View(), "state") {
		t.Error("rendered response was not installed in the viewport")
	}

	_, command = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if command != nil || model.diagnostics.mode != diagnosticClosed {
		t.Fatal("Esc from the result should close it without quitting")
	}

	transport.mutex.Lock()
	gotEndpoints := append([]string(nil), transport.diagnosticEndpoints...)
	transport.mutex.Unlock()
	if len(gotEndpoints) != 1 || gotEndpoints[0] != defaultDiagnosticEndpoint {
		t.Errorf("diagnostic endpoints = %v", gotEndpoints)
	}
}

func TestClosingDiagnosticsIgnoresLateFetchResult(t *testing.T) {
	model, transport := newTestModel(t)
	transport.diagnosticResult = DiagnosticResult{Content: "late"}
	_ = model.openDiagnostics()
	fetch := model.beginDiagnosticFetch()
	if fetch == nil {
		t.Fatal("nil diagnostics fetch command")
	}
	message := fetch()
	_, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if model.diagnostics.mode != diagnosticClosed {
		t.Fatal("Esc did not close loading overlay")
	}
	_, _ = model.Update(message)
	if model.diagnostics.mode != diagnosticClosed {
		t.Fatal("late result reopened a closed diagnostics overlay")
	}
}

func TestDiagnosticErrorsStayInOverlay(t *testing.T) {
	model, transport := newTestModel(t)
	transport.diagnosticErr = errors.New("invalid_endpoint: not allow-listed")
	_ = model.openDiagnostics()
	fetch := model.beginDiagnosticFetch()
	_, _ = model.Update(fetch())
	if model.diagnostics.mode != diagnosticResult {
		t.Fatalf("error mode = %d, want result", model.diagnostics.mode)
	}
	if !strings.Contains(model.diagnostics.err, "not allow-listed") {
		t.Errorf("overlay error = %q", model.diagnostics.err)
	}
	if model.lastError != "" {
		t.Errorf("diagnostic error incorrectly marked device offline: %q", model.lastError)
	}
}

func TestDiagnosticOverlayResponsiveDimensions(t *testing.T) {
	sizes := []tea.WindowSizeMsg{
		{Width: 24, Height: 8},
		{Width: 40, Height: 12},
		{Width: 80, Height: 24},
		{Width: 140, Height: 45},
	}
	for _, size := range sizes {
		size := size
		t.Run(
			fmt.Sprintf("%dx%d", size.Width, size.Height),
			func(t *testing.T) {
				model, transport := newTestModel(t)
				transport.diagnosticResult = DiagnosticResult{
					Content: strings.Repeat("line\n", 120),
				}
				_, _ = model.Update(size)
				_ = model.openDiagnostics()

				formView := model.View()
				assertViewFits(t, formView, size)

				fetch := model.beginDiagnosticFetch()
				_, _ = model.Update(fetch())
				resultView := model.View()
				assertViewFits(t, resultView, size)

				layout := calculateDiagnosticOverlayLayout(size.Width, size.Height)
				if layout.width > size.Width || layout.height > size.Height ||
					layout.bodyHeight < 1 {
					t.Errorf("invalid layout %+v for %+v", layout, size)
				}
			},
		)
	}
}

func assertViewFits(t *testing.T, view tea.View, size tea.WindowSizeMsg) {
	t.Helper()
	if width := lipgloss.Width(view.Content); width > size.Width {
		t.Errorf("view width = %d, terminal width = %d", width, size.Width)
	}
	if height := lipgloss.Height(view.Content); height > size.Height {
		t.Errorf("view height = %d, terminal height = %d", height, size.Height)
	}
}

func keyMessage(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Text: text})
}
