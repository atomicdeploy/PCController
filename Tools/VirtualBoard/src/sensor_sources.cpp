#include "virtual_board/sensor_sources.hpp"
#include <Arduino.h>
#include <charconv>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <stdexcept>
#include <vector>

namespace NativeHal {
namespace {
SensorSources sources;
uint64_t lastReadUs = 0;
bool readOnce = false;

std::filesystem::path ownedSensorDirectory() {
  auto env = [](const char *key) { const char *value = std::getenv(key); return value ? std::string(value) : std::string(); };
#ifdef _WIN32
  return std::filesystem::path(env("LOCALAPPDATA")) / "Programs" / "PCController" / "sensors";
#elif defined(__APPLE__)
  return std::filesystem::path(env("HOME")) / "Library" / "Application Support" / "PCController" / "sensors";
#else
  const auto state = env("XDG_STATE_HOME");
  return (state.empty() ? std::filesystem::path(env("HOME")) / ".local" / "state" : std::filesystem::path(state)) / "pccontroller" / "sensors";
#endif
}

// A mapped scalar is not a general-purpose filesystem read. Resolve links
// before checking the complete directory boundary (not a textual prefix).
std::filesystem::path validatedSensorPath(const std::string &input) {
  if (input.empty() || input.size() > 4096 || input.find('\0') != std::string::npos || input.find("..") != std::string::npos) return {};
  const std::filesystem::path requested(input);
  if (!requested.is_absolute()) return {};
  std::error_code error;
  const auto candidate = std::filesystem::weakly_canonical(requested, error);
  if (error) return {};
  std::vector<std::filesystem::path> roots{ownedSensorDirectory()};
#ifdef __linux__
  roots.emplace_back("/sys");
#endif
  for (const auto &root : roots) {
    if (!root.is_absolute()) continue;
    const auto canonicalRoot = std::filesystem::weakly_canonical(root, error);
    if (error) { error.clear(); continue; }
    const auto relative = candidate.lexically_relative(canonicalRoot);
    if (relative.empty() || relative.is_absolute()) continue;
    bool safe = true;
    for (const auto &part : relative) if (part == "..") { safe = false; break; }
    if (safe && relative != ".") return candidate;
  }
  return {};
}
}
bool readSensorInteger(const std::string &path, int64_t &value) {
  const auto validated = validatedSensorPath(path);
  if (validated.empty()) return false;
  std::error_code error;
  if (!std::filesystem::is_regular_file(validated, error) || error) return false;
  std::ifstream input(validated, std::ios::binary);
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
  for (const auto &path : {value.temperature[0], value.temperature[1], value.voltage, value.current})
    if (!path.empty() && validatedSensorPath(path).empty())
      throw std::invalid_argument("native sensor paths must be absolute scalar files inside /sys (Linux) or the PCController owned sensors directory; traversal and escaping links are rejected");
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
