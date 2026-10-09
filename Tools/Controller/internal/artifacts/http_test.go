package artifacts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHTTPUploadListRangeDownloadAndExplicitUpdate(t *testing.T) {
	executor := &fakeExecutor{}
	service, err := NewService(Options{Store: newTestStore(t), Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	handler := service.Handler()
	upload := httptest.NewRequest(http.MethodPost, "/api/artifacts/upload?kind=firmware&name=browser.hex", strings.NewReader(validIntelHEX))
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var uploadResult OperationResult
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploadResult); err != nil {
		t.Fatal(err)
	}
	descriptor := *uploadResult.Artifact
	if !strings.HasPrefix(descriptor.DownloadURL, "/api/artifacts/") {
		t.Fatalf("non-canonical download URL %q", descriptor.DownloadURL)
	}
	download := httptest.NewRequest(http.MethodGet, descriptor.DownloadURL, nil)
	download.Header.Set("Range", "bytes=0-9")
	downloadResponse := httptest.NewRecorder()
	handler.ServeHTTP(downloadResponse, download)
	if downloadResponse.Code != http.StatusPartialContent || downloadResponse.Header().Get("X-Checksum-SHA256") != descriptor.SHA256 {
		t.Fatalf("download status=%d headers=%v", downloadResponse.Code, downloadResponse.Header())
	}
	body, _ := json.Marshal(UpdateRequest{ArtifactSHA256: descriptor.SHA256, Authorized: true})
	update := httptest.NewRequest(http.MethodPost, "/api/updates/firmware", bytes.NewReader(body))
	update.Header.Set("Idempotency-Key", "browser-deploy-1")
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusAccepted {
		t.Fatalf("update status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	var result OperationResult
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if status := waitOperation(t, service, result.Operation.ID); status.State != "completed" {
		t.Fatalf("status=%#v", status)
	}
	journalData, err := os.ReadFile(service.operationJournalPath(result.Operation.ID))
	if err != nil {
		t.Fatalf("completed operation journal is not durable: %v", err)
	}
	var journal operationJournal
	if err := json.Unmarshal(journalData, &journal); err != nil {
		t.Fatalf("decode completed operation journal: %v", err)
	}
	if journal.Status.State != "completed" {
		t.Fatalf("completed operation was exposed before its journal: %#v", journal.Status)
	}
	repeat := httptest.NewRequest(http.MethodPost, "/api/updates/firmware", bytes.NewReader(body))
	repeat.Header.Set("Idempotency-Key", "browser-deploy-1")
	repeatResponse := httptest.NewRecorder()
	handler.ServeHTTP(repeatResponse, repeat)
	var repeated OperationResult
	if err := json.Unmarshal(repeatResponse.Body.Bytes(), &repeated); err != nil {
		t.Fatal(err)
	}
	if repeatResponse.Code != http.StatusAccepted || !repeated.Reused || repeated.Operation.ID != result.Operation.ID {
		t.Fatalf("repeat status=%d result=%#v", repeatResponse.Code, repeated)
	}
}

func TestHTTPCurrentFlashRequiresVerifiedCapture(t *testing.T) {
	service, _ := NewService(Options{Store: newTestStore(t)})
	defer service.Close()
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/artifacts/current/flash", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "capture") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHTTPFlashRestoreUsesDedicatedRoute(t *testing.T) {
	store := newTestStore(t)
	readback, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFlashBackup, Name: "captured.hex", Source: "device-readback",
		VerifiedReadback: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{}
	service, err := NewService(Options{Store: store, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	body, _ := json.Marshal(UpdateRequest{ArtifactSHA256: readback.SHA256, Authorized: true})
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/restores/flash", bytes.NewReader(body)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("restore status=%d body=%s", response.Code, response.Body.String())
	}
	var result OperationResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Operation.Kind != "flash-restore" {
		t.Fatalf("operation=%#v", result.Operation)
	}
	if status := waitOperation(t, service, result.Operation.ID); status.State != "completed" {
		t.Fatalf("status=%#v", status)
	}
}
