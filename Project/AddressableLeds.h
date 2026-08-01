#pragma once

#include <Arduino.h>

struct RgbColor {
  uint8_t red;
  uint8_t green;
  uint8_t blue;

  constexpr RgbColor(uint8_t redValue = 0, uint8_t greenValue = 0,
                     uint8_t blueValue = 0)
      : red(redValue), green(greenValue), blue(blueValue) {}
};

using CRGB = RgbColor;

namespace AddressableLeds {

constexpr uint8_t PixelCount = 11;

// Initialize the configured strip at full brightness, clear its RAM buffer,
// and send the cleared frame to the LEDs.
void begin();

// Buffer operations are intentionally separate from show(), so callers can
// compose a complete frame before sending it.
void clear();
void show();
bool setPixel(uint8_t index, const RgbColor &color);
void fill(const RgbColor &color);
void brightness(uint8_t value);
uint8_t brightness();
void setBrightness(uint8_t brightness);

RgbColor *buffer();
uint8_t count();

} // namespace AddressableLeds

// Compatibility entry point used by the inherited project layer.
void setupWS2811();
