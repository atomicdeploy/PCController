#include "I2cLcd.h"

#if PCCONTROLLER_ENABLE_I2C_LCD
#include "../Project/CompactI2c.h"
#include <avr/pgmspace.h>
#include <string.h>

namespace {
// Common PCF8574 LCD addresses, backpack pin bits, and probe pacing.
constexpr uint8_t CandidateAddresses[] = {0x27, 0x3F};
constexpr uint8_t BacklightBit = 0x08;
constexpr uint8_t EnableBit = 0x04;
constexpr uint8_t RegisterSelectBit = 0x01;
constexpr uint16_t ProbeSpacingMs = 10;

bool timeReached(uint32_t now, uint32_t deadline) {
  return static_cast<int32_t>(now - deadline) >= 0;
}
} // namespace
#endif

I2cLcd optionalLcd;

void I2cLcd::begin(uint32_t now) { rescan(now); }

void I2cLcd::serviceDiscovery(uint32_t now) {
#if PCCONTROLLER_ENABLE_I2C_LCD
  if (address_ != 0 || scanComplete_ || !timeReached(now, nextProbeAt_)) {
    return;
  }
  const uint8_t candidate = CandidateAddresses[scanIndex_++];
  if (probe(candidate)) {
    address_ = candidate;
    scanComplete_ = true;
  } else if (scanIndex_ >= sizeof(CandidateAddresses)) {
    scanComplete_ = true;
  } else {
    nextProbeAt_ = now + ProbeSpacingMs;
  }
#else
  (void)now;
#endif
}

void I2cLcd::rescan(uint32_t now) {
#if PCCONTROLLER_ENABLE_I2C_LCD
  address_ = 0;
  initialized_ = false;
  scanIndex_ = 0;
  scanComplete_ = false;
  nextProbeAt_ = now;
  invalidateCache();
#else
  (void)now;
#endif
}

bool I2cLcd::detected() const {
#if PCCONTROLLER_ENABLE_I2C_LCD
  return address_ != 0;
#else
  return false;
#endif
}

bool I2cLcd::initialize() {
#if PCCONTROLLER_ENABLE_I2C_LCD
  if (address_ == 0) {
    return false;
  }
  if (initialized_) {
    return true;
  }

  delay(50);
  writeNibble(0x30);
  delayMicroseconds(4500);
  writeNibble(0x30);
  delayMicroseconds(4500);
  writeNibble(0x30);
  delayMicroseconds(150);
  writeNibble(0x20);
  command(0x28); // 4-bit, 2-line, 5x8
  command(0x08); // display off
  command(0x01); // clear
  delayMicroseconds(2000);
  command(0x06); // left-to-right entry
  command(0x0C); // display on, cursor off
  initialized_ = true;
  invalidateCache();
  return true;
#else
  return false;
#endif
}

bool I2cLcd::available() const {
#if PCCONTROLLER_ENABLE_I2C_LCD
  return initialized_;
#else
  return false;
#endif
}

bool I2cLcd::scanComplete() const {
#if PCCONTROLLER_ENABLE_I2C_LCD
  return scanComplete_;
#else
  return true;
#endif
}

uint8_t I2cLcd::address() const {
#if PCCONTROLLER_ENABLE_I2C_LCD
  return address_;
#else
  return 0;
#endif
}

void I2cLcd::clear() {
#if PCCONTROLLER_ENABLE_I2C_LCD
  if (!initialized_) {
    return;
  }
  command(0x01);
  delayMicroseconds(2000);
  for (uint8_t row = 0; row < Rows; ++row) {
    memset(lineCache_[row], ' ', Columns);
    lineCache_[row][Columns] = '\0';
  }
#endif
}

void I2cLcd::showLine(uint8_t row, const char *text) {
#if PCCONTROLLER_ENABLE_I2C_LCD
  if (!initialized_ || row >= Rows) {
    return;
  }
  char normalized[Columns + 1];
  uint8_t index = 0;
  if (text != nullptr) {
    while (index < Columns && text[index] != '\0') {
      normalized[index] = text[index];
      ++index;
    }
  }
  while (index < Columns) {
    normalized[index++] = ' ';
  }
  normalized[Columns] = '\0';
  writeNormalized(row, normalized);
#else
  (void)row;
  (void)text;
#endif
}

