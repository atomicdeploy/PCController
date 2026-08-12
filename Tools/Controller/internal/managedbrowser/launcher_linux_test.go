//go:build linux

package managedbrowser

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChromeArgumentsContainNoTargetOrCredential(t *testing.T) {
	const secret = "configured-vault-token-must-not-leak"
	arguments := chromeArguments("/home/operator/.local/share/pccontroller/chrome-profile")
	joined := strings.Join(arguments, "\x00")
	for _, forbidden := range []string{secret, "127.0.0.1:8787", "Authorization", "token="} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("Chrome argv exposed %q: %q", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "--remote-debugging-pipe") || strings.Contains(joined, "--remote-debugging-port") {
		t.Fatalf("Chrome did not use an inherited-only debugging channel: %q", joined)
	}
	if !strings.Contains(joined, "--app="+initialBrowserTarget) {
		t.Fatalf("Chrome did not start on the inert bootstrap target: %q", joined)
	}
	for _, required := range []string{"--disable-extensions", "--disable-background-networking", "--disable-sync"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Chrome launch omitted %q: %q", required, joined)
		}
	}
}

func TestInertBootstrapIsDarkAndContainsNoVisibleCopy(t *testing.T) {
	encoded, found := strings.CutPrefix(initialBrowserTarget, "data:text/html,")
	if !found {
		t.Fatalf("unexpected bootstrap target=%q", initialBrowserTarget)
	}
	decoded, err := url.PathUnescape(encoded)
	if err != nil || !strings.Contains(decoded, "background:#070b14") || strings.Contains(decoded, "<body") {
		t.Fatalf("bootstrap document=%q err=%v", decoded, err)
	}
	if !strings.Contains(legacySessionTokenCleanupScript, "removeItem('pccontroller.session-token')") {
		t.Fatalf("legacy storage cleanup=%q", legacySessionTokenCleanupScript)
	}
}

func TestChromeFinalEnvironmentExcludesResolvedSecret(t *testing.T) {
	const secret = "direct-managed-browser-secret"
	filtered := browserEnvironmentWithoutSecret([]string{
		"DISPLAY=:0",
		"PCC_TOKEN=" + secret,
		"WRAPPED=prefix-" + secret + "-suffix",
	}, secret)
	joined := strings.Join(filtered, "\n")
	if strings.Contains(joined, secret) || !strings.Contains(joined, "DISPLAY=:0") {
		t.Fatalf("final Chrome environment=%q", joined)
	}
}

func TestStartScrubsResolvedSecretFromFinalChromeEnvironment(t *testing.T) {
	const secret = "direct-start-managed-browser-secret"
	directory := t.TempDir()
	capture := filepath.Join(directory, "environment.txt")
	executable := filepath.Join(directory, "fake-chrome")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n/usr/bin/env > \"$PCCONTROLLER_TEST_ENV_CAPTURE\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PCCONTROLLER_TEST_ENV_CAPTURE", capture)
	t.Setenv("PCCONTROLLER_TEST_SECRET", "prefix-"+secret+"-suffix")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if session, err := Start(ctx, Options{
		Executable: executable,
		URL:        "http://127.0.0.1:8787/",
		ProfileDir: filepath.Join(directory, "profile"),
		Token:      secret,
	}); err == nil {
		_ = session.Close(context.Background())
		t.Fatal("fake Chrome unexpectedly completed the DevTools bootstrap")
	}
	environment, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(environment), secret) {
		t.Fatalf("managedbrowser.Start leaked its resolved token to final Chrome environment: %s", environment)
	}
}

