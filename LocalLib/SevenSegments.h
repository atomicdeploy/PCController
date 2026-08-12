#pragma once

#include <Arduino.h>

// Compact fixed-pin TM1637 display with segment-level output caching and
// fixed-point rendering.
class SevenSegments {
public:
  void begin(uint8_t brightness = 5);
  void clear();
  // Reads at most four bytes from RAM; a terminator is optional within that
  // fixed display-cell bound.
  void showText(const char *text);
  // Reads at most four bytes from flash, so packed four-byte cells need not
  // carry individual NUL terminators.
  void showText(const __FlashStringHelper *text);
  void showInteger(int32_t value);
  // Avoids floating-point formatting when the caller already owns a scaled
  // integer: showFixed(1234, 2) renders "12.34".
  void showFixed(int32_t scaledValue, uint8_t decimalPlaces);

  void showUnavailable();
  void setBrightness(uint8_t brightness);
  // Moves one hardware brightness step toward the target at a quiet 70 ms cadence.
  void serviceBrightness(uint8_t target, uint32_t now = millis());
  const uint8_t *rawSegments() const { return cachedSegments_; }
  uint8_t brightness() const { return brightness_; }
#if defined(PCCONTROLLER_NATIVE_TEST)
  uint8_t lastCommandForTest() const { return lastCommand_; }
#endif

private:
  static uint8_t encodeCharacter(char value);

  void showScaled(int32_t value, uint8_t decimalPlaces);
  void commit(const uint8_t segments[4]);
  void sendCommand(uint8_t command);
  void writeSegments(const uint8_t segments[4]);

  uint8_t cachedSegments_[4] = {};
  uint8_t brightness_ = 0;
  uint16_t brightnessChangedAt_ = 0;
  bool begun_ = false;
#if defined(PCCONTROLLER_NATIVE_TEST)
  uint8_t lastCommand_ = 0;
#endif
};

// display is the single board-wide TM1637 presentation service.
extern SevenSegments display;
