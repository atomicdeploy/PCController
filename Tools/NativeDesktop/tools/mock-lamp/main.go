// Command mock-lamp provides a local development double for the Rayan Lamp
// firmware. It implements the HTTP and WebSocket surfaces consumed by the
// desktop application and exposes explicit test-only event injection routes.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	maxRequestBytes      = 16 << 10
	maxEventBytes        = 16 << 10
	maxMessageBytes      = 4 << 10
	maxTerminalLineBytes = maxMessageBytes + 128
	terminalReadBuffer   = 512

	mockAccessPointPassword = "mock-ap-password-never-use"
	mockStationPassword     = "mock-station-password-never-use"
)

var errTerminalLineTooLong = errors.New("terminal line too long")

type options struct {
	listen         string
	telnetListen   string
	initialState   string
	cycle          time.Duration
	allowNonLocal  bool
	verbose        bool
	firmware       string
	country        string
	signalStrength int
	diagnostics    string
}

type mockLamp struct {
	started time.Time
	logger  *log.Logger
	opts    options

	mu      sync.RWMutex
	state   string
	clients map[*wsClient]struct{}

	terminalMu          sync.Mutex
	terminalListener    net.Listener
	terminalConnections map[net.Conn]struct{}
	terminalStopping    bool
	terminalWG          sync.WaitGroup
}

type wsClient struct {
	connection *websocket.Conn
	writeMu    sync.Mutex
}

type eventRequest struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Icon    int    `json:"icon,omitempty"`
	Count   int    `json:"count,omitempty"`
	Raw     string `json:"raw,omitempty"`
}

type apiResponse struct {
	OK      bool   `json:"ok"`
	State   string `json:"state,omitempty"`
	Event   string `json:"event,omitempty"`
	Clients int    `json:"clients,omitempty"`
	Error   string `json:"error,omitempty"`
}

func main() {
	os.Exit(run())
}

func run() int {
	var opts options
	flag.StringVar(&opts.listen, "listen", "127.0.0.1:8080", "HTTP listen address")
	flag.StringVar(
		&opts.telnetListen,
		"telnet-listen",
		"",
		"optional raw/Telnet-style terminal listen address (disabled by default)",
	)
	flag.StringVar(&opts.initialState, "state", "off", "initial lamp state: on or off")
	flag.DurationVar(&opts.cycle, "cycle", 0, "automatically toggle and broadcast at this interval")
	flag.BoolVar(&opts.allowNonLocal, "allow-non-loopback", false, "allow a non-loopback listen address")
	flag.BoolVar(&opts.verbose, "verbose", true, "log HTTP and WebSocket activity")
	flag.StringVar(&opts.firmware, "firmware", "mock-1.0.0", "firmware version returned by /info")
	flag.StringVar(&opts.country, "country", "IR", "country code returned by /wifi/country")
	flag.IntVar(&opts.signalStrength, "signal", -42, "RSSI returned by /wifi/signal")
	flag.StringVar(
		&opts.diagnostics,
		"diagnostics-format",
		"asa",
		"diagnostic response format: asa or json",
	)
	flag.Parse()

	opts.initialState = normalizeState(opts.initialState)
	if opts.initialState == "" {
		fmt.Fprintln(os.Stderr, "mock-lamp: -state must be on or off")
		return 2
	}
	if opts.cycle < 0 {
		fmt.Fprintln(os.Stderr, "mock-lamp: -cycle cannot be negative")
		return 2
	}
	opts.diagnostics = strings.ToLower(strings.TrimSpace(opts.diagnostics))
	if opts.diagnostics != "asa" && opts.diagnostics != "json" {
		fmt.Fprintln(os.Stderr, "mock-lamp: -diagnostics-format must be asa or json")
		return 2
	}
	if err := validateListenAddress(opts.listen, opts.allowNonLocal); err != nil {
		fmt.Fprintln(os.Stderr, "mock-lamp:", err)
		return 2
	}
	if opts.telnetListen != "" {
		if err := validateNamedListenAddress(
			"-telnet-listen",
			opts.telnetListen,
			opts.allowNonLocal,
		); err != nil {
			fmt.Fprintln(os.Stderr, "mock-lamp:", err)
			return 2
		}
	}

	logger := log.New(os.Stderr, "mock-lamp: ", log.LstdFlags|log.Lmicroseconds)
	lamp := &mockLamp{
		started: time.Now().UTC(),
		logger:  logger,
		opts:    opts,
		state:   strings.ToUpper(opts.initialState),
		clients: make(map[*wsClient]struct{}),
	}

	server := &http.Server{
		Addr:              opts.listen,
		Handler:           lamp.routes(),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if opts.cycle > 0 {
		go lamp.cycleState(ctx, opts.cycle)
	}

	failure := make(chan error, 1)
	var terminalFailure <-chan error
	if opts.telnetListen != "" {
		listener, err := net.Listen("tcp", opts.telnetListen)
		if err != nil {
			logger.Printf("terminal listener failed: %v", err)
			return 1
		}
		terminalFailure = lamp.startTerminal(listener)
		logger.Printf("terminal listening at %s", listener.Addr())
	}
	go func() {
		logger.Printf("listening at http://%s (state %s)", opts.listen, lamp.currentState())
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			failure <- err
			return
		}
		failure <- nil
	}()

	select {
	case err := <-failure:
		lamp.stopTerminal()
		if err != nil {
			logger.Printf("server failed: %v", err)
			return 1
		}
	case err := <-terminalFailure:
		lamp.stopTerminal()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		lamp.disconnectClients()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Printf("shutdown after terminal failure failed: %v", shutdownErr)
		}
		<-failure
		if err != nil {
			logger.Printf("terminal listener failed: %v", err)
			return 1
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		lamp.disconnectClients()
		lamp.stopTerminal()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Printf("shutdown failed: %v", err)
			return 1
		}
		<-failure
	}
	return 0
}

