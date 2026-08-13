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

// Indicates that the format marker and CRC validate. The canonical host writer
// rejects non-printable cells before provisioning; a failed record exposes only
// the four-dash fallback instead of arbitrary EEPROM bytes.
bool available();

// Copies one four-character label into caller-owned display storage. A missing,
// corrupt, or out-of-range record becomes dashes without an SRAM cache.
void copy(uint8_t page, char output[LabelWidth]);

// Returns one display-safe label byte or '-' when the block is unavailable or
// the requested cell lies outside the fixed board menu catalog.
char read(uint8_t page, uint8_t character);

} // namespace EepromMenuLabels
