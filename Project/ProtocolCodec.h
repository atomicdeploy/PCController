#pragma once

// Target-neutral bounded native wire algorithms.
//
// The AVR UART adapter and native firmware tests use these exact C++11
// routines. They deliberately use only caller-owned fixed buffers: no Arduino
// types, STL, heap allocation, clocks, or transport state belong here.

#include <stdint.h>

namespace ControllerProtocol {
namespace WireCodec {

// Advances a CRC-8/ATM value (polynomial 0x07) by one byte. Streaming readers
// use it to verify EEPROM records without allocating a record-sized buffer.
inline uint8_t crc8Update(uint8_t crc, uint8_t value) {
  crc ^= value;
  for (uint8_t bit = 0; bit < 8; ++bit) {
    crc = (crc & 0x80) ? static_cast<uint8_t>((crc << 1) ^ 0x07)
                       : static_cast<uint8_t>(crc << 1);
  }
  return crc;
}

// CRC-8/ATM (polynomial 0x07, initial value zero) for an unencoded envelope.
inline uint8_t crc8(const uint8_t *data, uint8_t length) {
  uint8_t crc = 0;
  while (length-- != 0) {
    crc = crc8Update(crc, *data++);
  }
  return crc;
}

// Decodes one delimiter-free COBS packet into caller-owned storage. A zero
// return means either malformed input or insufficient output capacity; valid
// protocol packets always have a non-zero decoded envelope length.
inline uint8_t cobsDecode(const uint8_t *input, uint8_t length,
                          uint8_t *output, uint8_t capacity) {
  uint8_t readIndex = 0;
  uint8_t writeIndex = 0;
  while (readIndex < length) {
    const uint8_t code = input[readIndex++];
    if (code == 0) {
      return 0;
    }
    const uint8_t count = static_cast<uint8_t>(code - 1);
    if (count > static_cast<uint8_t>(length - readIndex) ||
        count > static_cast<uint8_t>(capacity - writeIndex)) {
      return 0;
    }
    for (uint8_t index = 0; index < count; ++index) {
      output[writeIndex++] = input[readIndex++];
    }
    if (code != 0xFF && readIndex < length) {
      if (writeIndex >= capacity) {
        return 0;
      }
      output[writeIndex++] = 0;
    }
  }
  return writeIndex;
}

} // namespace WireCodec
} // namespace ControllerProtocol