func (lamp *mockLamp) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", lamp.index)
	mux.HandleFunc("/healthz", lamp.health)
	mux.HandleFunc("/lamp/get", lamp.get)
	mux.HandleFunc("/lamp/on", lamp.on)
	mux.HandleFunc("/lamp/off", lamp.off)
	mux.HandleFunc("/lamp/toggle", lamp.toggle)
	mux.HandleFunc("/ws", lamp.webSocket)

	mux.HandleFunc("/info", lamp.info)
	mux.HandleFunc("/update/info", lamp.updateInfo)
	mux.HandleFunc("/wifi/signal", lamp.wifiSignal)
	mux.HandleFunc("/wifi/status", lamp.wifiStatus)
	mux.HandleFunc("/wifi/country", lamp.wifiCountry)
	mux.HandleFunc("/config/ap", lamp.accessPointConfig)
	mux.HandleFunc("/config/st", lamp.stationConfig)
	mux.HandleFunc("/description.xml", lamp.description)

	mux.HandleFunc("/mock/event", lamp.injectEvent)
	mux.HandleFunc("/mock/disconnect", lamp.disconnect)

	return lamp.logRequests(mux)
}

func (lamp *mockLamp) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if lamp.opts.verbose {
			lamp.logger.Printf("%s %s from %s", request.Method, request.URL.RequestURI(), request.RemoteAddr)
		}
		next.ServeHTTP(w, request)
	})
}

func (lamp *mockLamp) index(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, apiResponse{OK: false, Error: "route not found"})
		return
	}
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":              "Rayan Lamp firmware mock",
		"state":             lamp.currentState(),
		"websocket_clients": lamp.clientCount(),
		"lamp_endpoints":    []string{"/lamp/get", "/lamp/on", "/lamp/off", "/lamp/toggle", "/ws"},
		"inject":            "POST /mock/event",
		"disconnect":        "POST /mock/disconnect",
	})
}

func (lamp *mockLamp) health(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{
		OK:      true,
		State:   lamp.currentState(),
		Clients: lamp.clientCount(),
	})
}

func (lamp *mockLamp) get(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	writeText(w, http.StatusOK, lamp.currentState()+"\n")
}

func (lamp *mockLamp) on(w http.ResponseWriter, request *http.Request) {
	lamp.setStateCommand(w, request, "ON")
}

