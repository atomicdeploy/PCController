package localapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"pccontroller.local/desktop/assets"
	"pccontroller.local/desktop/internal/config"
)

func TestUIAudioAssetIsSessionProtectedAndExact(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: source.Config()}
	wave, err := assets.Files.ReadFile("sfx/message.wav")
	if err != nil {
		t.Fatal(err)
	}
	running := startBrowserAPI(t, source, handler.Handle, func(options *Options) {
		options.AudioAsset = func(cue string) (AudioAsset, error) {
			if cue != "message" {
				t.Fatalf("cue = %q", cue)
			}
			return AudioAsset{
				Data:   append([]byte(nil), wave...),
				Name:   "Shared message.wav",
				Source: config.SoundSourceBundled,
			}, nil
		}
	})

	unauthorized, err := running.client.Get(running.origin + "/ui/audio/message")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}

	cookie, _ := mintBrowserSession(t, running)
	request, err := http.NewRequest(
		http.MethodGet,
		running.origin+"/ui/audio/message",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Origin", running.origin)
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != "audio/wav" ||
		!bytes.Equal(data, wave) {
		t.Fatalf(
			"asset response = %d %q, exact=%v",
			response.StatusCode,
			response.Header.Get("Content-Type"),
			bytes.Equal(data, wave),
		)
	}
	tag := response.Header.Get("ETag")
	if tag == "" {
		t.Fatal("audio ETag is missing")
	}

	cached, err := http.NewRequest(
		http.MethodGet,
		running.origin+"/ui/audio/message",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	cached.AddCookie(cookie)
	cached.Header.Set("Origin", running.origin)
	cached.Header.Set("If-None-Match", tag)
	cachedResponse, err := running.client.Do(cached)
	if err != nil {
		t.Fatal(err)
	}
	cachedResponse.Body.Close()
	if cachedResponse.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional status = %d", cachedResponse.StatusCode)
	}
}

func TestUIAudioImportStoresOncePatchesConfigAndRedactsSecrets(t *testing.T) {
	source := newTestEventSource()
	const assetID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	updated := source.Config()
	updated.Integration.LocalAPIToken = "audio-import-must-not-expose-this"
	updated.Notifications.Sound = true
	updated.Notifications.SoundSource = config.SoundSourceCustom
	updated.Notifications.SoundFile = assetID
	updated.Notifications.SoundName = "My cue.wav"
	handler := &recordedHandler{result: updated}

	var importMu sync.Mutex
	var importedName string
	var importedData []byte
	running := startBrowserAPI(t, source, handler.Handle, func(options *Options) {
		options.AudioImport = func(name string, data []byte) (ImportedAudio, error) {
			importMu.Lock()
			importedName = name
			importedData = append([]byte(nil), data...)
			importMu.Unlock()
			return ImportedAudio{ID: assetID, Name: "My cue.wav"}, nil
		}
	})
	cookie, csrf := mintBrowserSession(t, running)
	wave, err := assets.Files.ReadFile("sfx/on.wav")
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(
		http.MethodPost,
		running.origin+"/ui/audio/import",
		bytes.NewReader(wave),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Origin", running.origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(csrfHeader, csrf)
	request.Header.Set("Content-Type", "audio/wav")
	request.Header.Set(audioFilenameHeader, `C:\fakepath\My cue.wav`)
	request.Header.Set("If-Match", `"1"`)
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", response.StatusCode, body)
	}
	if strings.Contains(string(body), updated.Integration.LocalAPIToken) ||
		strings.Contains(string(body), `"local_api_token":`) {
		t.Fatalf("audio import leaked native config: %s", body)
	}
	if !strings.Contains(string(body), `"sound_source":"custom"`) ||
		!strings.Contains(string(body), `"revision":2`) {
		t.Fatalf("audio import omitted public config/revision: %s", body)
	}

	importMu.Lock()
	gotName := importedName
	gotData := append([]byte(nil), importedData...)
	importMu.Unlock()
	if gotName != `C:\fakepath\My cue.wav` || !bytes.Equal(gotData, wave) {
		t.Fatalf("import callback = %q, exact=%v", gotName, bytes.Equal(gotData, wave))
	}
	recorded := handler.last(t)
	if recorded.Method != "config.patch" {
		t.Fatalf("command = %q", recorded.Method)
	}
	var patch struct {
		Notifications config.NotificationsConfig `json:"notifications"`
	}
	if err := json.Unmarshal(recorded.Params, &patch); err != nil {
		t.Fatal(err)
	}
	if !patch.Notifications.Sound ||
		patch.Notifications.SoundSource != config.SoundSourceCustom ||
		patch.Notifications.SoundFile != assetID ||
		patch.Notifications.SoundName != "My cue.wav" {
		t.Fatalf("config patch = %+v", patch.Notifications)
	}
}

func TestUIAudioImportRejectsNonWaveMediaTypeBeforeCallback(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: source.Config()}
	called := false
	running := startBrowserAPI(t, source, handler.Handle, func(options *Options) {
		options.AudioImport = func(string, []byte) (ImportedAudio, error) {
			called = true
			return ImportedAudio{}, nil
		}
	})
	cookie, csrf := mintBrowserSession(t, running)
	request, err := http.NewRequest(
		http.MethodPost,
		running.origin+"/ui/audio/import",
		strings.NewReader("not audio"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Origin", running.origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(csrfHeader, csrf)
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnsupportedMediaType || called {
		t.Fatalf("status = %d, callback=%v", response.StatusCode, called)
	}
}

func TestUIAudioImportRequiresCurrentConfigurationRevisionBeforeImport(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: source.Config()}
	var calls int
	running := startBrowserAPI(t, source, handler.Handle, func(options *Options) {
		options.AudioImport = func(string, []byte) (ImportedAudio, error) {
			calls++
			return ImportedAudio{
				ID:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Name: "cue.wav",
			}, nil
		}
	})
	cookie, csrf := mintBrowserSession(t, running)
	wave, err := assets.Files.ReadFile("sfx/on.wav")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		ifMatch  string
		wantCode int
	}{
		{
			name:     "missing",
			wantCode: http.StatusPreconditionRequired,
		},
		{
			name:     "stale",
			ifMatch:  `"99"`,
			wantCode: http.StatusPreconditionFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, requestErr := http.NewRequest(
				http.MethodPost,
				running.origin+"/ui/audio/import",
				bytes.NewReader(wave),
			)
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			request.AddCookie(cookie)
			request.Header.Set("Origin", running.origin)
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			request.Header.Set(csrfHeader, csrf)
			request.Header.Set("Content-Type", "audio/wav")
			if test.ifMatch != "" {
				request.Header.Set("If-Match", test.ifMatch)
			}
			response, requestErr := running.client.Do(request)
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if response.StatusCode != test.wantCode {
				t.Fatalf(
					"status = %d, want %d; body=%s",
					response.StatusCode,
					test.wantCode,
					body,
				)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("audio importer called %d times for stale requests", calls)
	}
}
