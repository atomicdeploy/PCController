#pragma once

#include <Arduino.h>

namespace TemperatureRoles {

// Stable telemetry role indexes for the two sorted DS18B20 identities.
constexpr uint8_t Led = 0;
constexpr uint8_t BluetoothAudio = 1;

// The sorted-ROM factory mapping is first=tLED and second=tBT; EEPROM Swap
// reverses only those two roles without changing the reported ROM identities.
inline uint8_t fromSortedIndex(uint8_t index, bool swap) {
  return swap ? static_cast<uint8_t>(1U - index) : index;
}

} // namespace TemperatureRoles