func (lamp *mockLamp) off(w http.ResponseWriter, request *http.Request) {
	lamp.setStateCommand(w, request, "OFF")
}

func (lamp *mockLamp) toggle(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	lamp.mu.Lock()
	if lamp.state == "ON" {
		lamp.state = "OFF"
	} else {
		lamp.state = "ON"
	}
	state := lamp.state
	lamp.mu.Unlock()
	lamp.broadcast(state)
	writeText(w, http.StatusOK, state+"\n")
}

func (lamp *mockLamp) setStateCommand(w http.ResponseWriter, request *http.Request, state string) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	lamp.setState(state)
	lamp.broadcast(state)
	writeText(w, http.StatusOK, state+"\n")
}

func (lamp *mockLamp) info(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	if lamp.jsonDiagnostics() {
		writeJSON(w, http.StatusOK, map[string]any{
			"device":            "Rayan Lamp Mock",
			"firmware":          lamp.opts.firmware,
			"hostname":          "rayanlamp-mock",
			"state":             lamp.currentState(),
			"uptime_seconds":    int64(time.Since(lamp.started).Seconds()),
			"websocket_clients": lamp.clientCount(),
			"time":              time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	uptime := formatMockUptime(int64(time.Since(lamp.started).Seconds()))
	writeText(
		w,
		http.StatusOK,
		"hostname: rayanlamp-mock\n"+
			"wifi version: ESP8266-Mock/"+lamp.opts.firmware+"/WebServer:mock\n"+
			"esp chip id: a1b2c3\n"+
			"flash chip id: deadbeef\n"+
			"firmware hash: 0123456789abcdef0123456789abcdef\n"+
			"protocol rev.: 1\n"+
			"st mac: 02:00:00:00:00:01\n"+
			"ap mac: 02:00:00:00:00:02\n"+
			"client ip: 127.0.0.1\n"+
			"flash size: 4.00 MB\n"+
			"program usage: 25%\n"+
			"settings hash: cafe1234\n"+
			"heap memory: 49152\n"+
			"build: Jul 23 2026 12:00:00 by mock@localhost\n"+
			"uptime: "+uptime+"\n",
	)
}

func (lamp *mockLamp) jsonDiagnostics() bool {
	return strings.EqualFold(strings.TrimSpace(lamp.opts.diagnostics), "json")
}

func formatMockUptime(totalSeconds int64) string {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	days := totalSeconds / 86400
	totalSeconds %= 86400
	hours := totalSeconds / 3600
	totalSeconds %= 3600
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	if days > 0 {
		return fmt.Sprintf("%d days, %02d:%02d:%02d", days, hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func (lamp *mockLamp) diagnosticQuality() string {
	rssi := lamp.opts.signalStrength
	switch {
	case rssi <= -110:
		return "No signal"
	case rssi <= -80:
		return "Bad"
	case rssi <= -70:
		return "Low"
	case rssi <= -60:
		return "Normal"
	case rssi <= -50:
		return "Good"
	case rssi <= -40:
		return "Excellent"
	default:
		return "Undefined"
	}
}

func (lamp *mockLamp) updateInfo(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hash": "0123456789abcdef0123456789abcdef",
		"size": 1_048_576,
		"free": 2_097_152,
		"date": "Jul 23 2026 12:00:00",
	})
}

func (lamp *mockLamp) wifiSignal(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	if lamp.jsonDiagnostics() {
		writeJSON(w, http.StatusOK, map[string]any{
			"rssi":    lamp.opts.signalStrength,
			"quality": lamp.diagnosticQuality(),
		})
		return
	}
	writeText(
		w,
		http.StatusOK,
		strconv.Itoa(lamp.opts.signalStrength)+"\n"+lamp.diagnosticQuality()+"\n",
	)
}

func (lamp *mockLamp) wifiStatus(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	if lamp.jsonDiagnostics() {
		writeJSON(w, http.StatusOK, map[string]any{
			"connected": true,
			"ssid":      "RayanLamp-Mock",
			"ip":        "127.0.0.1",
			"rssi":      lamp.opts.signalStrength,
		})
		return
	}
	// ASA returns its NetStatus_* numeric values rather than the friendly text
	// used by older development fixtures. 1 means OK/connected for both modes.
	writeText(w, http.StatusOK, "ap status: 1\nst status: 1\n")
}

func (lamp *mockLamp) wifiCountry(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	country := strings.ToUpper(strings.TrimSpace(lamp.opts.country))
	if lamp.jsonDiagnostics() {
		writeJSON(w, http.StatusOK, map[string]any{"country": country})
		return
	}
	writeText(w, http.StatusOK, "country: "+country+"\n")
}

func (lamp *mockLamp) accessPointConfig(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	if lamp.jsonDiagnostics() {
		writeJSON(w, http.StatusOK, map[string]any{
			"ssid":    "RayanLamp-Mock-AP",
			"pass":    mockAccessPointPassword,
			"ip":      "192.168.4.1",
			"netmask": "255.255.255.0",
		})
		return
	}
	writeText(
		w,
		http.StatusOK,
		"ssid: RayanLamp-Mock-AP\n"+
			"pass: "+mockAccessPointPassword+"\n"+
			"ip: 192.168.4.1\n"+
			"netmask: 255.255.255.0\n",
	)
}

func (lamp *mockLamp) stationConfig(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	if lamp.jsonDiagnostics() {
		writeJSON(w, http.StatusOK, map[string]any{
			"ssid":    "RayanLamp-Mock",
			"pass":    mockStationPassword,
			"dhcp":    "enabled",
			"ip":      "127.0.0.1",
			"netmask": "255.0.0.0",
			"gw":      "127.0.0.1",
			"dns":     "127.0.0.1, 1.1.1.1",
		})
		return
	}
	writeText(
		w,
		http.StatusOK,
		"ssid: RayanLamp-Mock\n"+
			"pass: "+mockStationPassword+"\n"+
			"dhcp: enabled\n"+
			"ip: 127.0.0.1\n"+
			"netmask: 255.0.0.0\n"+
			"gw: 127.0.0.1\n"+
			"dns: 127.0.0.1, 1.1.1.1\n",
	)
}

func (lamp *mockLamp) dateTime(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	writeText(w, http.StatusOK, time.Now().UTC().Format(time.RFC3339)+"\n")
}

func (lamp *mockLamp) description(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodGet) {
		return
	}
	if lamp.jsonDiagnostics() {
		writeJSON(w, http.StatusOK, map[string]any{
			"friendlyName": "Rayan Lamp Mock",
			"modelName":    "ESP8266 Mock",
			"manufacturer": "Rayan's Lamp",
		})
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(
		w,
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+
			"<device><friendlyName>Rayan Lamp Mock</friendlyName>"+
			"<modelName>RayanLamp Development Double</modelName></device>\n",
	)
}

func (lamp *mockLamp) expertText(value string) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		if !requireMethod(w, request, http.MethodGet) {
			return
		}
		writeText(w, http.StatusOK, value+"\n")
	}
}

func (lamp *mockLamp) webSocket(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		CheckOrigin: func(candidate *http.Request) bool {
			origin := strings.TrimSpace(candidate.Header.Get("Origin"))
			if origin == "" {
				return true
			}
			parsed, err := url.Parse(origin)
			return err == nil && strings.EqualFold(parsed.Host, candidate.Host)
		},
	}
	connection, err := upgrader.Upgrade(w, request, nil)
	if err != nil {
		lamp.logger.Printf("WebSocket upgrade: %v", err)
		return
	}
	client := &wsClient{connection: connection}
	connection.SetReadLimit(64 << 10)
	lamp.register(client)
	defer lamp.unregister(client)

	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				lamp.logger.Printf("WebSocket read: %v", err)
			}
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		command := strings.TrimSpace(string(payload))
		if strings.EqualFold(command, "get") {
			if err := client.write(lamp.currentState()); err != nil {
				return
			}
		} else if lamp.opts.verbose {
			lamp.logger.Printf("ignored WebSocket command %q", command)
		}
	}
}

