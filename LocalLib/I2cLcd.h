#pragma once

#include <Arduino.h>

#include "../ProjectConfig.h"

// Lightweight HD44780 + PCF8574 backpack driver. Discovery is intentionally
// bounded to the common 0x27 and 0x3F addresses, one probe per service call.
class I2cLcd {
public:
  // Starts a bounded scan of the supported backpack addresses.
  void begin(uint32_t now = millis());
  // Probes at most one candidate per call; call periodically until complete.
  void serviceDiscovery(uint32_t now = millis());
  // Forgets discovery, initialization, and cached rows, then starts over.
  void rescan(uint32_t now = millis());

  // True once an address probe succeeds; the LCD may not be initialized yet.
  bool detected() const;
  // Initializes the detected backpack and reports whether it became usable.
  bool initialize();
  // True only after a backpack was detected and initialized successfully.
  bool available() const;
  // True once a candidate was found or all supported addresses were tried.
  bool scanComplete() const;
  // Returns the detected 7-bit address, or zero when no candidate was found.
  uint8_t address() const;

  // Clears both HD44780 rows and the matching RAM cache.
  void clear();
  // Reads at most one 16-column row from NUL-terminated RAM text.
  void showLine(uint8_t row, const char *text);
  // Reads at most one 16-column row from a standalone NUL-terminated flash
  // string (or another flash region that is safe to read for 16 bytes). Never
  // pass commonText() here: its packed four-byte cells are not terminated.
  void showLine(uint8_t row, const __FlashStringHelper *text);
  // Bounded host bridge: up to 32 bytes are split across the two 16-column
  // rows and cached, so no periodic redraw or heap-backed String is needed.
  void showText(const uint8_t *text, uint8_t length);
  // Updates the backpack output while retaining the requested state in RAM.
  void setBacklight(bool enabled);
  // Copies the cached two-row, 32-character image to caller-owned RAM.
  void copyText(uint8_t *destination) const;
  // Returns the requested cached backlight state.
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

// optionalLcd is the board-wide host-assisted LCD bridge.
extern I2cLcd optionalLcd;
