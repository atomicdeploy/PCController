package ipcjson

import (
	"encoding/json"
	"errors"
	"strings"

	"pccontroller.local/controller/internal/shell"
)

// ValidateBridgeRequest prevents both immediate and persisted third-peer pivots.
// This topology guard remains active while alpha authentication is disabled.
func ValidateBridgeRequest(request Request) error {
	if peerUpdate, _ := requestInvokesPeerHostUpdate(request.Method, request.Params, 0); peerUpdate {
		return errors.New("peer host updates may not be chained through a bridge; bridge ingress may not pivot")
	}
	if bridgeIngressCanPivot(request.Method, request.Params) {
		return errors.New("recursive bridge calls are not permitted; bridge ingress may not pivot")
	}
	return nil
}

func bridgeCommandWords(command string) []string {
	words, err := shell.Split(command)
	if err != nil {
		return nil
	}
	for index := range words {
		words[index] = strings.ToLower(words[index])
	}
	return words
}

func requestInvokesPeerHostUpdate(method string, params json.RawMessage, depth int) (bool, bool) {
	if depth > 4 {
		return true, true
	}
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "controller.peer.update.host":
		return true, false
	case "controller.command.execute", "controller.app.action":
		var value struct {
			Command string `json:"command"`
			Kind    string `json:"kind"`
			Value   string `json:"value"`
		}
		if json.Unmarshal(params, &value) != nil {
			return false, false
		}
		command := value.Command
		if method == "controller.app.action" {
			if !strings.EqualFold(strings.TrimSpace(value.Kind), "command") {
				return false, false
			}
			command = value.Value
		}
		words, err := shell.Split(command)
		if err != nil || len(words) == 0 {
			return false, false
		}
		if strings.EqualFold(words[0], "peer-update") {
			return true, false
		}
		if len(words) >= 4 && strings.EqualFold(words[0], "bridge") && strings.EqualFold(words[1], "call") {
			var nested json.RawMessage
			if len(words) >= 5 {
				nested = json.RawMessage(words[4])
			}
			invokes, _ := requestInvokesPeerHostUpdate(words[3], nested, depth+1)
			return invokes, invokes
		}
	case "controller.bridge.call":
		var value struct {
			Request Request `json:"request"`
		}
		if json.Unmarshal(params, &value) == nil {
			invokes, _ := requestInvokesPeerHostUpdate(value.Request.Method, value.Request.Params, depth+1)
			return invokes, invokes
		}
	}
	return false, false
}