func (lamp *mockLamp) injectEvent(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodPost) {
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, maxRequestBytes)
	defer request.Body.Close()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var event eventRequest
	if err := decoder.Decode(&event); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{OK: false, Error: "invalid JSON event: " + err.Error()})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, apiResponse{OK: false, Error: "body must contain one JSON object"})
		return
	}

	event.Type = strings.ToLower(strings.TrimSpace(event.Type))
	var raw string
	switch event.Type {
	case "on":
		lamp.setState("ON")
		raw = "ON"
	case "off":
		lamp.setState("OFF")
		raw = "OFF"
	case "toggle":
		lamp.mu.Lock()
		if lamp.state == "ON" {
			lamp.state = "OFF"
		} else {
			lamp.state = "ON"
		}
		raw = lamp.state
		lamp.mu.Unlock()
	case "message":
		if err := validateMessage(event.Message); err != nil {
			writeJSON(w, http.StatusBadRequest, apiResponse{OK: false, Error: err.Error()})
			return
		}
		raw = "message:" + strings.TrimSpace(event.Message)
	case "msg":
		if event.Icon < 0 || event.Icon > 255 {
			writeJSON(w, http.StatusBadRequest, apiResponse{OK: false, Error: "icon must be between 0 and 255"})
			return
		}
		if err := validateMessage(event.Message); err != nil {
			writeJSON(w, http.StatusBadRequest, apiResponse{OK: false, Error: err.Error()})
			return
		}
		raw = fmt.Sprintf("msg:%d,%s", event.Icon, url.QueryEscape(strings.TrimSpace(event.Message)))
	case "beep":
		if event.Count < 1 || event.Count > 10 {
			writeJSON(w, http.StatusBadRequest, apiResponse{OK: false, Error: "count must be between 1 and 10"})
			return
		}
		raw = fmt.Sprintf("beep:%d", event.Count)
	case "raw":
		raw = strings.TrimSpace(event.Raw)
		if raw == "" || len(raw) > maxEventBytes || !utf8.ValidString(raw) ||
			strings.IndexByte(raw, 0) >= 0 {
			writeJSON(w, http.StatusBadRequest, apiResponse{OK: false, Error: "raw event is empty or invalid"})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, apiResponse{
			OK:    false,
			Error: "type must be on, off, toggle, message, msg, beep, or raw",
		})
		return
	}

	lamp.broadcast(raw)
	writeJSON(w, http.StatusOK, apiResponse{
		OK:      true,
		State:   lamp.currentState(),
		Event:   raw,
		Clients: lamp.clientCount(),
	})
}

