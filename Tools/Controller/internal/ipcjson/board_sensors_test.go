package ipcjson

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestBoardSensorHTTPReadOnlyAndNoSyntheticReadings(t *testing.T) {
	r := control.New(control.Options{})
	defer r.Close()
	s := &Service{Client: controller.AttachSharedRuntime(r, shell.New(8)), AuthorizationDisabled: true}
	h := websocketMux(context.Background(), s)
	for _, path := range []string{"/api/sensors/board", "/api/sensors/metrics"} {
		w := httptest.NewRecorder()
		request := httptest.NewRequest("GET", path, nil)
		request.RemoteAddr = "127.0.0.1:12345"
		h.ServeHTTP(w, request)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		if strings.HasSuffix(path, "metrics") && strings.Contains(w.Body.String(), "pccontroller_board_supply_volts ") {
			t.Fatal(w.Body.String())
		}
		w = httptest.NewRecorder()
		request = httptest.NewRequest("POST", path, nil)
		request.RemoteAddr = "127.0.0.1:12345"
		h.ServeHTTP(w, request)
		if w.Code != 405 {
			t.Fatal("sensor exporter accepted mutation", w.Code)
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 403 {
			t.Fatal("sensor exporter bypassed remote policy", w.Code)
		}
	}
}
