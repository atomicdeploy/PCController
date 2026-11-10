package ipcjson

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pccontroller.local/controller/internal/hostui"
)

type osdFixture struct {
	shown     int
	dismissed string
	request   hostui.OSDRequest
}

func TestOSDRPCAcrossNDJSONAndWebSocket(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	service.AuthorizationDisabled = true
	fake := &osdFixture{}
	service.DesktopOSD = fake
	request := `{"jsonrpc":"2.0","id":1,"method":"controller.osd.show","params":{"id":"transport-fixture","text":"Ready"}}`
	var raw bytes.Buffer
	if err := serveStreams(context.Background(), strings.NewReader(request+"\n"), &raw, service, Access{Transport: "ipc"}); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(raw.Bytes(), &response); err != nil || response.Error != nil || fake.shown != 1 {
		t.Fatalf("NDJSON: %s (%v)", raw.String(), err)
	}
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ipc", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if err = connection.Write(ctx, websocket.MessageText, []byte(request)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(payload, &response); err != nil || response.Error != nil || fake.shown != 2 {
		t.Fatalf("WebSocket: %s (%v)", payload, err)
	}
}

func (f *osdFixture) Show(_ context.Context, r hostui.OSDRequest) (hostui.OSDStatus, error) {
	f.shown++
	f.request = r
	return hostui.OSDStatus{Supported: true, Active: true, ID: r.ID}, nil
}
func (f *osdFixture) Dismiss(_ context.Context, id string) (hostui.OSDStatus, error) {
	f.dismissed = id
	return hostui.OSDStatus{Supported: true}, nil
}
func (f *osdFixture) Status() hostui.OSDStatus                { return hostui.OSDStatus{Supported: true} }
func (f *osdFixture) Result(string) (hostui.OSDStatus, error) { return f.Status(), nil }

func TestOSDHTTPAdmissionReadOnlyStatusAndStrictPayload(t *testing.T) {
	fake := &osdFixture{}
	service := &Service{DesktopOSD: fake}
	handler := websocketMux(context.Background(), service)
	for _, tc := range []struct {
		method, body, remote string
		code                 int
	}{{"GET", "", "127.0.0.1:12", 200}, {"POST", `{"text":"Ready","id":"one","duration_ms":8000,"actions":[{"label":"Events","uri":"pccontroller://page/events"}]}`, "127.0.0.1:12", 200}, {"POST", `{"text":"Ready","icon":"http://remote/icon.png"}`, "127.0.0.1:12", 400}, {"POST", `{"text":"Ready","unknown":true}`, "127.0.0.1:12", 400}, {"POST", `{"text":"Ready","actions":[{"label":"Run","uri":"file:///unsafe"}]}`, "127.0.0.1:12", 400}, {"POST", `{"text":"Ready"}`, "192.0.2.1:12", 403}, {"DELETE", `{"id":"one"}`, "127.0.0.1:12", 200}, {"PUT", `{}`, "127.0.0.1:12", 405}} {
		r := httptest.NewRequest(tc.method, "/api/osd", strings.NewReader(tc.body))
		r.RemoteAddr = tc.remote
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d %s", tc.method, tc.body, w.Code, w.Body.String())
		}
	}
	if fake.shown != 1 || fake.dismissed != "one" || fake.request.DurationMS != 8000 {
		t.Fatal(fake)
	}
	for method, want := range map[string]string{"controller.osd.show": capabilityMessages, "controller.osd.dismiss": capabilityMessages, "controller.osd.status": capabilityRead} {
		if got := requestCapability(method, nil); got != want {
			t.Fatal(method, got)
		}
	}
}
