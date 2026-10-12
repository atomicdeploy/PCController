#pragma once

#include <Arduino.h>

namespace TemperatureRoles {

// Stable telemetry role indexes for the two sorted DS18B20 identities.
constexpr uint8_t Led = 0;
constexpr uint8_t BluetoothAudio = 1;

// A commissioned lighting ROM is authoritative. Discovery order before
// commissioning is temporary and is never persisted as identity.
} // namespace TemperatureRoles
