#include "virtual_board/sensor_sources.hpp"
#include <Arduino.h>
#include <charconv>
#include <filesystem>
#include <fstream>
#include <stdexcept>

namespace NativeHal {
namespace {
SensorSources sources;
uint64_t lastReadUs = 0;
bool readOnce = false;
}
bool readSensorInteger(const std::string &path, int64_t &value) {
  if (path.empty()) return false;
  std::error_code error;
  if (!std::filesystem::is_regular_file(path, error) || error) return false;
  std::ifstream input(path, std::ios::binary);
  char buffer[64];
  input.read(buffer, sizeof(buffer));
  const auto count = input.gcount();
  if (input.bad() || count <= 0 || count == sizeof(buffer)) return false;
  const char *first = buffer, *last = buffer + count;
  auto whitespace = [](char c) { return c==' ' || c=='\t' || c=='\r' || c=='\n'; };
  while (first != last && whitespace(*first)) ++first;
  while (first != last && whitespace(last[-1])) --last;
  int64_t parsed = 0;
  const auto result = std::from_chars(first, last, parsed);
  if (result.ec != std::errc{} || result.ptr != last) return false;
  value = parsed;
  return true;
}
void configureSensorSources(const SensorSources &value) {
  if (value.voltage.empty() != value.current.empty())
    throw std::invalid_argument("native power mapping requires both --sensor-voltage and --sensor-current; unknown current is not zero");
  sources = value;
  readOnce = false;
  // Reserve role zero when only role one is mapped, so discovery cannot move
  // the second named source into the first role after unplug/recovery.
  temperatureConfigured[0] = !sources.temperature[0].empty() || !sources.temperature[1].empty();
  temperatureConfigured[1] = !sources.temperature[1].empty();
}
bool refreshSensorSources(uint64_t nowUs, bool force) {
  if (!force && readOnce && nowUs - lastReadUs < 1000000) return false;
  readOnce = true; lastReadUs = nowUs;
  for (size_t i=0; i<2; ++i) {
    if (sources.temperature[i].empty()) continue;
    int64_t milliC = 0;
    temperature[i] = readSensorInteger(sources.temperature[i], milliC) && milliC >= -55000 && milliC <= 125000
      ? static_cast<int16_t>(milliC / 10) : INT16_MIN;
  }
  if (!sources.voltage.empty()) {
    int64_t microV = 0, microA = 0;
    powerAvailable = readSensorInteger(sources.voltage, microV) && readSensorInteger(sources.current, microA)
      && microV >= 0 && microV <= 100000000 && microA >= -100000000 && microA <= 100000000;
    if (powerAvailable) {
      supplyMilliVolts = static_cast<int32_t>(microV / 1000);
      currentMilliAmps = static_cast<int32_t>(microA / 1000);
      powerMilliWatts = static_cast<int32_t>(static_cast<int64_t>(supplyMilliVolts) * currentMilliAmps / 1000);
    }
  }
  return true;
}
}
