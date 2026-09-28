package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
)

type Caller interface {
	Call(context.Context, Request) (Response, error)
}

type Client struct {
	caller Caller
}

func NewClient(caller Caller) (*Client, error) {
	if caller == nil {
		return nil, errors.New("RPC caller is required")
	}
	return &Client{caller: caller}, nil
}

func NewInProcess(dispatch func(context.Context, Request) Response) (*Client, error) {
	if dispatch == nil {
		return nil, errors.New("in-process RPC dispatcher is required")
	}
	return NewClient(dispatchCaller(dispatch))
}

func (client *Client) Call(ctx context.Context, request Request) (Response, error) {
	if client == nil || client.caller == nil {
		return Response{}, errors.New("RPC client is unavailable")
	}
	request = normalizeRequest(request)
	encoded, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	if len(encoded)+1 > MaxMessageBytes {
		return Response{}, fmt.Errorf("RPC request exceeds %d bytes", MaxMessageBytes)
	}
	return client.caller.Call(ctx, request)
}

type dispatchCaller func(context.Context, Request) Response

func (dispatch dispatchCaller) Call(ctx context.Context, request Request) (Response, error) {
	response := dispatch(ctx, request)
	if response.Error != nil {
		return response, response.Error
	}
	return response, nil
}

type DialContextFunc func(context.Context, string, string) (net.Conn, error)

type ClientOptions struct {
	// DialContext customizes TCP resolution/dialing. Native transports ignore
	// it and always use their platform-specific bounded dialer.
	DialContext DialContextFunc
}

type streamCaller struct {
	endpoint Endpoint
	options  ClientOptions
}

func Dial(endpoint Endpoint, options ClientOptions) (*Client, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	return NewClient(&streamCaller{endpoint: endpoint, options: options})
}

func Call(ctx context.Context, endpoint Endpoint, request Request, options ClientOptions) (Response, error) {
	client, err := Dial(endpoint, options)
	if err != nil {
		return Response{}, err
	}
	return client.Call(ctx, request)
}

func (caller *streamCaller) Call(ctx context.Context, request Request) (Response, error) {
	connection, err := dialEndpoint(ctx, caller.endpoint, caller.options)
	if err != nil {
		return Response{}, err
	}
	defer connection.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-finished:
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Response{}, ctxErr
		}
		return Response{}, err
	}
	var response Response
	decoder := json.NewDecoder(io.LimitReader(connection, MaxMessageBytes))
	if err := decoder.Decode(&response); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Response{}, ctxErr
		}
		return Response{}, err
	}
	if response.Error != nil {
		return response, response.Error
	}
	return response, nil
}

func normalizeRequest(request Request) Request {
	request.JSONRPC = Version
	if len(request.ID) == 0 {
		request.ID = json.RawMessage("1")
	}
	return request
}

func dialEndpoint(ctx context.Context, endpoint Endpoint, options ClientOptions) (net.Conn, error) {
	switch endpoint.Transport {
	case TransportTCP:
		dial := options.DialContext
		if dial == nil {
			dialer := &net.Dialer{}
			dial = dialer.DialContext
		}
		return dial(ctx, "tcp", endpoint.Address)
	case TransportNamedPipe, TransportUnix:
		return dialNativeEndpoint(ctx, endpoint)
	default:
		return nil, errors.New("RPC endpoint is not dialable")
	}
}
