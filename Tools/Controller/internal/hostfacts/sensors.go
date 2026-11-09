package hostfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
)

const maxSensorInputBytes = 2 * 1024 * 1024

func sensorSource() string {
	switch runtime.GOOS {
	case "linux":
		return "lm-sensors"
	case "windows":
		return "librehardwaremonitor"
	default:
		return "unsupported"
	}
}

func sensorNumber(value any) (float64, bool) {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case json.Number:
		var err error
		number, err = value.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func sensorRow(id, name, hardware, kind, unit, source string, value any) map[string]any {
	number, available := sensorNumber(value)
	row := map[string]any{
		"ID": id, "Name": name, "Hardware": hardware, "Type": kind,
		"Unit": unit, "Source": source, "Available": available, "Value": nil,
	}
	if available {
		row["Value"] = number
	}
	return row
}

func optionalSensorNumber(row map[string]any, key string, value any) {
	if number, ok := sensorNumber(value); ok {
		row[key] = number
	}
}

func boundedSensorRows(rows []map[string]any) ([]map[string]any, bool) {
	sort.Slice(rows, func(i, j int) bool { return rows[i]["ID"].(string) < rows[j]["ID"].(string) })
	truncated := len(rows) > maxGlobalRows
	if truncated {
		rows = rows[:maxGlobalRows]
	}
	return rows, truncated
}

// sensors -j can emit bare NaN for unreadable subfeatures. Convert only whole
// non-finite tokens outside strings to JSON null, never labels or quoted text.
func sensorJSON(data []byte) ([]byte, error) {
	if len(data) > maxSensorInputBytes {
		return nil, errors.New("sensor response exceeds 2 MiB")
	}
	var output bytes.Buffer
	quoted, escaped := false, false
	for index := 0; index < len(data); {
		character := data[index]
		if quoted {
			output.WriteByte(character)
			index++
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				quoted = false
			}
			continue
		}
		if character == '"' {
			quoted = true
			output.WriteByte(character)
			index++
			continue
		}
		matched := false
		for _, token := range []string{"NaN", "Infinity", "-Infinity"} {
			end := index + len(token)
			if end <= len(data) && string(data[index:end]) == token &&
				(index == 0 || strings.ContainsRune(" \t\r\n:[,", rune(data[index-1]))) &&
				(end == len(data) || strings.ContainsRune(" \t\r\n,]}", rune(data[end]))) {
				output.WriteString("null")
				index = end
				matched = true
				break
			}
		}
		if !matched {
			output.WriteByte(character)
			index++
		}
	}
	return output.Bytes(), nil
}

func parseLMSensors(data []byte) ([]map[string]any, bool, error) {
	data, err := sensorJSON(data)
	if err != nil {
		return nil, false, err
	}
	var chips map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &chips); err != nil {
		return nil, false, fmt.Errorf("decode lm-sensors JSON: %w", err)
	}
	if chips == nil {
		return nil, false, errors.New("lm-sensors did not return a chip object")
	}
	rows := make([]map[string]any, 0)
	for chip, features := range chips {
		for label, raw := range features {
			if label == "Adapter" {
				continue
			}
			var values map[string]any
			if err := json.Unmarshal(raw, &values); err != nil || values == nil {
				return nil, false, errors.New("invalid lm-sensors feature object")
			}
			bases := make(map[string]bool)
			for field := range values {
				base, suffix, ok := strings.Cut(field, "_")
				if ok && (suffix == "input" || suffix == "average" || suffix == "min" || suffix == "max" || suffix == "crit" || suffix == "lcrit" || suffix == "alarm" || suffix == "fault") {
					bases[base] = true
				}
			}
			for base := range bases {
				kind, unit := lmSensorKind(base)
				if kind == "" {
					continue
				}
				value, present := values[base+"_input"]
				if !present {
					value = values[base+"_average"]
				}
				row := sensorRow("lm-sensors:"+chip+"/"+base, label, chip, kind, unit, "lm-sensors", value)
				if _, average := values[base+"_average"]; !present && average {
					row["Statistic"] = "average"
				}
				optionalSensorNumber(row, "LowLimit", values[base+"_min"])
				optionalSensorNumber(row, "HighLimit", values[base+"_max"])
				optionalSensorNumber(row, "CriticalLow", values[base+"_lcrit"])
				optionalSensorNumber(row, "CriticalHigh", values[base+"_crit"])
				if alarm, ok := sensorNumber(values[base+"_alarm"]); ok {
					row["Alarm"] = alarm != 0
				}
				if fault, ok := sensorNumber(values[base+"_fault"]); ok {
					row["Fault"] = fault != 0
					if fault != 0 {
						row["Value"], row["Available"] = nil, false
					}
				}
				rows = append(rows, row)
			}
		}
	}
	rows, truncated := boundedSensorRows(rows)
	return rows, truncated, nil
}

