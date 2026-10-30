// SPDX-FileCopyrightText: 2026 PCController contributors
// SPDX-License-Identifier: MIT
#include <FastLED.h>

constexpr uint8_t DataPin = PIN_PD6; // Arduino D6, PCController strip data
constexpr uint8_t PixelCount = 100;
CRGB pixels[PixelCount];
uint8_t hue = 0;
uint32_t frames = 0;
uint32_t lastReport = 0;

void setup() {
  // Disable the PCController's unrelated active-low shift-register outputs.
  digitalWrite(PIN_PB0, HIGH);
  pinMode(PIN_PB0, OUTPUT);
  Serial.begin(115200);
  FastLED.addLeds<WS2811, DataPin, BRG>(pixels, PixelCount);
  FastLED.setBrightness(64);
  FastLED.clear(true);
  Serial.println(F("FastLED rainbow: D6/PD6, WS2811, BRG, 100 pixels, brightness 64"));
}

void loop() {
  fill_rainbow(pixels, PixelCount, hue++, 7);
  FastLED.show();
  ++frames;
  if (millis() - lastReport >= 2000) {
    lastReport = millis();
    Serial.print(F("rainbow frames="));
    Serial.println(frames);
  }
  delay(20);
}
