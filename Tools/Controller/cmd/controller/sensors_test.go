package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/boardsensors"
)

func TestBoardSensorFeederBoundsAndReadOnlyRequest(t *testing.T) {
	now := time.Now()
	zero := 0.0
	s := boardsensors.Snapshot{Source: "pccontroller-board-status", Connected: true, Fresh: true, ObservedAt: now, ExpiresAt: now.Add(boardsensors.MaxAge), Readings: []boardsensors.Reading{{ID: "temperature_led", Unit: "C", Value: &zero}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("feeder issued a mutation", r.Method)
		}
		if r.URL.Path == "/oversized" {
			io.WriteString(w, strings.Repeat(" ", 65537))
			return
		}
		json.NewEncoder(w).Encode(s)
	}))
	defer server.Close()
	frame, err := readBoardSensorFrame(context.Background(), server.Client(), server.URL)
	if err != nil || !strings.Contains(frame, " 16 0 0 0 0 0 0\n") {
		t.Fatal(frame, err)
	}
	if _, err = readBoardSensorFrame(context.Background(), server.Client(), server.URL+"/oversized"); err == nil {
		t.Fatal("accepted oversized sensor response")
	}
}

func TestBoardSensorFeederRejectsRemoteCredentialsAndPathsBeforeDeviceAccess(t *testing.T) {
	for _, endpoint := range []string{"http://example.com:8787", "http://localhost:8787", "http://127.0.0.1:8787/api", "http://user:secret@127.0.0.1:8787", "http://127.0.0.1:8787?token=secret", "https://127.0.0.1:8787"} {
		err := runSensors([]string{"hwmon", "--url", endpoint}, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "literal loopback HTTP origin") {
			t.Fatal(endpoint, err)
		}
	}
}
