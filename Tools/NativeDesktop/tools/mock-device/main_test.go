package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"pccontroller.local/desktop/compatibility"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pccontroller.local/desktop/internal/diagnostics"
)

func newTestDevice() *mockDevice {
	return &mockDevice{
		started: time.Now().UTC(),
		logger:  log.New(io.Discard, "", 0),
		opts: options{
			initialState:   "off",
			firmware:       "test",
			country:        "IR",
			signalStrength: -55,
			diagnostics:    "asa",
		},
		state:   "OFF",
		clients: make(map[*wsClient]struct{}),
	}
}

func TestHTTPCommandsAndWebSocketEvents(t *testing.T) {
	device := newTestDevice()
	server := httptest.NewServer(device.routes())
	defer server.Close()

	response, err := http.Get(server.URL + compatibility.StateEndpoint("get"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "OFF\n" {
		t.Fatalf("initial response = %d %q", response.StatusCode, body)
	}

	socketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	connection, _, err := websocket.DefaultDialer.Dial(socketURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteMessage(websocket.TextMessage, []byte("get")); err != nil {
		t.Fatal(err)
	}
	if got := readTextMessage(t, connection); got != "OFF" {
		t.Fatalf("initial WebSocket state = %q", got)
	}

	response, err = http.Get(server.URL + compatibility.StateEndpoint("on"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := readTextMessage(t, connection); got != "ON" {
		t.Fatalf("on broadcast = %q", got)
	}

	eventBody := []byte(`{"type":"msg","icon":2,"message":"hello world"}`)
	response, err = http.Post(
		server.URL+"/mock/event",
		"application/json",
		bytes.NewReader(eventBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("event injection = %d %s", response.StatusCode, data)
	}
	response.Body.Close()
	if got := readTextMessage(t, connection); got != "msg:2,hello+world" {
		t.Fatalf("message broadcast = %q", got)
	}
}

func TestDiagnosticAndInvalidEventRoutes(t *testing.T) {
	device := newTestDevice()
	server := httptest.NewServer(device.routes())
	defer server.Close()

	response, err := http.Get(server.URL + "/info")
	if err != nil {
		t.Fatal(err)
	}
	info, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if !strings.Contains(string(info), "hostname: pccontroller-desktop-mock") ||
		!strings.Contains(string(info), "/test/") {
		t.Fatalf("/info = %q", info)
	}

	response, err = http.Post(
		server.URL+"/mock/event",
		"application/json",
		strings.NewReader(`{"type":"beep","count":11}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid beep status = %d", response.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+compatibility.StateEndpoint("off"), nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed ||
		response.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("wrong-method response = %d, Allow %q", response.StatusCode, response.Header.Get("Allow"))
	}
}

func TestDiagnosticFixturesSupportASAAndJSONWireFormats(t *testing.T) {
	for _, format := range []string{"asa", "json"} {
		t.Run(format, func(t *testing.T) {
			device := newTestDevice()
			device.opts.diagnostics = format
			server := httptest.NewServer(device.routes())
			defer server.Close()

			documents := make(map[string]diagnostics.Document)
			for _, endpoint := range diagnostics.PollEndpoints() {
				response, err := http.Get(server.URL + endpoint)
				if err != nil {
					t.Fatalf("%s: %v", endpoint, err)
				}
				data, readErr := io.ReadAll(response.Body)
				response.Body.Close()
				if readErr != nil {
					t.Fatalf("%s: %v", endpoint, readErr)
				}
				if response.StatusCode != http.StatusOK {
					t.Fatalf("%s status = %d body=%q", endpoint, response.StatusCode, data)
				}
				document := diagnostics.Parse(endpoint, data, time.Now())
				if len(document.Fields) == 0 {
					t.Fatalf("%s parsed no fields: %+v", endpoint, document)
				}
				if endpoint == diagnostics.EndpointConfigAP ||
					endpoint == diagnostics.EndpointConfigST {
					assertNoMockCredential(t, endpoint, document)
				}
				documents[endpoint] = document
			}
			info := diagnostics.Project(documents)
			if info.Hostname != "pccontroller-desktop-mock" ||
				info.RSSIDBm == nil || *info.RSSIDBm != -55 ||
				info.WiFiConnected == nil || !*info.WiFiConnected ||
				info.SSID != "PCControllerDesktop-Mock" ||
				info.WiFiIP != "127.0.0.1" ||
				info.Country != "IR" ||
				info.Description.FriendlyName != "PCController Mock" ||
				info.Update.Hash == "" {
				t.Fatalf("%s projected diagnostics = %+v", format, info)
			}
			if format == "asa" {
				if documents[diagnostics.EndpointInfo].Format != diagnostics.FormatText ||
					documents[diagnostics.EndpointDescription].Format != diagnostics.FormatXML {
					t.Fatalf("ASA formats = info:%s description:%s",
						documents[diagnostics.EndpointInfo].Format,
						documents[diagnostics.EndpointDescription].Format)
				}
			} else {
				if documents[diagnostics.EndpointInfo].Format != diagnostics.FormatJSON ||
					documents[diagnostics.EndpointWiFiSignal].Format != diagnostics.FormatJSON {
					t.Fatalf("JSON formats = info:%s signal:%s",
						documents[diagnostics.EndpointInfo].Format,
						documents[diagnostics.EndpointWiFiSignal].Format)
				}
			}
		})
	}
}

func assertNoMockCredential(
	t *testing.T,
	endpoint string,
	document diagnostics.Document,
) {
	t.Helper()
	serialized, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("%s serialize parsed document: %v", endpoint, err)
	}
	values := []struct {
		name  string
		value string
	}{
		{"document raw", document.Raw},
		{"safe raw", document.SafeRaw},
		{"serialized document", string(serialized)},
	}
	for _, field := range document.Fields {
		values = append(
			values,
			struct {
				name  string
				value string
			}{name: "field raw", value: field.Raw},
			struct {
				name  string
				value string
			}{name: "field display", value: field.Display},
		)
	}
	for _, secret := range []string{
		mockAccessPointPassword,
		mockStationPassword,
	} {
		for _, candidate := range values {
			if strings.Contains(candidate.value, secret) {
				t.Fatalf(
					"%s retained mock credential in %s: %q",
					endpoint,
					candidate.name,
					candidate.value,
				)
			}
		}
	}
	if !strings.Contains(document.SafeRaw, "[redacted]") {
		t.Fatalf("%s safe raw does not record credential redaction: %q", endpoint, document.SafeRaw)
	}
}

func TestValidateListenAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		if err := validateListenAddress(address, false); err != nil {
			t.Errorf("validateListenAddress(%q): %v", address, err)
		}
	}
	for _, address := range []string{":8080", "0.0.0.0:8080", "192.0.2.2:8080"} {
		if err := validateListenAddress(address, false); err == nil {
			t.Errorf("validateListenAddress(%q) accepted unsafe address", address)
		}
	}
	if err := validateListenAddress("0.0.0.0:8080", true); err != nil {
		t.Fatalf("explicit non-loopback opt-in: %v", err)
	}
	if err := validateNamedListenAddress(
		"-telnet-listen",
		"127.0.0.1:2323",
		false,
	); err != nil {
		t.Fatalf("terminal loopback listener: %v", err)
	}
	if err := validateNamedListenAddress(
		"-telnet-listen",
		"0.0.0.0:2323",
		false,
	); err == nil || !strings.Contains(err.Error(), "-telnet-listen") {
		t.Fatalf("unsafe terminal listener error = %v", err)
	}
}

func TestOptionalTerminalCommandsBoundInputAndTrackShutdown(t *testing.T) {
	device := newTestDevice()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	failure := device.startTerminal(listener)

	connection, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	banner := readTerminalPrompt(t, connection, reader)
	if !bytes.HasPrefix(
		[]byte(banner),
		[]byte{mockTelnetIAC, mockTelnetWILL, 1},
	) || !strings.Contains(banner, "PCController mock terminal") {
		t.Fatalf("terminal banner = %q", banner)
	}

	// A real Telnet client answers WILL ECHO with DO ECHO. Split that reply to
	// prove it cannot corrupt the first shell command.
	for _, fragment := range [][]byte{
		{mockTelnetIAC},
		{mockTelnetDO},
		{1},
		[]byte("state\r\n"),
	} {
		if _, err := connection.Write(fragment); err != nil {
			t.Fatal(err)
		}
	}
	if response := readTerminalPrompt(t, connection, reader); !strings.Contains(response, "OFF\r\n") {
		t.Fatalf("state response = %q", response)
	}

	commands := []struct {
		line     string
		contains string
		state    string
	}{
		{"help", "Commands:", "OFF"},
		{"on", "OK ON", "ON"},
		{"toggle", "OK OFF", "OFF"},
		{"off", "OK OFF", "OFF"},
		{"message hello from terminal", "OK message", "OFF"},
		{"beep 3", "OK beep:3", "OFF"},
		{"beep 11", "ERR beep count", "OFF"},
	}
	for _, command := range commands {
		if _, err := io.WriteString(connection, command.line+"\r\n"); err != nil {
			t.Fatalf("%s write: %v", command.line, err)
		}
		response := readTerminalPrompt(t, connection, reader)
		if !strings.Contains(response, command.contains) {
			t.Fatalf("%s response = %q, want %q", command.line, response, command.contains)
		}
		if state := device.currentState(); state != command.state {
			t.Fatalf("%s state = %q, want %q", command.line, state, command.state)
		}
	}

	if _, err := io.WriteString(connection, "quit\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	line, err := reader.ReadString('\n')
	if err != nil || line != "BYE\r\n" {
		t.Fatalf("quit response = %q, %v", line, err)
	}
	_ = connection.Close()

	oversized, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	oversizedReader := bufio.NewReader(oversized)
	_ = readTerminalPrompt(t, oversized, oversizedReader)
	if _, err := io.WriteString(
		oversized,
		strings.Repeat("x", maxTerminalLineBytes+1)+"\n",
	); err != nil {
		t.Fatalf("write oversized terminal command: %v", err)
	}
	rejection := readTerminalPrompt(t, oversized, oversizedReader)
	if !strings.Contains(rejection, "ERR terminal line too long") {
		t.Fatalf("oversized command response = %q", rejection)
	}
	if _, err := io.WriteString(oversized, "state\r\n"); err != nil {
		t.Fatalf("write command after oversized rejection: %v", err)
	}
	if response := readTerminalPrompt(
		t,
		oversized,
		oversizedReader,
	); !strings.Contains(response, "OFF\r\n") {
		t.Fatalf("terminal did not resynchronize after oversized line: %q", response)
	}
	_ = oversized.Close()

	idle, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	idleReader := bufio.NewReader(idle)
	_ = readTerminalPrompt(t, idle, idleReader)
	stopped := make(chan struct{})
	go func() {
		device.stopTerminal()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal shutdown did not wait for and close active sessions")
	}
	if err := idle.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := idleReader.ReadByte(); err == nil {
		t.Fatal("idle terminal connection remained readable after shutdown")
	}
	_ = idle.Close()
	select {
	case err := <-failure:
		if err != nil {
			t.Fatalf("terminal accept loop shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal accept loop did not report shutdown")
	}
}

func readTerminalPrompt(
	t *testing.T,
	connection net.Conn,
	reader *bufio.Reader,
) string {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	for !bytes.HasSuffix(output.Bytes(), []byte("\r\n> ")) {
		value, err := reader.ReadByte()
		if err != nil {
			t.Fatalf("read terminal prompt after %q: %v", output.Bytes(), err)
		}
		output.WriteByte(value)
		if output.Len() > 16<<10 {
			t.Fatalf("terminal response exceeded test bound: %q", output.Bytes())
		}
	}
	return output.String()
}

func readTextMessage(t *testing.T, connection *websocket.Conn) string {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.TextMessage {
		t.Fatalf("message type = %d", messageType)
	}
	return string(payload)
}