func (lamp *mockLamp) disconnect(w http.ResponseWriter, request *http.Request) {
	if !requireMethod(w, request, http.MethodPost) {
		return
	}
	count := lamp.clientCount()
	lamp.disconnectClients()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"disconnected": count,
	})
}

func (lamp *mockLamp) currentState() string {
	lamp.mu.RLock()
	defer lamp.mu.RUnlock()
	return lamp.state
}

func (lamp *mockLamp) setState(state string) {
	lamp.mu.Lock()
	lamp.state = state
	lamp.mu.Unlock()
}

func (lamp *mockLamp) clientCount() int {
	lamp.mu.RLock()
	defer lamp.mu.RUnlock()
	return len(lamp.clients)
}

func (lamp *mockLamp) register(client *wsClient) {
	lamp.mu.Lock()
	lamp.clients[client] = struct{}{}
	count := len(lamp.clients)
	lamp.mu.Unlock()
	if lamp.opts.verbose {
		lamp.logger.Printf("WebSocket connected (%d clients)", count)
	}
}

func (lamp *mockLamp) unregister(client *wsClient) {
	lamp.mu.Lock()
	_, exists := lamp.clients[client]
	delete(lamp.clients, client)
	count := len(lamp.clients)
	lamp.mu.Unlock()
	if exists {
		_ = client.connection.Close()
		if lamp.opts.verbose {
			lamp.logger.Printf("WebSocket disconnected (%d clients)", count)
		}
	}
}

