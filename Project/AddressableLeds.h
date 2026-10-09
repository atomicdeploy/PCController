#pragma once

#include <Arduino.h>

// RgbColor stores one unscaled addressable-LED pixel in RGB byte order.
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

// Number of addressable status pixels wired to the controller strip.
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
