package hostbridge

import (
	"encoding/json"
	"errors"
	"strings"

	"pccontroller.local/controller/internal/ipcjson"
)

// decodePeerResponse accepts additional metadata but never an ambiguous ACK.
// Identity and exactly one result/error are required for safe correlation.
func decodePeerResponse(raw []byte) (ipcjson.Response, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ipcjson.Response{}, err
	}
	var response ipcjson.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return response, err
	}
	_, result := fields["result"]
	_, failure := fields["error"]
	var method string
	if value, ok := fields["method"]; ok {
		_ = json.Unmarshal(value, &method)
	}
	id := strings.TrimSpace(string(response.ID))
	if response.JSONRPC != ipcjson.Version || result == failure || method != "" || id == "" || id == "null" {
		return response, errors.New("invalid peer JSON-RPC response envelope")
	}
	var identity any
	if err := json.Unmarshal(response.ID, &identity); err != nil {
		return response, err
	}
	switch identity.(type) {
	case string, float64:
	default:
		return response, errors.New("invalid peer response identity")
	}
	if failure && response.Error == nil {
		return response, errors.New("invalid peer error acknowledgement")
	}
	return response, nil
}

func (session *peerRPCSession) resolveRaw(raw []byte) bool {
	response, err := decodePeerResponse(raw)
	if err != nil {
		// If malformed data claims an in-flight ID, fail the call immediately
		// as outcome-uncertain instead of accepting its purported success.
		if len(response.ID) != 0 {
			return session.Resolve(ipcjson.Response{JSONRPC: ipcjson.Version, ID: response.ID,
				Error: &ipcjson.RPCError{Code: -32004, Message: "peer outcome uncertain: " + err.Error()}})
		}
		return false
	}
	return session.Resolve(response)
}