func (lamp *mockLamp) broadcast(message string) {
	lamp.mu.RLock()
	clients := make([]*wsClient, 0, len(lamp.clients))
	for client := range lamp.clients {
		clients = append(clients, client)
	}
	lamp.mu.RUnlock()

	for _, client := range clients {
		if err := client.write(message); err != nil {
			lamp.unregister(client)
		}
	}
	if lamp.opts.verbose {
		lamp.logger.Printf("broadcast %q to %d clients", message, len(clients))
	}
}

func (lamp *mockLamp) disconnectClients() {
	lamp.mu.Lock()
	clients := make([]*wsClient, 0, len(lamp.clients))
	for client := range lamp.clients {
		clients = append(clients, client)
		delete(lamp.clients, client)
	}
	lamp.mu.Unlock()
	for _, client := range clients {
		_ = client.connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseServiceRestart, "mock disconnect"),
			time.Now().Add(time.Second),
		)
		_ = client.connection.Close()
	}
}

const (
	mockTelnetIAC  = byte(255)
	mockTelnetDONT = byte(254)
	mockTelnetDO   = byte(253)
	mockTelnetWONT = byte(252)
	mockTelnetWILL = byte(251)
	mockTelnetSB   = byte(250)
	mockTelnetSE   = byte(240)
)

type mockTelnetInputState uint8

const (
	mockTelnetData mockTelnetInputState = iota
	mockTelnetCommand
	mockTelnetOption
	mockTelnetSubnegotiation
	mockTelnetSubnegotiationIAC
)

type mockTelnetInputReader struct {
	source  io.Reader
	state   mockTelnetInputState
	scratch [1024]byte
}

func (reader *mockTelnetInputReader) Read(output []byte) (int, error) {
	if len(output) == 0 {
		return 0, nil
	}
	for {
		readLimit := len(output)
		if readLimit > len(reader.scratch) {
			readLimit = len(reader.scratch)
		}
		count, err := reader.source.Read(reader.scratch[:readLimit])
		written := 0
		for _, value := range reader.scratch[:count] {
			switch reader.state {
			case mockTelnetData:
				if value == mockTelnetIAC {
					reader.state = mockTelnetCommand
				} else {
					output[written] = value
					written++
				}
			case mockTelnetCommand:
				switch value {
				case mockTelnetIAC:
					output[written] = mockTelnetIAC
					written++
					reader.state = mockTelnetData
				case mockTelnetWILL, mockTelnetWONT, mockTelnetDO, mockTelnetDONT:
					reader.state = mockTelnetOption
				case mockTelnetSB:
					reader.state = mockTelnetSubnegotiation
				default:
					reader.state = mockTelnetData
				}
			case mockTelnetOption:
				reader.state = mockTelnetData
			case mockTelnetSubnegotiation:
				if value == mockTelnetIAC {
					reader.state = mockTelnetSubnegotiationIAC
				}
			case mockTelnetSubnegotiationIAC:
				if value == mockTelnetSE {
					reader.state = mockTelnetData
				} else {
					reader.state = mockTelnetSubnegotiation
				}
			}
		}
		if written > 0 || err != nil {
			return written, err
		}
	}
}

func (lamp *mockLamp) startTerminal(listener net.Listener) <-chan error {
	failure := make(chan error, 1)
	if listener == nil {
		failure <- errors.New("terminal listener is nil")
		close(failure)
		return failure
	}

	lamp.terminalMu.Lock()
	if lamp.terminalListener != nil {
		lamp.terminalMu.Unlock()
		_ = listener.Close()
		failure <- errors.New("terminal listener is already running")
		close(failure)
		return failure
	}
	lamp.terminalListener = listener
	lamp.terminalConnections = make(map[net.Conn]struct{})
	lamp.terminalStopping = false
	lamp.terminalWG.Add(1)
	lamp.terminalMu.Unlock()

	go lamp.acceptTerminal(listener, failure)
	return failure
}

