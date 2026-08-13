#pragma once

#include <stdint.h>

// Optional EEPROM-backed replacement for the packed four-character menu label
// table. It is intentionally narrow: labels remain fixed-width and read-only
// to firmware. The host provisions a versioned CRC record whose commit marker
// is written last; firmware never accepts an interrupted or future-format
// record and does not spend flash on an EEPROM migration chain.
namespace EepromMenuLabels {

constexpr uint8_t LabelWidth = 4;

// Validates the factory-provisioned label block once during board startup.
// It never writes EEPROM and leaves the fallback active when validation fails.
void begin();

// Indicates that the format marker and CRC validate. read()/copy() still
// sanitize every cell, so even a deliberately CRC-correct control byte cannot
// reach the display. The host writer rejects such bytes before provisioning.
bool available();

// Copies one four-character label into caller-owned display storage. Missing,
// corrupt, out-of-range, or non-printable cells become dashes without a cache.
void copy(uint8_t page, char output[LabelWidth]);

// Returns one display-safe label byte or '-' when the block is unavailable or
// the requested cell lies outside the fixed board menu catalog.
char read(uint8_t page, uint8_t character);

} // namespace EepromMenuLabels