func lmSensorKind(base string) (string, string) {
	for _, spec := range []struct{ prefix, kind, unit string }{
		{"temp", "temperature", "°C"}, {"in", "voltage", "V"}, {"curr", "current", "A"},
		{"power", "power", "W"}, {"fan", "fan", "rpm"}, {"energy", "energy", "J"},
		{"humidity", "humidity", "%"}, {"freq", "frequency", "Hz"},
	} {
		if strings.HasPrefix(base, spec.prefix) && len(base) > len(spec.prefix) {
			valid := true
			for _, digit := range base[len(spec.prefix):] {
				if digit < '0' || digit > '9' {
					valid = false
				}
			}
			if valid {
				return spec.kind, spec.unit
			}
		}
	}
	return "", ""
}

// Use raw numeric values only; the monitor's formatted strings may be localized
// and expressed in different units. No display-text parsing or guessed readings.
type lhmNode struct {
	Text       string    `json:"Text"`
	SensorID   string    `json:"SensorId"`
	HardwareID string    `json:"HardwareId"`
	Type       string    `json:"Type"`
	Value      any       `json:"RawValue"`
	Min        any       `json:"RawMin"`
	Max        any       `json:"RawMax"`
	Children   []lhmNode `json:"Children"`
}

func decodeLibreTree(data []byte) (lhmNode, error) {
	if len(data) > maxSensorInputBytes {
		return lhmNode{}, errors.New("sensor response exceeds 2 MiB")
	}
	var root lhmNode
	if err := json.Unmarshal(data, &root); err != nil {
		return root, fmt.Errorf("decode Libre Hardware Monitor JSON: %w", err)
	}
	if root.Children == nil {
		return root, errors.New("Libre Hardware Monitor response has no sensor tree")
	}
	return root, nil
}

func parseLibreSensors(data []byte) ([]map[string]any, bool, error) {
	root, err := decodeLibreTree(data)
	if err != nil {
		return nil, false, err
	}
	return libreSensorRows(root)
}

func libreSensorRows(root lhmNode) ([]map[string]any, bool, error) {
	rows := make([]map[string]any, 0)
	seen := make(map[string]bool)
	var visit func(lhmNode, string, int) error
	visit = func(node lhmNode, hardware string, depth int) error {
		if depth > 32 {
			return errors.New("sensor tree exceeds 32 levels")
		}
		if node.HardwareID != "" {
			hardware = node.HardwareID
		}
		if node.SensorID != "" {
			if seen[node.SensorID] {
				return errors.New("duplicate Libre Hardware Monitor sensor identity")
			}
			seen[node.SensorID] = true
			unit, known := libreSensorUnits[node.Type]
			if !known {
				return fmt.Errorf("unknown Libre Hardware Monitor sensor type %q", sanitizeText(node.Type))
			}
			row := sensorRow("librehardwaremonitor:"+node.SensorID, node.Text, hardware, strings.ToLower(node.Type), unit, "librehardwaremonitor", node.Value)
			optionalSensorNumber(row, "ObservedMin", node.Min)
			optionalSensorNumber(row, "ObservedMax", node.Max)
			rows = append(rows, row)
		}
		for _, child := range node.Children {
			if err := visit(child, hardware, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root, "", 0); err != nil {
		return nil, false, err
	}
	rows, truncated := boundedSensorRows(rows)
	return rows, truncated, nil
}

var libreSensorUnits = map[string]string{
	"Temperature": "°C", "Voltage": "V", "Current": "A", "Power": "W", "Fan": "rpm",
	"Clock": "MHz", "Frequency": "Hz", "Load": "%", "Control": "%", "Level": "%",
	// Data units differ between upstream builds (GiB/MiB versus bytes). Never
	// infer their units from a version or silently call a native count bytes.
	"Humidity": "%", "Flow": "L/h", "Factor": "1", "Data": "native", "SmallData": "native", "Throughput": "B/s",
	"TimeSpan": "s", "Timing": "ns", "Energy": "mWh", "Noise": "dBA", "Conductivity": "µS/cm",
}
