package ipc

import (
	"encoding/json"
	"errors"
	"testing"
)

type testCodedError struct{}

func (testCodedError) Error() string { return "no device" }
func (testCodedError) Code() string  { return "offline" }

func TestResponseRoundTrip(t *testing.T) {
	response := Success("42", map[string]any{"state": "on"})
	var result struct {
		State string `json:"state"`
	}
	if err := response.DecodeResult(&result); err != nil {
		t.Fatal(err)
	}
	if result.State != "on" {
		t.Fatalf("state = %q", result.State)
	}

	failed := FailureFrom("42", testCodedError{})
	if failed.OK || failed.Error == nil || failed.Error.Code != "offline" {
		t.Fatalf("unexpected failure: %#v", failed)
	}
	if err := failed.DecodeResult(nil); err == nil {
		t.Fatal("expected DecodeResult error")
	}
}

func TestNewRequest(t *testing.T) {
	request, err := NewRequest("x", "toggle", map[string]bool{"force": true})
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]bool
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	if !params["force"] {
		t.Fatal("params lost")
	}

	if response := FailureFrom("x", errors.New("boom")); response.Error.Code != "internal_error" {
		t.Fatalf("code = %q", response.Error.Code)
	}
}
