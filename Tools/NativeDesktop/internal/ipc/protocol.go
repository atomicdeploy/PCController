package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	ProtocolVersion = 1
	MaxMessageBytes = 1 << 20
)

type Request struct {
	ProtocolVersion int             `json:"protocol_version"`
	ID              string          `json:"id,omitempty"`
	Method          string          `json:"method"`
	Params          json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ProtocolVersion int             `json:"protocol_version"`
	ID              string          `json:"id,omitempty"`
	OK              bool            `json:"ok"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Handler func(context.Context, Request) (any, error)

type CodedError interface {
	error
	Code() string
}

func NewRequest(id, method string, params any) (Request, error) {
	req := Request{
		ProtocolVersion: ProtocolVersion,
		ID:              id,
		Method:          method,
	}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return Request{}, fmt.Errorf("encode IPC parameters: %w", err)
		}
		req.Params = data
	}
	return req, nil
}

func Success(id string, result any) Response {
	resp := Response{ProtocolVersion: ProtocolVersion, ID: id, OK: true}
	if result == nil {
		return resp
	}
	data, err := json.Marshal(result)
	if err != nil {
		return Failure(id, "internal_error", "unable to encode response")
	}
	resp.Result = data
	return resp
}

func Failure(id, code, message string) Response {
	return Response{
		ProtocolVersion: ProtocolVersion,
		ID:              id,
		OK:              false,
		Error:           &Error{Code: code, Message: message},
	}
}

func FailureFrom(id string, err error) Response {
	if err == nil {
		return Success(id, nil)
	}
	code := "internal_error"
	var coded CodedError
	if errors.As(err, &coded) {
		code = coded.Code()
	}
	return Failure(id, code, err.Error())
}

func (r Response) DecodeResult(target any) error {
	if !r.OK {
		if r.Error == nil {
			return errors.New("IPC command failed")
		}
		return fmt.Errorf("%s: %s", r.Error.Code, r.Error.Message)
	}
	if target == nil || len(r.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.Result, target); err != nil {
		return fmt.Errorf("decode IPC result: %w", err)
	}
	return nil
}