func TestBootstrapClearsLegacyStorageBeforeNavigation(t *testing.T) {
	toServerRead, toServerWrite := io.Pipe()
	fromServerRead, fromServerWrite := io.Pipe()
	client := newProtocolClient(toServerWrite, fromServerRead)
	sessionContext, cancelSession := context.WithCancel(context.Background())
	session := &Session{
		client: client,
		events: make(chan error, 1),
		ready:  make(chan struct{}),
		ctx:    sessionContext,
		cancel: cancelSession,
	}
	type result struct {
		methods []string
		err     error
	}
	served := make(chan result, 1)
	go func() {
		methods, err := serveBootstrapProtocol(toServerRead, fromServerWrite)
		served <- result{methods: methods, err: err}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := session.bootstrap(
		ctx,
		"http://127.0.0.1:8787/#/dashboard",
		"http://127.0.0.1:8787",
		"bootstrap-test-vault-token",
	)
	cancelSession()
	client.close()
	_ = toServerRead.Close()
	_ = fromServerWrite.Close()
	if err != nil {
		t.Fatal(err)
	}
	transcript := <-served
	if transcript.err != nil {
		t.Fatal(transcript.err)
	}
	cleanup := methodPosition(transcript.methods, "Page.addScriptToEvaluateOnNewDocument")
	navigate := methodPosition(transcript.methods, "Page.navigate")
	if cleanup < 0 || navigate < 0 || cleanup >= navigate {
		t.Fatalf("legacy cleanup was not installed before navigation: %v", transcript.methods)
	}
}

func serveBootstrapProtocol(input io.Reader, output io.Writer) ([]string, error) {
	const (
		sessionID    = "managed-session"
		targetOrigin = "http://127.0.0.1:8787"
	)
	reader := bufio.NewReader(input)
	var methods []string
	for {
		payload, err := readASCIIZ(reader, maxProtocolMessageBytes)
		if err != nil {
			return methods, err
		}
		var request protocolMessage
		if err := json.Unmarshal(payload, &request); err != nil {
			return methods, err
		}
		methods = append(methods, request.Method)
		result := map[string]any{}
		switch request.Method {
		case "Target.getTargets":
			result["targetInfos"] = []map[string]string{{
				"targetId": "managed-target", "type": "page", "url": initialBrowserTarget,
			}}
		case "Target.attachToTarget":
			result["sessionId"] = sessionID
		case "Page.addScriptToEvaluateOnNewDocument":
			var params struct {
				Source string `json:"source"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil || params.Source != legacySessionTokenCleanupScript {
				return methods, fmt.Errorf("legacy cleanup source=%q err=%v", params.Source, err)
			}
		case "Fetch.continueRequest":
			var params struct {
				Headers []fetchHeader `json:"headers"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				return methods, err
			}
			authorized := false
			for _, header := range params.Headers {
				if header.Name == "Authorization" && header.Value == "Bearer bootstrap-test-vault-token" {
					authorized = true
				}
			}
			if !authorized {
				return methods, fmt.Errorf("continued request omitted mediated authorization: %+v", params.Headers)
			}
			if err := writeProtocolValue(output, map[string]any{"id": request.ID, "result": result}); err != nil {
				return methods, err
			}
			if err := writeProtocolValue(output, map[string]any{
				"method": "Network.responseReceived", "sessionId": sessionID,
				"params": map[string]any{
					"requestId": "network-1",
					"response":  map[string]any{"url": targetOrigin + "/api/snapshot", "status": 200},
				},
			}); err != nil {
				return methods, err
			}
			return methods, nil
		}
		if err := writeProtocolValue(output, map[string]any{"id": request.ID, "result": result}); err != nil {
			return methods, err
		}
		if request.Method == "Target.activateTarget" {
			for _, event := range []map[string]any{
				{
					"method": "Page.frameNavigated", "sessionId": sessionID,
					"params": map[string]any{"frame": map[string]string{"id": "main", "url": targetOrigin + "/#/dashboard"}},
				},
				{
					"method": "Fetch.requestPaused", "sessionId": sessionID,
					"params": map[string]any{
						"requestId": "paused-1", "networkId": "network-1", "frameId": "main",
						"request": map[string]any{
							"url": targetOrigin + "/api/snapshot", "headers": map[string]string{"Accept": "application/json"},
						},
					},
				},
			} {
				if err := writeProtocolValue(output, event); err != nil {
					return methods, err
				}
			}
		}
	}
}

func writeProtocolValue(output io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = output.Write(append(payload, 0))
	return err
}

func methodPosition(methods []string, target string) int {
	for index, method := range methods {
		if method == target {
			return index
		}
	}
	return -1
}
