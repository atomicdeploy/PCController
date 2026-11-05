package lamp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"rayanlamp.local/app/internal/model"
)

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name string
		in   string
		want string
	}{
		{name: "mdns without scheme", in: "rayanlamp.local", want: "http://rayanlamp.local"},
		{name: "whitespace", in: "  https://rayanlamp.local/  ", want: "https://rayanlamp.local"},
		{name: "port", in: "http://rayanlamp.local:8080/", want: "http://rayanlamp.local:8080"},
		{name: "ipv4", in: "192.0.2.5:81", want: "http://192.0.2.5:81"},
		{name: "ipv6", in: "http://[::1]:8080", want: "http://[::1]:8080"},
	}
	for _, test := range valid {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeBaseURL(test.in)
			if err != nil {
				t.Fatalf("NormalizeBaseURL(%q): %v", test.in, err)
			}
			if got != test.want {
				t.Fatalf("NormalizeBaseURL(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}

	invalid := []string{
		"",
		"ftp://rayanlamp.local",
		"http://",
		"http://user:secret@rayanlamp.local",
		"http://rayanlamp.local/a",
		"http://rayanlamp.local?full=1",
		"http://rayanlamp.local#fragment",
		"http://rayanlamp.local:99999",
		"http://rayanlamp.local/\nheader",
		`http://rayanlamp.local\path`,
	}
	for _, input := range invalid {
		input := input
		t.Run("invalid_"+input, func(t *testing.T) {
			t.Parallel()
			if _, err := NormalizeBaseURL(input); !errors.Is(err, ErrInvalidBaseURL) {
				t.Fatalf("NormalizeBaseURL(%q) error = %v, want ErrInvalidBaseURL", input, err)
			}
		})
	}
}

func TestHTTPCommandsAndDiagnosticFetch(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		switch r.URL.Path {
		case "/lamp/get":
			fmt.Fprintln(w, "ON")
		case "/lamp/on":
			fmt.Fprintln(w, "ok")
		case "/lamp/off":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"state":"off"}`)
		case "/lamp/toggle":
			fmt.Fprint(w, `"on"`)
		case "/info":
			fmt.Fprintf(w, "diagnostics:%s", r.URL.Query().Get("view"))
		case "/fail":
			http.Error(w, "no device", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if state, err := client.Get(ctx); err != nil || state != model.StateOn {
		t.Fatalf("Get() = (%v, %v), want on", state, err)
	}
	if state, err := client.On(ctx); err != nil || state != model.StateOn {
		t.Fatalf("On() = (%v, %v), want on fallback", state, err)
	}
	if state, err := client.Off(ctx); err != nil || state != model.StateOff {
		t.Fatalf("Off() = (%v, %v), want off", state, err)
	}
	if state, err := client.Toggle(ctx); err != nil || state != model.StateOn {
		t.Fatalf("Toggle() = (%v, %v), want on", state, err)
	}
	body, err := client.Fetch(ctx, "info?view=full")
	if err != nil {
		t.Fatalf("Fetch(info): %v", err)
	}
	if got, want := string(body), "diagnostics:full"; got != want {
		t.Fatalf("Fetch(info) = %q, want %q", got, want)
	}

	_, err = client.Fetch(ctx, "/fail")
	var statusError *HTTPStatusError
	if !errors.As(err, &statusError) || statusError.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("Fetch(/fail) error = %#v, want HTTPStatusError 503", err)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"/lamp/get",
		"/lamp/on",
		"/lamp/off",
		"/lamp/toggle",
		"/info?view=full",
		"/fail",
	}
	if fmt.Sprint(gotRequests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v, want %v", gotRequests, wantRequests)
	}
}

func TestFetchRejectsUnsafeEndpoints(t *testing.T) {
	t.Parallel()

	client, err := New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	tests := []string{
		"https://example.com/info",
		"//example.com/info",
		"/../info",
		"/%2e%2e/info",
		`/info\admin`,
		"/info#fragment",
		"/lamp/on",
		"/lamp/off?force=1",
		"/lamp/toggle/",
	}
	for _, endpoint := range tests {
		endpoint := endpoint
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			if _, err := client.Fetch(context.Background(), endpoint); !errors.Is(err, ErrUnsafeEndpoint) {
				t.Fatalf("Fetch(%q) error = %v, want ErrUnsafeEndpoint", endpoint, err)
			}
		})
	}
}

func TestHTTPBodyLimitAndUnexpectedState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lamp/get":
			fmt.Fprint(w, "not-a-state")
		default:
			fmt.Fprint(w, "12345")
		}
	}))
	defer server.Close()

	client, err := New(server.URL, WithBodyLimit(4))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Fetch(context.Background(), "/info"); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Fetch oversized error = %v, want ErrResponseTooLarge", err)
	}

	client, err = New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background()); !errors.Is(err, ErrUnexpectedState) {
		t.Fatalf("Get unexpected state error = %v, want ErrUnexpectedState", err)
	}
}

func TestNilContext(t *testing.T) {
	t.Parallel()
	client, err := New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(nil); !errors.Is(err, ErrNilContext) {
		t.Fatalf("Get(nil) error = %v, want ErrNilContext", err)
	}
	if err := client.Listen(nil, Handlers{}); !errors.Is(err, ErrNilContext) {
		t.Fatalf("Listen(nil) error = %v, want ErrNilContext", err)
	}
}

func TestCloseIdleConnectionsDelegatesToHTTPTransport(t *testing.T) {
	t.Parallel()

	transport := &closeIdleTransport{}
	client, err := New(
		"http://127.0.0.1:1",
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	client.CloseIdleConnections()
	if got := transport.closeCount(); got != 2 {
		t.Fatalf("CloseIdleConnections calls = %d, want 2", got)
	}

	// Lifecycle cleanup is deliberately nil-safe.
	var nilClient *Client
	nilClient.CloseIdleConnections()
}

type closeIdleTransport struct {
	mu     sync.Mutex
	closes int
}

func (*closeIdleTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected HTTP request")
}

func (t *closeIdleTransport) CloseIdleConnections() {
	t.mu.Lock()
	t.closes++
	t.mu.Unlock()
}

func (t *closeIdleTransport) closeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closes
}
