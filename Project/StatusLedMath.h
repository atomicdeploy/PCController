#pragma once

#include <stdint.h>

// StatusLedMath is the byte-exact procedural contract shared by AVR and the
// VirtualBoard. Unsigned magnitude arithmetic avoids 16-bit AVR overflow.
namespace StatusLedMath {

inline uint8_t scale(uint8_t value, uint8_t level) {
  return static_cast<uint8_t>(
      (static_cast<uint16_t>(value) * (static_cast<uint16_t>(level) + 1U)) >>
      8);
}

inline uint8_t interpolate(uint8_t from, uint8_t to, uint8_t phase) {
  if (to >= from) {
    return static_cast<uint8_t>(
        from + (static_cast<uint16_t>(to - from) * phase) / 256U);
  }
  return static_cast<uint8_t>(
      from - (static_cast<uint16_t>(from - to) * phase) / 256U);
}

inline uint16_t phaseDeadline(uint16_t periodMs, uint8_t step) {
  const uint16_t whole = static_cast<uint16_t>(periodMs >> 6);
  const uint8_t remainder = static_cast<uint8_t>(periodMs & 63U);
  return static_cast<uint16_t>(
      static_cast<uint16_t>(step) * whole +
      ((static_cast<uint16_t>(step) * remainder) >> 6));
}

} // namespace StatusLedMath
