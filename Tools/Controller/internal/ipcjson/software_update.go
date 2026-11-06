package ipcjson

// Explicit, bounded software-only gateway. It does not grant general HTTP
// proxying, board commands, nested bridge calls or executable replacement here.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/hostui"
)

const softwareChunkBytes = 384 << 10

type softwareTarget struct {
	ID       string `json:"id"`
	Product  string `json:"product"`
	Revision string `json:"revision"`
	origin   string
}
type softwareCall struct {
	Target     string  `json:"target"`
	Revision   string  `json:"revision"`
	Authorized bool    `json:"authorized"`
	Request    Request `json:"request"`
}

func softwareRevision(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func instanceSoftwareTarget(instance hostui.AppInstance) (softwareTarget, bool) {
	if instance.Surface != "pealayer" || instance.Self == nil || instance.Self.Kind != "native" {
		return softwareTarget{}, false
	}
	origin := strings.TrimSpace(instance.Values["peer_origin"])
	if origin == "" {
		host, _ := os.Hostname()
		if !strings.EqualFold(instance.Values["host"], host) {
			return softwareTarget{}, false
		}
		rpc, err := url.Parse(instance.Self.Vars["rpc"])
		if err != nil || rpc.Path != "/api/rpc" {
			return softwareTarget{}, false
		}
		origin = rpc.Scheme + "://" + rpc.Host + "/"
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return softwareTarget{}, false
	}
	origin = parsed.Scheme + "://" + parsed.Host
	return softwareTarget{ID: "app:" + instance.ID, Product: "pealayer", origin: origin,
		Revision: softwareRevision(instance.ID + "\x00" + origin + "\x00" + strconv.Itoa(instance.Self.ProcessID) + "\x00" + instance.RegisteredAt.String())}, true
}

func (service *Service) softwareTargets() []softwareTarget {
	targets := []softwareTarget{}
	if service.Artifacts != nil {
		targets = append(targets, softwareTarget{ID: "self", Product: "pccontroller", Revision: softwareRevision(service.HostInstanceID + "\x00" + strconv.Itoa(service.HostProcessID))})
	}
	if service.AppInstances != nil {
		for _, instance := range service.AppInstances.List() {
			if target, ok := instanceSoftwareTarget(instance); ok {
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func validateSoftwareCall(raw json.RawMessage) (softwareCall, error) {
	var call softwareCall
	if err := decodeStrictParams(raw, &call); err != nil {
		return call, err
	}
	if !call.Authorized {
		return call, errors.New("software update forwarding requires authorized=true")
	}
	if call.Revision == "" || (call.Target != "self" && !strings.HasPrefix(call.Target, "app:")) {
		return call, errors.New("select a registered software target and its revision")
	}
	method := call.Request.Method
	if call.Request.JSONRPC != "" && call.Request.JSONRPC != Version {
		return call, errors.New("nested jsonrpc must be 2.0")
	}
	if call.Request.Auth != "" || call.Request.ClientID != "" {
		return call, errors.New("nested credentials and client actors are not forwarded")
	}
	if call.Target == "self" {
		switch method {
		case "controller.artifact.manifest", "controller.artifact.list", "controller.artifact.upload.begin", "controller.artifact.upload.chunk", "controller.artifact.upload.finish", "controller.artifact.upload.abort", "controller.artifact.fetch", "controller.update.host", "controller.update.status", "controller.artifact.download.begin", "controller.artifact.download.chunk", "controller.artifact.download.close":
		default:
			return call, errors.New("method is not a controller software update operation")
		}
		if method == "controller.artifact.upload.begin" || method == "controller.artifact.fetch" || method == "controller.artifact.list" || method == "controller.artifact.download.begin" {
			var kind struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal(call.Request.Params, &kind) != nil || kind.Kind != "host-executable" {
				return call, errors.New("software gateway accepts host-executable artifacts only")
			}
		}
	} else {
		switch method {
		case "pealayer.update.manifest", "pealayer.update.status", "pealayer.update.artifact.read", "pealayer.update.upload.begin", "pealayer.update.upload.chunk", "pealayer.update.upload.finish", "pealayer.update.upload.abort", "pealayer.update.from-url":
		case "pealayer.update.bundle.list", "pealayer.update.bundle.read":
		default:
			return call, errors.New("method is not a Pealayer software update operation")
		}
	}
	return call, nil
}

func (service *Service) dispatchSoftware(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method == "controller.software.bundle.list" || method == "controller.software.bundle.read" {
		return dispatchSoftwareBundle(method, raw)
	}
	if method == "controller.software.targets" {
		return map[string]any{"targets": service.softwareTargets()}, nil
	}
	if method != "controller.software.call" {
		return nil, errors.New("unknown software gateway method")
	}
	call, err := validateSoftwareCall(raw)
	if err != nil {
		return nil, err
	}
	var selected *softwareTarget
	for _, target := range service.softwareTargets() {
		if target.ID == call.Target && target.Revision == call.Revision {
			copy := target
			selected = &copy
			break
		}
	}
	if selected == nil {
		return nil, errors.New("software target changed or expired; refresh targets before sending")
	}
	if selected.Product == "pccontroller" {
		if service.Artifacts == nil {
			return nil, errors.New("primary artifact service unavailable")
		}
		value, handled, err := service.Artifacts.DispatchRPC(ctx, call.Request.Method, call.Request.Params)
		if !handled && err == nil {
			err = errors.New("unsupported artifact operation")
		}
		return value, err
	}
	return callPealayerUpdate(ctx, *selected, call.Request)
}

func callPealayerUpdate(ctx context.Context, target softwareTarget, rpc Request) (any, error) {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("software target redirects are not permitted")
	}}
	transport := &http.Transport{Proxy: nil}
	client.Transport = transport
	defer transport.CloseIdleConnections()
	// Verify product before starting a transfer or URL pull. Later chunks carry
	// an owner-issued operation ID and remain fenced by the registered revision.
	if rpc.Method == "pealayer.update.upload.begin" || rpc.Method == "pealayer.update.from-url" {
		manifest, err := softwareHTTP(ctx, client, target.origin, "GET", "/api/update/manifest", nil, "", false)
		if err != nil {
			return nil, err
		}
		object, _ := manifest.(map[string]any)
		if object["format"] != "pealayer-update" || object["service"] != "pealayer" {
			return nil, errors.New("selected target did not identify itself as Pealayer")
		}
	}
	verb, path, body, content := "POST", "", []byte(rpc.Params), "application/json"
	switch rpc.Method {
	case "pealayer.update.manifest":
		verb, path = "GET", "/api/update/manifest"
	case "pealayer.update.status":
		verb, path = "GET", "/api/update/status"
	case "pealayer.update.upload.begin":
		path = "/api/update/begin"
	case "pealayer.update.upload.finish":
		path = "/api/update/finish"
	case "pealayer.update.upload.abort":
		path = "/api/update/abort"
	case "pealayer.update.from-url":
		path = "/api/update/from-url"
	case "pealayer.update.upload.chunk":
		var chunk struct {
			OperationID string `json:"operation_id"`
			Offset      uint64 `json:"offset"`
			Data        string `json:"data"`
		}
		if err := decodeStrictParams(rpc.Params, &chunk); err != nil {
			return nil, err
		}
		if chunk.OperationID == "" || len(chunk.OperationID) > 128 || len(chunk.Data) > ((softwareChunkBytes+2)/3)*4 {
			return nil, errors.New("invalid software chunk identity or size")
		}
		var err error
		body, err = base64.StdEncoding.Strict().DecodeString(chunk.Data)
		if err != nil || len(body) == 0 || len(body) > softwareChunkBytes {
			return nil, errors.New("invalid software chunk encoding or size")
		}
		path = "/api/update/chunk?id=" + url.QueryEscape(chunk.OperationID) + "&offset=" + strconv.FormatUint(chunk.Offset, 10)
		content = "application/octet-stream"
	case "pealayer.update.artifact.read", "pealayer.update.bundle.list", "pealayer.update.bundle.read":
		path = "/api/rpc"
		var err error
		body, err = json.Marshal(rpc)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported Pealayer update operation")
	}
	isRPCRead := rpc.Method == "pealayer.update.artifact.read" || rpc.Method == "pealayer.update.bundle.list" || rpc.Method == "pealayer.update.bundle.read"
	value, err := softwareHTTP(ctx, client, target.origin, verb, path, body, content, verb != "GET" && !isRPCRead)
	if err != nil {
		return nil, err
	}
	if isRPCRead {
		envelope, ok := value.(map[string]any)
		if !ok || envelope["jsonrpc"] != "2.0" {
			return nil, errors.New("invalid Pealayer artifact reply")
		}
		if failure, ok := envelope["error"].(map[string]any); ok {
			return nil, fmt.Errorf("Pealayer artifact read failed: %v", failure["message"])
		}
		result, ok := envelope["result"]
		if !ok {
			return nil, errors.New("Pealayer omitted artifact result")
		}
		return result, nil
	}
	return value, nil
}

func softwareHTTP(ctx context.Context, client *http.Client, origin, verb, path string, body []byte, content string, mutation bool) (any, error) {
	request, err := http.NewRequestWithContext(ctx, verb, origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "PCController software-updater")
	if content != "" {
		request.Header.Set("Content-Type", content)
	}
	response, err := client.Do(request)
	uncertain := func(err error) error {
		if mutation {
			return &RPCError{Code: ErrorCodeOutcomeUncertain, Message: "software update outcome unknown; do not replay the mutation", Data: map[string]any{"detail": err.Error()}}
		}
		return err
	}
	if err != nil {
		return nil, uncertain(err)
	}
	defer response.Body.Close()
	bytes, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return nil, uncertain(err)
	}
	if len(bytes) > 1<<20 {
		return nil, uncertain(errors.New("software reply exceeded 1 MiB"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("software target rejected request (HTTP %d)", response.StatusCode)
	}
	var value any
	if err = json.Unmarshal(bytes, &value); err != nil {
		return nil, uncertain(errors.New("software target returned invalid JSON"))
	}
	return value, nil
}
