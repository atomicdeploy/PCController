#include "virtual_board/activity.hpp"
#include <iomanip>
#include <sstream>

namespace pccontroller::virtual_board {
std::vector<std::string> activityLines(const OutputSnapshot &before,
                                     const OutputSnapshot &after,
                                     bool pwm, bool peripherals, bool legacy) {
  std::vector<std::string> lines;
  for (unsigned i = 0; i < 8; ++i) {
    const unsigned bit = 1U << i;
    if (((before.relayMask ^ after.relayMask) & bit) == 0) continue;
    const std::string state = (after.relayMask & bit) ? "ON" : "OFF";
    lines.push_back(legacy ? state + ":" + std::to_string(i + 1)
                           : "R" + std::to_string(i + 1) + ":" + state);
  }
  if (pwm) for (size_t i = 0; i < after.pwm.size(); ++i)
    if (before.pwm[i] != after.pwm[i])
      lines.push_back((legacy ? "PWM" : "P") + std::to_string(i + 1) + ":" + std::to_string(after.pwm[i]));
  if (!peripherals) return lines;
  if (before.buzzerHz != after.buzzerHz) lines.push_back("BZ:" + std::to_string(after.buzzerHz));
  if (before.tm1637Detected != after.tm1637Detected)
    lines.push_back(std::string("DISPLAY:") + (after.tm1637Detected ? "ON" : "OFF"));
  if (before.brightness != after.brightness) lines.push_back("BRIGHTNESS:" + std::to_string(after.brightness));
  auto hex = [](const auto &bytes) {
    std::ostringstream text;
    text << std::hex << std::setfill('0');
    for (auto byte : bytes) text << std::setw(2) << unsigned(byte);
    return text.str();
  };
  if (before.segments != after.segments) lines.push_back("SEG:" + hex(after.segments));
  if (before.strip != after.strip) lines.push_back("STRIP:" + hex(after.strip));
  return lines;
}
}
