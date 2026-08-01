#pragma once

#include <Arduino.h>

#include "../ProjectConfig.h"

// Lightweight HD44780 + PCF8574 backpack driver. Discovery is intentionally
// bounded to the common 0x27 and 0x3F addresses, one probe per service call.
class I2cLcd {
public:
  void begin(uint32_t now = millis());
  void serviceDiscovery(uint32_t now = millis());
  void rescan(uint32_t now = millis());

  bool detected() const;
  bool initialize();
  bool available() const;
  bool scanComplete() const;
  uint8_t address() const;

  void clear();
  void showLine(uint8_t row, const char *text);
  void showLine(uint8_t row, const __FlashStringHelper *text);
  // Bounded host bridge: up to 32 bytes are split across the two 16-column
  // rows and cached, so no periodic redraw or heap-backed String is needed.
  void showText(const uint8_t *text, uint8_t length);
  void setBacklight(bool enabled);
  void copyText(uint8_t *destination) const;
  bool backlight() const;

private:
#if PCCONTROLLER_ENABLE_I2C_LCD
  static constexpr uint8_t Columns = 16;
  static constexpr uint8_t Rows = 2;

  bool probe(uint8_t address);
  bool writeExpander(uint8_t value);
  void pulseEnable(uint8_t value);
  void writeNibble(uint8_t value);
  void send(uint8_t value, bool data);
  void command(uint8_t value);
  void invalidateCache();
  void writeNormalized(uint8_t row, const char *normalized);

  char lineCache_[Rows][Columns + 1];
  uint32_t nextProbeAt_ = 0;
  uint8_t scanIndex_ = 0;
  uint8_t address_ = 0;
  bool scanComplete_ = false;
  bool initialized_ = false;
  bool backlight_ = true;
#endif
};

extern I2cLcd optionalLcd;
