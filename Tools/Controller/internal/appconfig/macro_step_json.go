package appconfig

import (
	"bytes"
	"encoding/json"
)

// Alpha definitions use the current contract, not silently ignored authoring
// fields. Reject unknown step fields so retired timing controls cannot be lost.
func (step *MacroStep) UnmarshalJSON(data []byte) error {
	type currentStep MacroStep
	var value currentStep
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*step = MacroStep(value)
	return nil
}
