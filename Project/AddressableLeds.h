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
constexpr uint8_t PixelCount = 100;

// Initialize the configured strip at full brightness, clear its RAM buffer,
// and send the cleared frame to the LEDs.
// Shared workspace must be claimed from MacroQueue before strip operations.
void bindWorkspace(uint8_t *workspace);
void begin();

// Buffer operations are intentionally separate from show(), so callers can
// compose a complete frame before sending it.
void clear();
void show();
bool setPixel(uint8_t index, const RgbColor &color);
bool stagePixels(uint8_t index, const uint8_t *rgb, uint8_t length);
bool apply(const uint8_t *payload, uint8_t length) __attribute__((noinline));
void fill(const RgbColor &color);
void brightness(uint8_t value);
uint8_t brightness();
void setBrightness(uint8_t brightness);

bool configure(uint8_t count);
uint8_t count();

} // namespace AddressableLeds
