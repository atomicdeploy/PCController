// Package managedbrowser launches a browser app whose authenticated requests
// are mediated by an inherited DevTools pipe. The durable controller token is
// never placed in browser-visible storage or process metadata.
package managedbrowser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
)

const (
	maxProtocolMessageBytes = 16 << 20
	maxQueuedProtocolEvents = 4096
)

type protocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type protocolMessage struct {
	ID        uint64          `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *protocolError  `json:"error,omitempty"`
}

type protocolRequest struct {
	ID        uint64 `json:"id"`
	Method    string `json:"method"`
	SessionID string `json:"sessionId,omitempty"`
	Params    any    `json:"params,omitempty"`
}

type protocolEventQueue struct {
	mu     sync.Mutex
	values []protocolMessage
	head   int
	wake   chan struct{}
	closed bool
}

func newProtocolEventQueue() *protocolEventQueue {
	return &protocolEventQueue{wake: make(chan struct{}, 1)}
}

func (queue *protocolEventQueue) push(message protocolMessage) error {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed {
		return errors.New("Chrome DevTools event queue is closed")
	}
	if len(queue.values)-queue.head >= maxQueuedProtocolEvents {
		return errors.New("Chrome DevTools event queue exceeded its safety limit")
	}
	queue.values = append(queue.values, message)
	select {
	case queue.wake <- struct{}{}:
	default:
	}
	return nil
}

func (queue *protocolEventQueue) close() {
	queue.mu.Lock()
	queue.closed = true
	queue.mu.Unlock()
	select {
	case queue.wake <- struct{}{}:
	default:
	}
}

func (queue *protocolEventQueue) next(ctx context.Context) (protocolMessage, bool) {
	for {
		queue.mu.Lock()
		if queue.head < len(queue.values) {
			message := queue.values[queue.head]
			queue.values[queue.head] = protocolMessage{}
			queue.head++
			if queue.head == len(queue.values) {
				queue.values = queue.values[:0]
				queue.head = 0
			}
			queue.mu.Unlock()
			return message, true
		}
		closed := queue.closed
		queue.mu.Unlock()
		if closed {
			return protocolMessage{}, false
		}
		select {
		case <-ctx.Done():
			return protocolMessage{}, false
		case <-queue.wake:
		}
	}
}

type protocolClient struct {
	input   io.WriteCloser
	output  io.ReadCloser
	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan protocolMessage
	events  *protocolEventQueue
	errMu   sync.Mutex
	err     error
}

func newProtocolClient(input io.WriteCloser, output io.ReadCloser) *protocolClient {
	client := &protocolClient{
		input: input, output: output, nextID: 1,
		pending: make(map[uint64]chan protocolMessage),
		events:  newProtocolEventQueue(),
	}
	go client.read()
	return client
}

func (client *protocolClient) read() {
	defer client.events.close()
	reader := bufio.NewReaderSize(client.output, 64*1024)
	for {
		payload, err := readASCIIZ(reader, maxProtocolMessageBytes)
		if err != nil {
			client.setError(err)
			client.failPending()
			return
		}
		var message protocolMessage
		if err = json.Unmarshal(payload, &message); err != nil {
			client.setError(errors.New("Chrome DevTools pipe returned invalid JSON"))
			client.failPending()
			return
		}
		if message.ID != 0 {
			client.mu.Lock()
			response := client.pending[message.ID]
			delete(client.pending, message.ID)
			client.mu.Unlock()
			if response != nil {
				response <- message
				close(response)
			}
			continue
		}
		if err = client.events.push(message); err != nil {
			client.setError(err)
			client.failPending()
			return
		}
	}
}

func (client *protocolClient) nextEvent(ctx context.Context) (protocolMessage, bool) {
	return client.events.next(ctx)
}

func readASCIIZ(reader *bufio.Reader, maximum int) ([]byte, error) {
	var result []byte
	for {
		fragment, err := reader.ReadSlice(0)
		if len(result)+len(fragment) > maximum+1 {
			return nil, errors.New("Chrome DevTools pipe message exceeded its safety limit")
		}
		result = append(result, fragment...)
		if err == nil {
			return result[:len(result)-1], nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}

func (client *protocolClient) setError(err error) {
	client.errMu.Lock()
	if client.err == nil {
		client.err = err
	}
	client.errMu.Unlock()
}

func (client *protocolClient) currentError() error {
	client.errMu.Lock()
	defer client.errMu.Unlock()
	if client.err == nil {
		return errors.New("Chrome DevTools pipe closed")
	}
	return client.err
}

func (client *protocolClient) failPending() {
	client.mu.Lock()
	defer client.mu.Unlock()
	for id, response := range client.pending {
		delete(client.pending, id)
		close(response)
	}
}

func (client *protocolClient) call(
	ctx context.Context,
	sessionID, method string,
	params, result any,
	sensitive bool,
) error {
	client.mu.Lock()
	id := client.nextID
	client.nextID++
	response := make(chan protocolMessage, 1)
	client.pending[id] = response
	client.mu.Unlock()

	payload, err := json.Marshal(protocolRequest{
		ID: id, Method: method, SessionID: sessionID, Params: params,
	})
	if err != nil {
		client.mu.Lock()
		delete(client.pending, id)
		client.mu.Unlock()
		return fmt.Errorf("encode Chrome DevTools %s request: %w", method, err)
	}
	payload = append(payload, 0)
	client.mu.Lock()
	_, err = client.input.Write(payload)
	client.mu.Unlock()
	if sensitive {
		for index := range payload {
			payload[index] = 0
		}
	}
	if err != nil {
		client.mu.Lock()
		delete(client.pending, id)
		client.mu.Unlock()
		return fmt.Errorf("send Chrome DevTools %s request: %w", method, err)
	}
	select {
	case <-ctx.Done():
		client.mu.Lock()
		delete(client.pending, id)
		client.mu.Unlock()
		return ctx.Err()
	case message, ok := <-response:
		if !ok {
			return client.currentError()
		}
		if message.Error != nil {
			return fmt.Errorf("Chrome DevTools %s failed (%d)", method, message.Error.Code)
		}
		if result != nil && len(message.Result) != 0 {
			if err = json.Unmarshal(message.Result, result); err != nil {
				return fmt.Errorf("decode Chrome DevTools %s response: %w", method, err)
			}
		}
		return nil
	}
}

func (client *protocolClient) close() {
	_ = client.input.Close()
	_ = client.output.Close()
}

type fetchHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type frameAuthority struct {
	targetOrigin string
	frames       map[string]struct{}
}

func newFrameAuthority(targetOrigin string) *frameAuthority {
	return &frameAuthority{
		targetOrigin: strings.ToLower(strings.TrimSpace(targetOrigin)),
		frames:       make(map[string]struct{}),
	}
}

func (authority *frameAuthority) navigate(frameID, value string) {
	frameID = strings.TrimSpace(frameID)
	if authority == nil || frameID == "" {
		return
	}
	if requestOrigin(value) == authority.targetOrigin {
		authority.frames[frameID] = struct{}{}
		return
	}
	delete(authority.frames, frameID)
}

func (authority *frameAuthority) detach(frameID string) {
	if authority != nil {
		delete(authority.frames, strings.TrimSpace(frameID))
	}
}

func (authority *frameAuthority) allows(frameID, requestURL string) bool {
	if authority == nil {
		return false
	}
	if _, ok := authority.frames[strings.TrimSpace(frameID)]; !ok {
		return false
	}
	return managedAPIRequest(authority.targetOrigin, requestURL)
}

func requestOrigin(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func authenticatedRequestHeaders(
	frameAuthorized bool,
	targetOrigin, requestURL string,
	current map[string]string,
	token string,
) ([]fetchHeader, bool) {
	if !frameAuthorized || !managedAPIRequest(targetOrigin, requestURL) || strings.TrimSpace(token) == "" {
		return nil, false
	}
	names := make([]string, 0, len(current))
	for name := range current {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-PCController-Token") {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(left, right int) bool {
		return strings.ToLower(names[left]) < strings.ToLower(names[right])
	})
	result := make([]fetchHeader, 0, len(names)+1)
	for _, name := range names {
		result = append(result, fetchHeader{Name: name, Value: current[name]})
	}
	result = append(result, fetchHeader{Name: "Authorization", Value: "Bearer " + strings.TrimSpace(token)})
	return result, true
}

func managedAPIRequest(targetOrigin, requestURL string) bool {
	target, targetErr := url.Parse(targetOrigin)
	request, requestErr := url.Parse(requestURL)
	if targetErr != nil || requestErr != nil || target.User != nil || request.User != nil {
		return false
	}
	if !strings.EqualFold(target.Scheme, request.Scheme) || !strings.EqualFold(target.Host, request.Host) {
		return false
	}
	path := request.EscapedPath()
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

func authenticationProofRequest(targetOrigin, requestURL string) bool {
	if !managedAPIRequest(targetOrigin, requestURL) {
		return false
	}
	request, err := url.Parse(requestURL)
	if err != nil {
		return false
	}
	return request.EscapedPath() == "/api/snapshot" || request.EscapedPath() == "/api/session/ticket"
}

func cleanLoopbackURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" {
		return nil, errors.New("managed browser URL must be a clean absolute loopback HTTP URL")
	}
	host := strings.Trim(parsed.Hostname(), "[]")
	address := net.ParseIP(host)
	if address == nil || !address.IsLoopback() || parsed.Port() == "" {
		return nil, errors.New("managed browser URL must use a literal loopback IP and explicit port")
	}
	parsed.Scheme = "http"
	parsed.Host = net.JoinHostPort(address.String(), parsed.Port())
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed, nil
}
