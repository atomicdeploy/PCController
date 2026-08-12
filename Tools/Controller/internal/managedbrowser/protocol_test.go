package managedbrowser

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatedRequestHeadersAreExactOriginAndAPIScoped(t *testing.T) {
	const (
		origin = "http://127.0.0.1:8787"
		token  = "managed-browser-vault-token-012345"
	)
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "API root", url: origin + "/api", want: true},
		{name: "API path", url: origin + "/api/session/ticket?transport=websocket", want: true},
		{name: "different port", url: "http://127.0.0.1:8788/api/snapshot"},
		{name: "different peer", url: "http://127.0.0.2:8787/api/snapshot"},
		{name: "lookalike path", url: origin + "/apix/snapshot"},
		{name: "userinfo", url: "http://operator@127.0.0.1:8787/api/snapshot"},
		{name: "non API", url: origin + "/service-worker.js"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers, ok := authenticatedRequestHeaders(true, origin, test.url, map[string]string{
				"Accept":               "application/json",
				"authorization":        "Bearer stale-browser-value",
				"X-PCController-Token": "stale-browser-value",
			}, token)
			if ok != test.want {
				t.Fatalf("authenticated=%v want=%v headers=%v", ok, test.want, headers)
			}
			if !ok {
				return
			}
			encoded, _ := json.Marshal(headers)
			text := string(encoded)
			if strings.Count(text, token) != 1 || strings.Contains(text, "stale-browser-value") {
				t.Fatalf("credential override was ambiguous: %s", text)
			}
		})
	}
}

func TestFrameAuthorityRevokesHostileAndOtherLoopbackNavigation(t *testing.T) {
	const (
		targetOrigin = "http://127.0.0.1:8787"
		targetAPI    = targetOrigin + "/api/snapshot"
	)
	authority := newFrameAuthority(targetOrigin)
	if authority.allows("main", targetAPI) {
		t.Fatal("uninitialized frame received ambient controller authority")
	}
	authority.navigate("main", targetOrigin+"/#/dashboard")
	if !authority.allows("main", targetAPI) {
		t.Fatal("exact target-origin frame did not receive controller authority")
	}
	authority.navigate("hostile", "http://127.0.0.1:9999/attack")
	if authority.allows("hostile", targetAPI) {
		t.Fatal("other-loopback-origin frame received controller authority")
	}
	authority.navigate("main", "http://127.0.0.1:9999/after-navigation")
	if authority.allows("main", targetAPI) {
		t.Fatal("authority survived a hostile cross-origin main-frame navigation")
	}
	authority.navigate("child", targetOrigin+"/embedded")
	if !authority.allows("child", targetAPI) {
		t.Fatal("initialized exact-origin child frame was rejected")
	}
	authority.detach("child")
	if authority.allows("child", targetAPI) {
		t.Fatal("detached frame retained controller authority")
	}
	if _, ok := authenticatedRequestHeaders(false, targetOrigin, targetAPI, nil, "vault-secret"); ok {
		t.Fatal("header injector ignored a denied frame binding")
	}
}

func TestCleanLoopbackURLRejectsRebindingAndCredentialShapes(t *testing.T) {
	for _, value := range []string{
		"http://server:8787/",
		"http://localhost:8787/",
		"http://127.0.0.1/",
		"http://127.0.0.1:8787/?token=forbidden",
		"http://operator@127.0.0.1:8787/",
		"https://127.0.0.1:8787/",
	} {
		if parsed, err := cleanLoopbackURL(value); err == nil {
			t.Fatalf("unsafe managed URL accepted: input=%q parsed=%s", value, parsed)
		}
	}
	parsed, err := cleanLoopbackURL("http://127.0.0.1:8787/#/dashboard")
	if err != nil || parsed.String() != "http://127.0.0.1:8787/#/dashboard" {
		t.Fatalf("clean URL parsed=%v err=%v", parsed, err)
	}
}

func TestAuthenticationProofExcludesPublicBootstrapMetadata(t *testing.T) {
	const origin = "http://127.0.0.1:8787"
	if authenticationProofRequest(origin, origin+"/api/ui-config") {
		t.Fatal("public UI configuration was accepted as authentication proof")
	}
	for _, value := range []string{origin + "/api/snapshot", origin + "/api/session/ticket"} {
		if !authenticationProofRequest(origin, value) {
			t.Fatalf("protected response %q was not accepted as authentication proof", value)
		}
	}
}

func TestProtocolErrorsDoNotEchoSensitiveRequest(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	client := newProtocolClient(inputWriter, outputReader)
	defer client.close()
	const secret = "do-not-echo-managed-browser-secret"
	go func() {
		payload, _ := readASCIIZ(bufio.NewReader(inputReader), maxProtocolMessageBytes)
		var request protocolRequest
		_ = json.Unmarshal(payload, &request)
		_, _ = io.WriteString(outputWriter, `{"id":`+jsonNumber(request.ID)+`,"error":{"code":-32000,"message":"`+secret+`"}}`+"\x00")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := client.call(ctx, "session", "Fetch.continueRequest", map[string]any{
		"headers": []fetchHeader{{Name: "Authorization", Value: "Bearer " + secret}},
	}, nil, true)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("sensitive protocol failure=%q", err)
	}
}

func TestProtocolResponsesRemainLiveAcrossEventBurst(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	client := newProtocolClient(inputWriter, outputReader)
	defer client.close()
	const eventCount = maxQueuedProtocolEvents
	go func() {
		payload, _ := readASCIIZ(bufio.NewReader(inputReader), maxProtocolMessageBytes)
		var request protocolRequest
		_ = json.Unmarshal(payload, &request)
		for index := 0; index < eventCount; index++ {
			_, _ = io.WriteString(outputWriter, `{"method":"Network.loadingFinished","params":{"sequence":`+jsonNumber(uint64(index))+`}}`+"\x00")
		}
		_, _ = io.WriteString(outputWriter, `{"id":`+jsonNumber(request.ID)+`,"result":{"ok":true}}`+"\x00")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var result struct {
		OK bool `json:"ok"`
	}
	if err := client.call(ctx, "", "Browser.getVersion", map[string]any{}, &result, false); err != nil || !result.OK {
		t.Fatalf("response starved behind event burst: result=%+v err=%v", result, err)
	}
	for index := 0; index < eventCount; index++ {
		if event, ok := client.nextEvent(ctx); !ok || event.Method != "Network.loadingFinished" {
			t.Fatalf("queued event %d missing: event=%+v ok=%v", index, event, ok)
		}
	}
}

func jsonNumber(value uint64) string {
	var output bytes.Buffer
	_ = json.NewEncoder(&output).Encode(value)
	return strings.TrimSpace(output.String())
}
