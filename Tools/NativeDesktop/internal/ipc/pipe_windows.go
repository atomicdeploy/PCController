//go:build windows

package ipc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const pipePrefix = `\\.\pipe\PCControllerDesktop.`

type Server struct {
	listener  net.Listener
	handler   Handler
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error

	connectionsMu sync.Mutex
	connections   map[net.Conn]struct{}
	closed        bool
}

func PipeName() (string, error) {
	sid, err := currentUserSID()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sid))
	return pipePrefix + hex.EncodeToString(sum[:8]), nil
}

func Listen(handler Handler) (*Server, error) {
	if handler == nil {
		return nil, errors.New("IPC handler is nil")
	}
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	name, err := PipeName()
	if err != nil {
		return nil, err
	}
	security := fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)", sid)
	listener, err := winio.ListenPipe(name, &winio.PipeConfig{
		SecurityDescriptor: security,
		MessageMode:        false,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("listen on IPC pipe: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		listener:    listener,
		handler:     handler,
		ctx:         ctx,
		cancel:      cancel,
		connections: make(map[net.Conn]struct{}),
	}
	server.wg.Add(1)
	go server.serve()
	return server, nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.cancel()

		s.connectionsMu.Lock()
		s.closed = true
		connections := make([]net.Conn, 0, len(s.connections))
		for connection := range s.connections {
			connections = append(connections, connection)
		}
		s.connectionsMu.Unlock()

		s.closeErr = s.listener.Close()
		for _, connection := range connections {
			_ = connection.Close()
		}
		s.wg.Wait()
	})
	return s.closeErr
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			continue
		}
		if !s.trackConnection(conn) {
			_ = conn.Close()
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrackConnection(conn)
			defer conn.Close()
			s.handleConnection(conn)
		}()
	}
}

func (s *Server) trackConnection(connection net.Conn) bool {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	if s.closed {
		return false
	}
	s.connections[connection] = struct{}{}
	return true
}

func (s *Server) untrackConnection(connection net.Conn) {
	s.connectionsMu.Lock()
	delete(s.connections, connection)
	s.connectionsMu.Unlock()
}

func (s *Server) handleConnection(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	line, err := bufio.NewReader(io.LimitReader(conn, MaxMessageBytes+1)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		_ = json.NewEncoder(conn).Encode(Failure("", "bad_request", "unable to read IPC request"))
		return
	}
	if len(line) > MaxMessageBytes {
		_ = json.NewEncoder(conn).Encode(Failure("", "request_too_large", "IPC request exceeds size limit"))
		return
	}
	var request Request
	if err := decodeSingleJSON(bytes.NewReader(line), &request, true); err != nil {
		_ = json.NewEncoder(conn).Encode(Failure("", "bad_request", "invalid IPC request"))
		return
	}
	if request.ProtocolVersion != ProtocolVersion {
		_ = json.NewEncoder(conn).Encode(Failure(
			request.ID,
			"protocol_mismatch",
			fmt.Sprintf("unsupported IPC protocol %d", request.ProtocolVersion),
		))
		return
	}
	request.Method = strings.ToLower(strings.TrimSpace(request.Method))
	if request.Method == "" || len(request.Method) > 64 {
		_ = json.NewEncoder(conn).Encode(Failure(request.ID, "bad_request", "invalid IPC method"))
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, 12*time.Second)
	defer cancel()
	result, err := s.handler(ctx, request)
	response := FailureFrom(request.ID, err)
	if err == nil {
		response = Success(request.ID, result)
	}
	_ = json.NewEncoder(conn).Encode(response)
}

func Call(ctx context.Context, request Request) (Response, error) {
	name, err := PipeName()
	if err != nil {
		return Response{}, err
	}
	conn, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		return Response{}, fmt.Errorf("connect to PCControllerDesktop: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, fmt.Errorf("send IPC request: %w", err)
	}

	var response Response
	if err := decodeSingleJSON(io.LimitReader(conn, MaxMessageBytes+1), &response, false); err != nil {
		return Response{}, fmt.Errorf("read IPC response: %w", err)
	}
	if response.ProtocolVersion != ProtocolVersion {
		return Response{}, fmt.Errorf("IPC protocol mismatch: %d", response.ProtocolVersion)
	}
	return response, nil
}

func IsRunning(ctx context.Context) bool {
	request, _ := NewRequest("probe", "ping", nil)
	response, err := Call(ctx, request)
	return err == nil && response.OK
}

func decodeSingleJSON(reader io.Reader, target any, disallowUnknown bool) error {
	decoder := json.NewDecoder(reader)
	if disallowUnknown {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return fmt.Errorf("invalid trailing JSON data: %w", err)
	}
	return nil
}

func currentUserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("get current user SID: %w", err)
	}
	return user.User.Sid.String(), nil
}
