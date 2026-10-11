package control

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type RecordingRequest struct {
	Name             string           `json:"name,omitempty"`
	Category         string           `json:"category,omitempty"`
	Color            string           `json:"color,omitempty"`
	Reference        string           `json:"reference,omitempty"`
	CaptureMode      string           `json:"capture_mode,omitempty"`
	CaptureSelection CaptureSelection `json:"capture_selection"`
}

func decodeCaptureJSON(encoded string, target any) error {
	if len(encoded) > 32768 {
		return errors.New("capture request exceeds 16 KiB")
	}
	data, err := hex.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode capture JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return err
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("capture request must contain exactly one JSON value")
	}
	return nil
}
func (runner *MacroRunner) recordingJSONCommand(ctx context.Context, action, encoded string) (string, error) {
	if action == "selection" {
		var selection CaptureSelection
		if err := decodeCaptureJSON(encoded, &selection); err != nil {
			return "", err
		}
		runner.operationMu.Lock()
		defer runner.operationMu.Unlock()
		state := runner.RecordingState()
		if !state.Active {
			return "", errors.New("no recording is active")
		}
		validated, err := runner.validateCaptureSelection(selection, state.Mode, state.DeviceRetained)
		if err != nil {
			return "", err
		}
		if state.DeviceRetained {
			return "", errors.New("choose board-retained selection before starting the take")
		}
		runner.recordMu.Lock()
		runner.recording.CaptureSelection = validated
		runner.recordRelaySeen = false
		runner.recordMu.Unlock()
		runner.runtime.PublishStructuredEvent(Event{
			Kind: "macro.recording", Lifecycle: "selection-changed", State: "recording",
			Text: "recording capture selection changed",
		})
		data, _ := json.Marshal(runner.RecordingState())
		return string(data), nil
	}
	var request RecordingRequest
	if err := decodeCaptureJSON(encoded, &request); err != nil {
		return "", err
	}
	if action == "import-json" {
		if request.Reference != "" || request.CaptureMode != "" && request.CaptureMode != "board-retained" {
			return "", errors.New("import-json reads the retained board take")
		}
		macro, err := runner.ImportBoardRecording(request.Name, request.Category, request.Color, request.CaptureSelection)
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(macro)
		return string(data), nil
	}
	var state MacroRecordingState
	var err error
	if action == "append-json" {
		if request.Name != "" || request.Category != "" || request.Color != "" {
			return "", errors.New("append inherits the existing effect metadata")
		}
		state, err = runner.StartAppendingRecording(ctx, request.Reference, request.CaptureMode, request.CaptureSelection)
	} else {
		if request.Reference != "" {
			return "", errors.New("new recording does not accept reference")
		}
		switch request.CaptureMode {
		case "", "automatic":
			state, err = runner.StartRecording(request.Name, request.Category, request.Color, request.CaptureSelection)
		case "device-clock":
			state, err = runner.StartMCURecording(request.Name, request.Category, request.Color, request.CaptureSelection)
		case "board-retained":
			state, err = runner.StartBoardRecording(ctx, request.Name, request.Category, request.Color, request.CaptureSelection)
		default:
			return "", errors.New("capture_mode must be automatic, device-clock or board-retained")
		}
	}
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(state)
	return string(data), nil
}