func (lamp *mockLamp) acceptTerminal(
	listener net.Listener,
	failure chan<- error,
) {
	defer lamp.terminalWG.Done()
	defer close(failure)
	for {
		connection, err := listener.Accept()
		if err != nil {
			lamp.terminalMu.Lock()
			stopping := lamp.terminalStopping ||
				lamp.terminalListener != listener
			lamp.terminalMu.Unlock()
			if stopping {
				failure <- nil
			} else {
				failure <- err
			}
			return
		}

		lamp.terminalMu.Lock()
		if lamp.terminalStopping || lamp.terminalListener != listener {
			lamp.terminalMu.Unlock()
			_ = connection.Close()
			continue
		}
		lamp.terminalConnections[connection] = struct{}{}
		lamp.terminalWG.Add(1)
		lamp.terminalMu.Unlock()
		go lamp.serveTerminal(connection)
	}
}

func (lamp *mockLamp) serveTerminal(connection net.Conn) {
	defer func() {
		_ = connection.Close()
		lamp.terminalMu.Lock()
		delete(lamp.terminalConnections, connection)
		lamp.terminalMu.Unlock()
		lamp.terminalWG.Done()
	}()

	banner := append(
		[]byte{mockTelnetIAC, mockTelnetWILL, 1},
		[]byte(
			"\x1b[2J\x1b[H"+
				"Rayan Lamp mock terminal\r\n"+
				"Type help for bounded test commands.\r\n> ",
		)...,
	)
	if err := writeTerminal(connection, banner); err != nil {
		return
	}

	input := &mockTelnetInputReader{source: connection}
	reader := bufio.NewReaderSize(input, terminalReadBuffer)
	for {
		line, err := readBoundedTerminalLine(reader)
		if err != nil {
			if errors.Is(err, errTerminalLineTooLong) {
				if writeErr := writeTerminal(
					connection,
					[]byte("ERR terminal line too long or invalid\r\n> "),
				); writeErr != nil {
					return
				}
				continue
			}
			return
		}
		response, quit := lamp.executeTerminalCommand(line)
		if response != "" {
			if err := writeTerminal(connection, []byte(response)); err != nil {
				return
			}
		}
		if quit {
			return
		}
		if err := writeTerminal(connection, []byte("> ")); err != nil {
			return
		}
	}
}

// readBoundedTerminalLine consumes one complete logical line while retaining
// at most maxTerminalLineBytes. An oversized line is drained through its
// newline before the error is returned, so rejecting it cannot leave unread
// TCP input that turns the subsequent response into a reset-on-close race.
func readBoundedTerminalLine(reader *bufio.Reader) (string, error) {
	line := make([]byte, 0, terminalReadBuffer)
	tooLong := false
	for {
		fragment, err := reader.ReadSlice('\n')
		content := fragment
		if err == nil && len(content) > 0 {
			content = content[:len(content)-1]
		}
		if !tooLong {
			if len(line)+len(content) > maxTerminalLineBytes {
				tooLong = true
			} else {
				line = append(line, content...)
			}
		}

		switch {
		case err == nil:
			if tooLong {
				return "", errTerminalLineTooLong
			}
			return string(line), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(fragment) == 0 {
				return "", io.EOF
			}
			if tooLong {
				return "", errTerminalLineTooLong
			}
			return string(line), nil
		default:
			return "", err
		}
	}
}

func (lamp *mockLamp) executeTerminalCommand(line string) (string, bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if line == "" {
		return "", false
	}
	command := line
	argument := ""
	if separator := strings.IndexFunc(line, unicode.IsSpace); separator >= 0 {
		command = line[:separator]
		argument = strings.TrimSpace(line[separator:])
	}

	switch strings.ToLower(command) {
	case "help":
		return "Commands: help state on off toggle message <text> beep [1-10] quit\r\n", false
	case "state":
		return lamp.currentState() + "\r\n", false
	case "on":
		lamp.setState("ON")
		lamp.broadcast("ON")
		return "OK ON\r\n", false
	case "off":
		lamp.setState("OFF")
		lamp.broadcast("OFF")
		return "OK OFF\r\n", false
	case "toggle":
		lamp.mu.Lock()
		if lamp.state == "ON" {
			lamp.state = "OFF"
		} else {
			lamp.state = "ON"
		}
		state := lamp.state
		lamp.mu.Unlock()
		lamp.broadcast(state)
		return "OK " + state + "\r\n", false
	case "message":
		if err := validateMessage(argument); err != nil {
			return "ERR " + err.Error() + "\r\n", false
		}
		lamp.broadcast("message:" + strings.TrimSpace(argument))
		return "OK message\r\n", false
	case "beep":
		count := 1
		if argument != "" {
			parsed, err := strconv.Atoi(argument)
			if err != nil || parsed < 1 || parsed > 10 {
				return "ERR beep count must be between 1 and 10\r\n", false
			}
			count = parsed
		}
		lamp.broadcast(fmt.Sprintf("beep:%d", count))
		return fmt.Sprintf("OK beep:%d\r\n", count), false
	case "quit":
		return "BYE\r\n", true
	default:
		return "ERR unknown command\r\n", false
	}
}

