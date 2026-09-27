package ipcjson

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
)

func TestPeerHostUpdateTransfersVerifiedArtifactThenQueuesRemoteCoordinator(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := store.PutFile(path, artifacts.PutOptions{
		Kind: artifacts.KindHostExecutable, Name: filepath.Base(path),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactService, err := artifacts.NewService(artifacts.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer artifactService.Close()
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	var transferID = "transfer-test"
	const idempotencyKey = "intent:test-success"
	var received int64
	updateCalled := false
	updateIdempotencyKey := ""
	service := &Service{Client: client, Artifacts: artifactService}
	service.BridgeCall = func(_ context.Context, peer string, request Request) (Response, error) {
		if peer != "edge" {
			t.Fatalf("peer=%q", peer)
		}
		response := Response{JSONRPC: Version, ID: request.ID}
		switch request.Method {
		case "controller.artifact.upload.begin":
			var value artifacts.PeerUploadBeginRequest
			if err := json.Unmarshal(request.Params, &value); err != nil {
				t.Fatal(err)
			}
			if value.SHA256 != descriptor.SHA256 || value.Bytes != descriptor.Bytes || value.Platform != descriptor.Platform {
				t.Fatalf("begin=%#v descriptor=%#v", value, descriptor)
			}
			response.Result = artifacts.PeerUploadBeginResult{TransferID: transferID, ChunkBytes: 32 << 10}
		case "controller.artifact.upload.chunk":
			var value artifacts.PeerUploadChunkRequest
			if err := json.Unmarshal(request.Params, &value); err != nil {
				t.Fatal(err)
			}
			if value.TransferID != transferID || value.Offset != received || len(value.Data) == 0 {
				t.Fatalf("chunk transfer=%q offset=%d received=%d bytes=%d", value.TransferID, value.Offset, received, len(value.Data))
			}
			received += int64(len(value.Data))
			response.Result = artifacts.PeerUploadChunkResult{TransferID: transferID, NextOffset: received, BytesTotal: descriptor.Bytes}
		case "controller.artifact.upload.finish":
			if received != descriptor.Bytes {
				t.Fatalf("finish after %d/%d bytes", received, descriptor.Bytes)
			}
			response.Result = artifacts.OperationResult{
				Operation: artifacts.UpdateStatus{
					ID: "remote-upload", Kind: "artifact-upload", State: "completed",
					ProgressPercent: 100, ArtifactSHA256: descriptor.SHA256,
					BytesDone: descriptor.Bytes, BytesTotal: descriptor.Bytes,
				},
				Artifact: &descriptor,
			}
		case "controller.update.host":
			var value artifacts.UpdateRequest
			if err := json.Unmarshal(request.Params, &value); err != nil {
				t.Fatal(err)
			}
			if !value.Authorized || value.ArtifactSHA256 != descriptor.SHA256 {
				t.Fatalf("update=%#v", value)
			}
			updateCalled = true
			updateIdempotencyKey = value.IdempotencyKey
			response.Result = artifacts.OperationResult{Operation: artifacts.UpdateStatus{
				ID: "remote-update", Kind: "host", State: "queued",
				ArtifactSHA256: descriptor.SHA256, IdempotencyKey: idempotencyKey,
			}}
		case "controller.artifact.upload.abort":
			t.Fatal("successful transfer was aborted")
		default:
			t.Fatalf("method=%q", request.Method)
		}
		return response, nil
	}
	result, err := service.updatePeerHost(context.Background(), peerHostUpdateRequest{
		Peer: "edge", ArtifactSHA256: descriptor.SHA256, Authorized: true,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedKey, err := peerHostIdempotencyKey(idempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if !updateCalled || updateIdempotencyKey != expectedKey || result.Peer != "edge" ||
		result.Artifact.SHA256 != descriptor.SHA256 || result.Operation.ID != "remote-update" ||
		result.Stage != "remote-queued" || result.TerminalVerified {
		t.Fatalf("result=%#v updateCalled=%t", result, updateCalled)
	}
}

func TestPeerHostUpdateRetainsIntentWhenTargetAcceptsThenBridgeCloses(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := store.PutFile(path, artifacts.PutOptions{
		Kind: artifacts.KindHostExecutable, Name: filepath.Base(path),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactService, err := artifacts.NewService(artifacts.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer artifactService.Close()
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	const idempotencyKey = "intent:accept-then-disconnect"
	var received int64
	acceptedKeys := make([]string, 0, 2)
	service := &Service{Client: client, Artifacts: artifactService}
	service.BridgeCall = func(_ context.Context, _ string, request Request) (Response, error) {
		response := Response{JSONRPC: Version, ID: request.ID}
		switch request.Method {
		case "controller.artifact.upload.begin":
			received = 0
			response.Result = artifacts.PeerUploadBeginResult{
				TransferID: "transfer-retry", ChunkBytes: artifacts.PeerUploadChunkBytes,
			}
		case "controller.artifact.upload.chunk":
			var value artifacts.PeerUploadChunkRequest
			if err := json.Unmarshal(request.Params, &value); err != nil {
				t.Fatal(err)
			}
			received += int64(len(value.Data))
			response.Result = artifacts.PeerUploadChunkResult{
				TransferID: value.TransferID, NextOffset: received, BytesTotal: descriptor.Bytes,
			}
		case "controller.artifact.upload.finish":
			response.Result = artifacts.OperationResult{
				Operation: artifacts.UpdateStatus{
					ID: "remote-upload", Kind: "artifact-upload", State: "completed",
					ProgressPercent: 100, ArtifactSHA256: descriptor.SHA256,
					BytesDone: descriptor.Bytes, BytesTotal: descriptor.Bytes,
				},
				Artifact: &descriptor,
			}
		case "controller.update.host":
			var value artifacts.UpdateRequest
			if err := json.Unmarshal(request.Params, &value); err != nil {
				t.Fatal(err)
			}
			acceptedKeys = append(acceptedKeys, value.IdempotencyKey)
			if len(acceptedKeys) == 1 {
				// The target has accepted this key, but its acknowledgement never
				// reaches the source host.
				return Response{}, errors.New("bridge session closed before response")
			}
			response.Result = artifacts.OperationResult{Operation: artifacts.UpdateStatus{
				ID: "remote-update", Kind: "host", State: "queued",
				ArtifactSHA256: descriptor.SHA256, IdempotencyKey: value.IdempotencyKey,
			}}
		default:
			t.Fatalf("unexpected method=%q", request.Method)
		}
		return response, nil
	}
	request := peerHostUpdateRequest{
		Peer: "edge", ArtifactSHA256: descriptor.SHA256, Authorized: true,
		IdempotencyKey: idempotencyKey,
	}
	eventCursor := client.LatestEventID()
	if _, err := service.updatePeerHost(context.Background(), request); err == nil {
		t.Fatal("accept-then-disconnect unexpectedly succeeded")
	} else {
		var rpcError *RPCError
		if !errors.As(err, &rpcError) || rpcError.Code != rpcErrorOutcomeUncertain {
			t.Fatalf("uncertain outcome err=%v rpc=%#v", err, rpcError)
		}
		if !strings.Contains(rpcError.Message, "retry with the same idempotency key") {
			t.Fatalf("uncertain outcome is not actionable: %q", rpcError.Message)
		}
	}
	eventContext, cancelEvent := context.WithTimeout(context.Background(), time.Second)
	defer cancelEvent()
	event, err := client.NextEvent(eventContext, eventCursor, "peer-update.outcome-uncertain")
	if err != nil {
		t.Fatalf("wait for shared uncertain-outcome event: %v", err)
	}
	if event.Metadata["retry_same_idempotency_key"] != "true" ||
		event.Metadata["terminal_verified"] != "false" ||
		event.Metadata["idempotency_key"] != idempotencyKey ||
		event.Metadata["operation_id"] == "" ||
		!strings.Contains(event.Text, "retry with the same idempotency key") {
		t.Fatalf("uncertain event=%#v", event)
	}
	result, err := service.updatePeerHost(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(acceptedKeys) != 2 || acceptedKeys[0] != idempotencyKey ||
		acceptedKeys[1] != acceptedKeys[0] || result.Operation.ID != "remote-update" {
		t.Fatalf("accepted keys=%v result=%#v", acceptedKeys, result)
	}
}

func TestPeerHostUpdateRequiresExplicitAuthorization(t *testing.T) {
	_, err := (&Service{}).updatePeerHost(context.Background(), peerHostUpdateRequest{Peer: "edge"})
	if err == nil {
		t.Fatal("unauthorized peer host update accepted")
	}
}

func TestPeerHostUpdateRejectsMismatchedChunkAcknowledgementsAndAborts(t *testing.T) {
	for name, mutate := range map[string]func(*artifacts.PeerUploadChunkResult){
		"transfer identity": func(result *artifacts.PeerUploadChunkResult) { result.TransferID = "different" },
		"next offset":       func(result *artifacts.PeerUploadChunkResult) { result.NextOffset++ },
		"declared total":    func(result *artifacts.PeerUploadChunkResult) { result.BytesTotal++ },
	} {
		t.Run(name, func(t *testing.T) {
			path, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"))
			if err != nil {
				t.Fatal(err)
			}
			descriptor, err := store.PutFile(path, artifacts.PutOptions{
				Kind: artifacts.KindHostExecutable, Name: filepath.Base(path),
			})
			if err != nil {
				t.Fatal(err)
			}
			artifactService, err := artifacts.NewService(artifacts.Options{Store: store})
			if err != nil {
				t.Fatal(err)
			}
			defer artifactService.Close()
			runtime := control.New(control.Options{})
			defer runtime.Close()
			client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
			aborted := false
			service := &Service{Client: client, Artifacts: artifactService}
			service.BridgeCall = func(_ context.Context, _ string, request Request) (Response, error) {
				response := Response{JSONRPC: Version, ID: request.ID}
				switch request.Method {
				case "controller.artifact.upload.begin":
					response.Result = artifacts.PeerUploadBeginResult{
						TransferID: "transfer-test", ChunkBytes: artifacts.PeerUploadChunkBytes,
					}
				case "controller.artifact.upload.chunk":
					var value artifacts.PeerUploadChunkRequest
					if err := json.Unmarshal(request.Params, &value); err != nil {
						t.Fatal(err)
					}
					result := artifacts.PeerUploadChunkResult{
						TransferID: value.TransferID,
						NextOffset: value.Offset + int64(len(value.Data)),
						BytesTotal: descriptor.Bytes,
					}
					mutate(&result)
					response.Result = result
				case "controller.artifact.upload.abort":
					aborted = true
				default:
					t.Fatalf("unexpected method=%q", request.Method)
				}
				return response, nil
			}
			_, err = service.updatePeerHost(context.Background(), peerHostUpdateRequest{
				Peer: "edge", ArtifactSHA256: descriptor.SHA256, Authorized: true,
				IdempotencyKey: "intent:ack-mismatch",
			})
			if err == nil || !strings.Contains(err.Error(), "acknowledgement mismatch") {
				t.Fatalf("mismatched acknowledgement err=%v", err)
			}
			if !aborted {
				t.Fatal("mismatched transfer acknowledgement did not abort the peer upload")
			}
		})
	}
}

func TestPeerHostUpdateIdempotencyKeyIsRetryStable(t *testing.T) {
	if _, err := peerHostIdempotencyKey(""); err == nil || !strings.Contains(err.Error(), "caller-generated") {
		t.Fatalf("missing key err=%v", err)
	}
	if explicit, err := peerHostIdempotencyKey("operator:retry-1"); err != nil || explicit != "operator:retry-1" {
		t.Fatalf("explicit key=%q err=%v", explicit, err)
	}
	if _, err := peerHostIdempotencyKey("contains spaces"); err == nil {
		t.Fatal("invalid explicit idempotency key was accepted")
	}
}

func TestPeerUploadFinishRequiresExactDescriptorAndOperationIdentity(t *testing.T) {
	descriptor := artifacts.Descriptor{
		Kind: artifacts.KindHostExecutable, SHA256: strings.Repeat("a", 64),
		Bytes: 4096, Platform: "windows/amd64",
	}
	valid := func() artifacts.OperationResult {
		artifact := descriptor
		return artifacts.OperationResult{
			Artifact: &artifact,
			Operation: artifacts.UpdateStatus{
				ID: "upload-1", Kind: "artifact-upload", State: "completed",
				ProgressPercent: 100, ArtifactSHA256: descriptor.SHA256,
				BytesDone: descriptor.Bytes, BytesTotal: descriptor.Bytes,
			},
		}
	}
	if err := validatePeerUploadResult(valid(), descriptor); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*artifacts.OperationResult){
		"artifact digest": func(value *artifacts.OperationResult) { value.Artifact.SHA256 = strings.Repeat("b", 64) },
		"artifact kind":   func(value *artifacts.OperationResult) { value.Artifact.Kind = artifacts.KindFirmware },
		"artifact bytes":  func(value *artifacts.OperationResult) { value.Artifact.Bytes++ },
		"platform":        func(value *artifacts.OperationResult) { value.Artifact.Platform = "linux/amd64" },
		"operation ID":    func(value *artifacts.OperationResult) { value.Operation.ID = "" },
		"operation kind":  func(value *artifacts.OperationResult) { value.Operation.Kind = "host" },
		"operation state": func(value *artifacts.OperationResult) { value.Operation.State = "queued" },
		"progress":        func(value *artifacts.OperationResult) { value.Operation.ProgressPercent = 99 },
		"operation digest": func(value *artifacts.OperationResult) {
			value.Operation.ArtifactSHA256 = strings.Repeat("b", 64)
		},
		"bytes done":  func(value *artifacts.OperationResult) { value.Operation.BytesDone-- },
		"bytes total": func(value *artifacts.OperationResult) { value.Operation.BytesTotal++ },
	} {
		t.Run(name, func(t *testing.T) {
			result := valid()
			mutate(&result)
			if err := validatePeerUploadResult(result, descriptor); err == nil {
				t.Fatalf("malformed finish result accepted: %#v", result)
			}
		})
	}
}

func TestPeerHostAcceptanceRequiresExactRetryAndStagingIdentity(t *testing.T) {
	digest := strings.Repeat("a", 64)
	const key = "intent:remote-stage"
	valid := artifacts.UpdateStatus{
		ID: "host-1", Kind: "host", State: "queued",
		ArtifactSHA256: digest, IdempotencyKey: key,
	}
	if stage, err := peerHostAcceptance(valid, digest, key); err != nil || stage != "remote-queued" {
		t.Fatalf("valid stage=%q err=%v", stage, err)
	}
	for name, mutate := range map[string]func(*artifacts.UpdateStatus){
		"operation ID":      func(value *artifacts.UpdateStatus) { value.ID = "" },
		"kind":              func(value *artifacts.UpdateStatus) { value.Kind = "firmware" },
		"digest":            func(value *artifacts.UpdateStatus) { value.ArtifactSHA256 = strings.Repeat("b", 64) },
		"idempotency key":   func(value *artifacts.UpdateStatus) { value.IdempotencyKey = "intent:different" },
		"unsupported state": func(value *artifacts.UpdateStatus) { value.State = "running" },
		"failed state":      func(value *artifacts.UpdateStatus) { value.State = "failed" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if _, err := peerHostAcceptance(candidate, digest, key); err == nil {
				t.Fatalf("malformed target operation accepted: %#v", candidate)
			}
		})
	}
	staged := valid
	staged.State = "completed"
	if stage, err := peerHostAcceptance(staged, digest, key); err != nil || stage != "remote-staged" {
		t.Fatalf("completed staging stage=%q err=%v", stage, err)
	}
}

func TestPeerHostEventOperationIdentityIncludesLogicalIntent(t *testing.T) {
	digest := strings.Repeat("a", 64)
	first := peerHostOperationID("edge", digest, "intent:first")
	if first != peerHostOperationID("edge", digest, "intent:first") {
		t.Fatal("same logical intent produced a different source operation ID")
	}
	if first == peerHostOperationID("edge", digest, "intent:second") {
		t.Fatal("distinct logical intents collided in source operation ID")
	}
}

func TestRemotePeerHostUpdateRequiresProgrammingAndBridgeCapabilitiesAndRejectsChaining(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	service := &Service{Client: client, HostConfig: func() appconfig.Config { return config }}
	params, _ := json.Marshal(peerHostUpdateRequest{
		Peer: "edge", ArtifactSHA256: strings.Repeat("a", 64), Authorized: true,
		IdempotencyKey: "intent:remote-policy",
	})
	request := Request{JSONRPC: Version, Method: "controller.peer.update.host", Params: params}

	config.IPC.RemotePolicy.Programming = true
	response := service.DispatchRemote(context.Background(), request, "websocket")
	if response.Error == nil || response.Error.Code != -32003 ||
		!strings.Contains(response.Error.Message, capabilityBridgeCalls) {
		t.Fatalf("programming-only response=%#v", response)
	}
	config.IPC.RemotePolicy.Programming = false
	config.IPC.RemotePolicy.BridgeCalls = true
	response = service.DispatchRemote(context.Background(), request, "websocket")
	if response.Error == nil || response.Error.Code != -32003 ||
		!strings.Contains(response.Error.Message, capabilityProgramming) {
		t.Fatalf("bridge-only response=%#v", response)
	}
	config.IPC.RemotePolicy.Programming = true
	response = service.DispatchRemote(context.Background(), request, "websocket")
	if response.Error == nil || response.Error.Code == -32003 ||
		!strings.Contains(response.Error.Message, "artifact service") {
		t.Fatalf("both-capabilities response=%#v", response)
	}
	response = service.DispatchRemote(context.Background(), request, "bridge")
	if response.Error == nil || response.Error.Code != -32003 ||
		!strings.Contains(response.Error.Message, "may not be chained") {
		t.Fatalf("bridge-chained response=%#v", response)
	}
}

func TestRemotePeerHostUpdateShellWrappersCannotBypassDualCapabilityOrBridgeBan(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	service := &Service{Client: client, HostConfig: func() appconfig.Config { return config }}
	commandParams, _ := json.Marshal(map[string]string{
		"command": "peer-update host edge " + strings.Repeat("a", 64) + " intent:wrapped",
	})
	actionParams, _ := json.Marshal(hostui.AppAction{
		Kind: "command", Value: "peer-update host edge " + strings.Repeat("a", 64) + " intent:wrapped",
	})
	requests := []Request{
		{JSONRPC: Version, Method: "controller.command.execute", Params: commandParams},
		{JSONRPC: Version, Method: "controller.app.action", Params: actionParams},
	}
	for _, request := range requests {
		t.Run(request.Method, func(t *testing.T) {
			config.IPC.RemotePolicy.Programming = true
			config.IPC.RemotePolicy.BridgeCalls = false
			response := service.DispatchRemote(context.Background(), request, "websocket")
			if response.Error == nil || response.Error.Code != -32003 ||
				!strings.Contains(response.Error.Message, capabilityBridgeCalls) {
				t.Fatalf("programming-only wrapper response=%#v", response)
			}
			config.IPC.RemotePolicy.Programming = false
			config.IPC.RemotePolicy.BridgeCalls = true
			response = service.DispatchRemote(context.Background(), request, "websocket")
			if response.Error == nil || response.Error.Code != -32003 ||
				!strings.Contains(response.Error.Message, capabilityProgramming) {
				t.Fatalf("bridge-only wrapper response=%#v", response)
			}
			config.IPC.RemotePolicy.Programming = true
			response = service.DispatchRemote(context.Background(), request, "websocket")
			if response.Error == nil || response.Error.Code == -32003 {
				t.Fatalf("dual-capability wrapper remained policy-blocked=%#v", response)
			}
			response = service.DispatchRemote(context.Background(), request, "bridge")
			if response.Error == nil || response.Error.Code != -32003 ||
				!strings.Contains(response.Error.Message, "may not be chained") {
				t.Fatalf("bridge wrapper response=%#v", response)
			}
		})
	}

	bridgeCommand, _ := json.Marshal(map[string]string{
		"command": "bridge call edge controller.peer.update.host {}",
	})
	response := service.DispatchRemote(context.Background(), Request{
		JSONRPC: Version, Method: "controller.command.execute", Params: bridgeCommand,
	}, "websocket")
	if response.Error == nil || response.Error.Code != -32003 ||
		!strings.Contains(response.Error.Message, "may not be chained") {
		t.Fatalf("shell bridge chain response=%#v", response)
	}
}

func TestBridgeTransportRejectsGenericBridgeRecursion(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	config.IPC.RemotePolicy.BridgeCalls = true
	service := &Service{
		Client: client, HostConfig: func() appconfig.Config { return config },
		AppAction: func(hostui.AppAction) error { return nil },
	}
	direct, _ := json.Marshal(map[string]any{
		"peer":    "second",
		"request": Request{JSONRPC: Version, Method: "controller.snapshot"},
	})
	command, _ := json.Marshal(map[string]string{
		"command": "bridge call second controller.snapshot",
	})
	action, _ := json.Marshal(hostui.AppAction{
		Kind: "command", Value: "bridge call second controller.snapshot",
	})
	for _, request := range []Request{
		{JSONRPC: Version, Method: "controller.bridge.call", Params: direct},
		{JSONRPC: Version, Method: "controller.command.execute", Params: command},
		{JSONRPC: Version, Method: "controller.app.action", Params: action},
	} {
		t.Run(request.Method, func(t *testing.T) {
			response := service.DispatchRemote(context.Background(), request, "bridge")
			if response.Error == nil || response.Error.Code != -32003 ||
				!strings.Contains(response.Error.Message, "recursive bridge calls") {
				t.Fatalf("bridge recursion response=%#v", response)
			}
		})
	}
}

func TestAuthenticatedRemoteHTTPCommandSurfacesApplyPeerUpdateDualCapability(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	config.IPC.AuthToken = "0123456789abcdefghijklmn"
	config.IPC.RemotePolicy.Programming = true
	service := &Service{
		Client: client, HostConfig: func() appconfig.Config { return config },
		AppAction: func(hostui.AppAction) error { return nil },
	}
	handler := websocketMux(context.Background(), service)
	command := "peer-update host edge " + strings.Repeat("a", 64) + " intent:http"
	for _, endpoint := range []struct {
		path string
		body string
	}{
		{path: "/api/command", body: `{"command":` + strconv.Quote(command) + `}`},
		{path: "/api/app/action", body: `{"kind":"command","value":` + strconv.Quote(command) + `}`},
		{path: "/api/rpc", body: `{"jsonrpc":"2.0","id":1,"method":"controller.command.execute","params":{"command":` + strconv.Quote(command) + `}}`},
	} {
		t.Run(endpoint.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://controller.example"+endpoint.path, strings.NewReader(endpoint.body))
			request.RemoteAddr = "198.51.100.10:43100"
			request.Header.Set("Authorization", "Bearer "+config.IPC.AuthToken)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden && endpoint.path != "/api/rpc" {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), capabilityBridgeCalls) {
				t.Fatalf("body=%s", response.Body.String())
			}
		})
	}
}

func TestBridgeCallEntryPointsRejectPeerHostUpdatePivot(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	config.IPC.RemotePolicy.BridgeCalls = true
	called := false
	service := &Service{
		Client: client, HostConfig: func() appconfig.Config { return config },
		BridgeCall: func(context.Context, string, Request) (Response, error) {
			called = true
			return Response{}, nil
		},
	}
	nested, _ := json.Marshal(map[string]any{
		"peer":    "edge",
		"request": Request{JSONRPC: Version, Method: "controller.peer.update.host"},
	})
	response := service.Dispatch(context.Background(), Request{
		JSONRPC: Version, Method: "controller.bridge.call", Params: nested,
	})
	if response.Error == nil || !strings.Contains(response.Error.Message, "may not be chained") || called {
		t.Fatalf("JSON-RPC bridge pivot response=%#v called=%t", response, called)
	}
	wrappedParams, _ := json.Marshal(map[string]string{
		"command": "peer-update host second " + strings.Repeat("a", 64) + " intent:nested",
	})
	wrapped, _ := json.Marshal(map[string]any{
		"peer":    "edge",
		"request": Request{JSONRPC: Version, Method: "controller.command.execute", Params: wrappedParams},
	})
	response = service.Dispatch(context.Background(), Request{
		JSONRPC: Version, Method: "controller.bridge.call", Params: wrapped,
	})
	if response.Error == nil || !strings.Contains(response.Error.Message, "may not be chained") || called {
		t.Fatalf("wrapped JSON-RPC bridge pivot response=%#v called=%t", response, called)
	}

	handler := websocketMux(context.Background(), service)
	httpRequest := httptest.NewRequest(
		http.MethodPost, "http://controller.example/api/bridges/call",
		strings.NewReader(string(wrapped)),
	)
	httpRequest.RemoteAddr = "127.0.0.1:43100"
	httpResponse := httptest.NewRecorder()
	handler.ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusBadRequest || called ||
		!strings.Contains(httpResponse.Body.String(), "may not be chained") {
		t.Fatalf("HTTP bridge pivot status=%d body=%s called=%t", httpResponse.Code, httpResponse.Body.String(), called)
	}
}
