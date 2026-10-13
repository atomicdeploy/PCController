package appconfig

// NativeSensorNames applies truthful defaults without changing stored names or
// claiming a CPU/battery source belongs to the physical lighting/audio circuit.
// Profile presentation still takes precedence in ProfileDescriptors.
func NativeSensorNames(names map[string]string) map[string]string {
	result := make(map[string]string, len(names)+6)
	for key, name := range names {
		result[key] = name
	}
	for key, name := range map[string]string{
		"sensor.supply-voltage":    "Native voltage",
		"sensor.bus-voltage":       "Native voltage",
		"sensor.current":           "Native current",
		"sensor.power":             "Native power",
		"sensor.temperature-led":   "Native temperature 1",
		"sensor.temperature-audio": "Native temperature 2",
	} {
		if result[key] == "" {
			result[key] = name
		}
	}
	return result
}