func writeTerminal(connection net.Conn, payload []byte) error {
	if err := connection.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	for len(payload) > 0 {
		written, err := connection.Write(payload)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return connection.SetWriteDeadline(time.Time{})
}

func (lamp *mockLamp) isTerminalStopping() bool {
	lamp.terminalMu.Lock()
	defer lamp.terminalMu.Unlock()
	return lamp.terminalStopping
}

func (lamp *mockLamp) stopTerminal() {
	lamp.terminalMu.Lock()
	if lamp.terminalStopping {
		lamp.terminalMu.Unlock()
		lamp.terminalWG.Wait()
		return
	}
	listener := lamp.terminalListener
	if listener == nil && len(lamp.terminalConnections) == 0 {
		lamp.terminalMu.Unlock()
		return
	}
	lamp.terminalStopping = true
	lamp.terminalListener = nil
	connections := make([]net.Conn, 0, len(lamp.terminalConnections))
	for connection := range lamp.terminalConnections {
		connections = append(connections, connection)
	}
	lamp.terminalMu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
	for _, connection := range connections {
		_ = connection.Close()
	}
	lamp.terminalWG.Wait()
}

func (lamp *mockLamp) cycleState(ctx context.Context, interval time.Duration) {
	timer := time.NewTicker(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			lamp.mu.Lock()
			if lamp.state == "ON" {
				lamp.state = "OFF"
			} else {
				lamp.state = "ON"
			}
			state := lamp.state
			lamp.mu.Unlock()
			lamp.broadcast(state)
		}
	}
}

func (client *wsClient) write(message string) error {
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	if err := client.connection.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	return client.connection.WriteMessage(websocket.TextMessage, []byte(message))
}

func validateListenAddress(address string, allowNonLocal bool) error {
	return validateNamedListenAddress("-listen", address, allowNonLocal)
}

func validateNamedListenAddress(
	option string,
	address string,
	allowNonLocal bool,
) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid %s address: %w", option, err)
	}
	if host == "" || port == "" {
		return fmt.Errorf("%s must include an explicit host and port", option)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("%s port must be numeric", option)
	}
	if allowNonLocal || strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf(
			"%s must be loopback unless -allow-non-loopback is explicitly set",
			option,
		)
	}
	return nil
}

func normalizeState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on":
		return "on"
	case "off":
		return "off"
	default:
		return ""
	}
}

func validateMessage(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return errors.New("message is required")
	}
	if len(message) > maxMessageBytes {
		return fmt.Errorf("message exceeds %d bytes", maxMessageBytes)
	}
	if !utf8.ValidString(message) || strings.IndexByte(message, 0) >= 0 {
		return errors.New("message must be valid UTF-8 without NUL")
	}
	for _, value := range message {
		if unicode.IsControl(value) && value != '\r' && value != '\n' && value != '\t' {
			return errors.New("message contains an unsupported control character")
		}
	}
	return nil
}

func requireMethod(w http.ResponseWriter, request *http.Request, method string) bool {
	if request.Method == method {
		return true
	}
	methodNotAllowed(w, method)
	return false
}

func methodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeJSON(w, http.StatusMethodNotAllowed, apiResponse{OK: false, Error: "method not allowed"})
}

func writeText(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		status = http.StatusInternalServerError
		data = []byte(`{"ok":false,"error":"JSON encoding failed"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}
