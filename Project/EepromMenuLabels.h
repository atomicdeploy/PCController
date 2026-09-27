#pragma once

#include <stdint.h>

// Optional EEPROM-backed replacement for the packed four-character menu label
// table. It is intentionally narrow: labels remain fixed-width and read-only
// to firmware, while the host provisions their factory bytes and checksum.
namespace EepromMenuLabels {

constexpr uint8_t LabelWidth = 4;

// Validates the factory-provisioned label block once during board startup.
// It never writes EEPROM and leaves the fallback active when validation fails.
void begin();

// Indicates whether copy() can return validated EEPROM label bytes.
bool available();

// Copies one four-character label into caller-owned display storage. A missing,
// corrupt, or out-of-range label becomes four dashes without an SRAM cache.
void copy(uint8_t page, char output[LabelWidth]);

} // namespace EepromMenuLabels