void I2cLcd::showLine(uint8_t row, const __FlashStringHelper *text) {
#if PCCONTROLLER_ENABLE_I2C_LCD
  char normalized[Columns + 1];
  uint8_t index = 0;
  const char *source = reinterpret_cast<const char *>(text);
  if (source != nullptr) {
    while (index < Columns) {
      const char value = static_cast<char>(pgm_read_byte(source + index));
      if (value == '\0') {
        break;
      }
      normalized[index++] = value;
    }
  }
  while (index < Columns) {
    normalized[index++] = ' ';
  }
  normalized[Columns] = '\0';
  if (initialized_ && row < Rows) {
    writeNormalized(row, normalized);
  }
#else
  (void)row;
  (void)text;
#endif
}

void I2cLcd::showText(const uint8_t *text, uint8_t length) {
#if PCCONTROLLER_ENABLE_I2C_LCD
  char line[Columns + 1];
  if (length > Rows * Columns) {
    length = Rows * Columns;
  }
  for (uint8_t row = 0; row < Rows; ++row) {
    const uint8_t offset = static_cast<uint8_t>(row * Columns);
    uint8_t index = 0;
    while (index < Columns && offset + index < length) {
      line[index] = static_cast<char>(text[offset + index]);
      ++index;
    }
    line[index] = '\0';
    showLine(row, line);
  }
#else
  (void)text;
  (void)length;
#endif
}

void I2cLcd::setBacklight(bool enabled) {
#if PCCONTROLLER_ENABLE_I2C_LCD
  if (backlight_ == enabled) {
    return;
  }
  backlight_ = enabled;
  if (address_ != 0) {
    writeExpander(0);
  }
#else
  (void)enabled;
#endif
}

void I2cLcd::copyText(uint8_t *destination) const {
#if PCCONTROLLER_ENABLE_I2C_LCD
  memcpy(destination, lineCache_[0], Columns);
  memcpy(destination + Columns, lineCache_[1], Columns);
#else
  memset(destination, ' ', 32);
#endif
}

bool I2cLcd::backlight() const {
#if PCCONTROLLER_ENABLE_I2C_LCD
  return backlight_;
#else
  return false;
#endif
}

#if PCCONTROLLER_ENABLE_I2C_LCD
bool I2cLcd::probe(uint8_t address) {
  i2cBus.beginTransmission(address);
  return i2cBus.endTransmission() == 0;
}

bool I2cLcd::writeExpander(uint8_t value) {
  i2cBus.beginTransmission(address_);
  i2cBus.write(static_cast<uint8_t>(value |
                                    (backlight_ ? BacklightBit : 0)));
  return i2cBus.endTransmission() == 0;
}

void I2cLcd::pulseEnable(uint8_t value) {
  writeExpander(static_cast<uint8_t>(value | EnableBit));
  delayMicroseconds(1);
  writeExpander(static_cast<uint8_t>(value & ~EnableBit));
  delayMicroseconds(50);
}

void I2cLcd::writeNibble(uint8_t value) {
  writeExpander(value);
  pulseEnable(value);
}

void I2cLcd::send(uint8_t value, bool data) {
  const uint8_t mode = data ? RegisterSelectBit : 0;
  writeNibble(static_cast<uint8_t>((value & 0xF0) | mode));
  writeNibble(static_cast<uint8_t>((value << 4) | mode));
}

void I2cLcd::command(uint8_t value) { send(value, false); }

void I2cLcd::invalidateCache() {
  memset(lineCache_, 0xFF, sizeof(lineCache_));
}

void I2cLcd::writeNormalized(uint8_t row, const char *normalized) {
  if (memcmp(lineCache_[row], normalized, Columns + 1) == 0) {
    return;
  }
  command(static_cast<uint8_t>(0x80 | (row == 0 ? 0x00 : 0x40)));
  for (uint8_t index = 0; index < Columns; ++index) {
    send(static_cast<uint8_t>(normalized[index]), true);
  }
  memcpy(lineCache_[row], normalized, Columns + 1);
}
#endif
