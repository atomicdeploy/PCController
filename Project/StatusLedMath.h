#pragma once

#include <stdint.h>

// Byte-exact renderer primitives shared by the production AVR controller and
// host-native component tests. Keep this header independent from Arduino so a
// later VirtualBoard adapter can compile the production engine unchanged.
namespace StatusLedMath {

inline uint8_t scale(uint8_t value, uint8_t level) {
  return static_cast<uint8_t>(
      (static_cast<uint16_t>(value) * (static_cast<uint16_t>(level) + 1U)) >>
      8);
}

// Unsigned magnitude arithmetic avoids signed 16-bit overflow on AVR while
// preserving the /256 wire contract in both directions.
inline uint8_t interpolate(uint8_t from, uint8_t to, uint8_t phase) {
  if (to >= from) {
    return static_cast<uint8_t>(
        from + (static_cast<uint16_t>(to - from) * phase) / 256U);
  }
  return static_cast<uint8_t>(
      from - (static_cast<uint16_t>(from - to) * phase) / 256U);
}

// Deadline for one of 64 phases. Distributing period%64 across the phase
// boundaries keeps the configured duration exact without accumulating loop
// quantization drift.
inline uint16_t phaseDeadline(uint16_t periodMs, uint8_t step) {
  const uint16_t whole = static_cast<uint16_t>(periodMs >> 6);
  const uint8_t remainder = static_cast<uint8_t>(periodMs & 63U);
  return static_cast<uint16_t>(
      static_cast<uint16_t>(step) * whole +
      ((static_cast<uint16_t>(step) * remainder) >> 6));
}

} // namespace StatusLedMath
