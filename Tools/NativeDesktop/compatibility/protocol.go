// Package compatibility owns historical wire vocabulary retained for device
// interoperability. PCController uses the host IPC/RPC presentation bridge;
// these routes never grant board authority or identify the current product.
package compatibility

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed device-protocol.json
var protocolData []byte

var protocol = func() struct {
	StatePrefix string   `json:"statePrefix"`
	StateFields []string `json:"stateFields"`
} {
	var value struct {
		StatePrefix string   `json:"statePrefix"`
		StateFields []string `json:"stateFields"`
	}
	if err := json.Unmarshal(protocolData, &value); err != nil {
		panic("invalid embedded compatibility protocol")
	}
	return value
}()

func StateEndpoint(command string) string {
	switch command {
	case "get", "on", "off", "toggle":
		return protocol.StatePrefix + command
	default:
		return ""
	}
}

func IsStateField(key string) bool {
	for _, field := range protocol.StateFields {
		if strings.EqualFold(key, field) {
			return true
		}
	}
	return false
}
