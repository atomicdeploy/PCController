#pragma once
#include <array>
#include <cstdint>
#include <string>

namespace NativeHal {
// Explicit read-only mappings, never automatic CPU -> lighting assignment.
// Linux hwmon/thermal use milli-Celsius; power_supply uses micro-V/A.
struct SensorSources {
  std::array<std::string, 2> temperature;
  std::string voltage, current;
};
void configureSensorSources(const SensorSources &sources);
bool refreshSensorSources(uint64_t nowUs, bool force = false);
// Bounded strict parser shared by adapters/tests. Missing data remains unknown.
bool readSensorInteger(const std::string &path, int64_t &value);
}
