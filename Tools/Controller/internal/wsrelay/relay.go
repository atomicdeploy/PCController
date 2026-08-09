package wsrelay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	MessageVersion = 1
	MessageType    = "firmware"
	DefaultMaxSize = 4 << 20
)

type FirmwareMessage struct {
	Version      int    `json:"version"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	SHA256       string `json:"sha256"`
	ModifiedUnix int64  `json:"modified_unix"`
	Data         []byte `json:"data"`
}

func Load(path string, maxSize int64) (FirmwareMessage, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	info, err := os.Stat(path)
	if err != nil {
		return FirmwareMessage{}, err
	}
	if info.IsDir() {
		return FirmwareMessage{}, errors.New("firmware path is a directory")
	}
	if info.Size() < 1 || info.Size() > maxSize {
		return FirmwareMessage{}, fmt.Errorf(
			"firmware size %d is outside 1..%d",
			info.Size(),
			maxSize,
		)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FirmwareMessage{}, err
	}
	sum := sha256.Sum256(data)
	return FirmwareMessage{
		Version:      MessageVersion,
		Type:         MessageType,
		Name:         filepath.Base(path),
		SHA256:       hex.EncodeToString(sum[:]),
		ModifiedUnix: info.ModTime().Unix(),
		Data:         data,
	}, nil
}

func Decode(data []byte, maxSize int64) (FirmwareMessage, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	var message FirmwareMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return FirmwareMessage{}, fmt.Errorf("decode firmware message: %w", err)
	}
	if message.Version != MessageVersion || message.Type != MessageType {
		return FirmwareMessage{}, fmt.Errorf(
			"unsupported firmware message version=%d type=%q",
			message.Version,
			message.Type,
		)
	}
	if len(message.Data) < 1 || int64(len(message.Data)) > maxSize {
		return FirmwareMessage{}, fmt.Errorf(
			"firmware payload size %d is outside 1..%d",
			len(message.Data),
			maxSize,
		)
	}
	if filepath.Base(message.Name) != message.Name ||
		strings.ContainsAny(message.Name, `/\`) {
		return FirmwareMessage{}, errors.New("firmware name must be a base name")
	}
	sum := sha256.Sum256(message.Data)
	if !strings.EqualFold(message.SHA256, hex.EncodeToString(sum[:])) {
		return FirmwareMessage{}, errors.New("firmware SHA-256 mismatch")
	}
	return message, nil
}

func Encode(message FirmwareMessage) ([]byte, error) {
	if _, err := DecodeMustValidate(message); err != nil {
		return nil, err
	}
	return json.Marshal(message)
}

func DecodeMustValidate(message FirmwareMessage) (FirmwareMessage, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return FirmwareMessage{}, err
	}
	maxSize := int64(len(message.Data))
	if maxSize < DefaultMaxSize {
		maxSize = DefaultMaxSize
	}
	return Decode(data, maxSize)
}

type hub struct {
	mu      sync.Mutex
	latest  []byte
	clients map[chan []byte]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[chan []byte]struct{})}
}

func (hub *hub) publish(message []byte) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.latest = append([]byte(nil), message...)
	for client := range hub.clients {
		select {
		case client <- append([]byte(nil), message...):
		default:
			select {
			case <-client:
			default:
			}
			select {
			case client <- append([]byte(nil), message...):
			default:
			}
		}
	}
}

func (hub *hub) subscribe() (chan []byte, func()) {
	channel := make(chan []byte, 2)
	hub.mu.Lock()
	hub.clients[channel] = struct{}{}
	if len(hub.latest) != 0 {
		channel <- append([]byte(nil), hub.latest...)
	}
	hub.mu.Unlock()
	return channel, func() {
		hub.mu.Lock()
		delete(hub.clients, channel)
		close(channel)
		hub.mu.Unlock()
	}
}

type ServerOptions struct {
	Listen       string
	Path         string
	FirmwarePath string
	PollInterval time.Duration
	MaxSize      int64
	Logger       *log.Logger
}

func Serve(ctx context.Context, options ServerOptions) error {
	if options.Listen == "" {
		options.Listen = "127.0.0.1:3000"
	}
	if options.Path == "" {
		options.Path = "/firmware"
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 500 * time.Millisecond
	}
	if options.MaxSize <= 0 {
		options.MaxSize = DefaultMaxSize
	}
	if options.Logger == nil {
		options.Logger = log.Default()
	}
	if options.FirmwarePath == "" {
		return errors.New("firmware path is required")
	}

	relay := newHub()
	mux := http.NewServeMux()
	mux.HandleFunc(options.Path, func(writer http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			options.Logger.Printf("WebSocket accept: %v", err)
			return
		}
		defer connection.CloseNow()
		messages, unsubscribe := relay.subscribe()
		defer unsubscribe()
		options.Logger.Printf("client connected: %s", request.RemoteAddr)
		for {
			select {
			case <-ctx.Done():
				_ = connection.Close(websocket.StatusGoingAway, "server stopping")
				return
			case message := <-messages:
				writeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := connection.Write(writeContext, websocket.MessageText, message)
				cancel()
				if err != nil {
					options.Logger.Printf("client %s write: %v", request.RemoteAddr, err)
					return
				}
			}
		}
	})
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("ok\n"))
	})

	server := &http.Server{
		Addr:              options.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		options.Logger.Printf(
			"serving %s on ws://%s%s",
			options.FirmwarePath,
			options.Listen,
			options.Path,
		)
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
		close(serverErrors)
	}()

	watchErrors := make(chan error, 1)
	go func() {
		watchErrors <- watch(ctx, relay, options)
	}()

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-serverErrors:
		return err
	case err := <-watchErrors:
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
		return err
	}
}

func watch(ctx context.Context, relay *hub, options ServerOptions) error {
	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()
	var previous string
	for {
		message, err := Load(options.FirmwarePath, options.MaxSize)
		if err == nil && message.SHA256 != previous {
			encoded, encodeErr := Encode(message)
			if encodeErr != nil {
				return encodeErr
			}
			relay.publish(encoded)
			previous = message.SHA256
			options.Logger.Printf(
				"published %s (%d bytes, sha256 %s)",
				message.Name,
				len(message.Data),
				message.SHA256,
			)
		} else if err != nil {
			options.Logger.Printf("watch: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

type ClientOptions struct {
	URL            string
	ReconnectDelay time.Duration
	MaxSize        int64
	Logger         *log.Logger
	OnFirmware     func(context.Context, FirmwareMessage) error
}

func RunClient(ctx context.Context, options ClientOptions) error {
	if options.URL == "" {
		return errors.New("WebSocket URL is required")
	}
	if options.ReconnectDelay <= 0 {
		options.ReconnectDelay = 2 * time.Second
	}
	if options.MaxSize <= 0 {
		options.MaxSize = DefaultMaxSize
	}
	if options.Logger == nil {
		options.Logger = log.Default()
	}
	if options.OnFirmware == nil {
		return errors.New("firmware callback is required")
	}

	for {
		err := runConnection(ctx, options)
		if ctx.Err() != nil {
			return nil
		}
		options.Logger.Printf("connection ended: %v; reconnecting", err)
		timer := time.NewTimer(options.ReconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func runConnection(ctx context.Context, options ClientOptions) error {
	connection, _, err := websocket.Dial(ctx, options.URL, nil)
	if err != nil {
		return err
	}
	defer connection.CloseNow()
	connection.SetReadLimit(options.MaxSize*2 + 4096)
	options.Logger.Printf("connected %s", options.URL)

	for {
		messageType, data, err := connection.Read(ctx)
		if err != nil {
			return err
		}
		if messageType != websocket.MessageText {
			continue
		}
		message, err := Decode(data, options.MaxSize)
		if err != nil {
			options.Logger.Printf("rejected firmware message: %v", err)
			continue
		}
		if err := options.OnFirmware(ctx, message); err != nil {
			options.Logger.Printf("firmware %s failed: %v", message.Name, err)
			continue
		}
		options.Logger.Printf("firmware %s processed successfully", message.Name)
	}
}

func SaveTemp(message FirmwareMessage) (string, func(), error) {
	extension := strings.ToLower(filepath.Ext(message.Name))
	if extension != ".hex" {
		extension = ".hex"
	}
	file, err := os.CreateTemp("", "pccontroller-*"+extension)
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := file.Write(message.Data); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}
