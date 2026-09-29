// SPDX-FileCopyrightText: Adafruit Industries and Adafruit NeoPixel contributors
// SPDX-FileCopyrightText: 2026 David Refoua and PCController contributors
// SPDX-License-Identifier: LGPL-3.0-or-later

#include "AddressableLeds.h"

#include "../LocalLib/BoardPins.h"
#include "../ProjectConfig.h"

#if defined(__AVR__)
#include <avr/interrupt.h>
#include <avr/io.h>
#include <util/delay.h>
#endif

namespace {

// Wire-order storage avoids a second 300-byte frame on the AVR stack.
uint8_t *pixels = nullptr;
uint8_t pixelCount = AddressableLeds::PixelCount;

} // namespace

namespace AddressableLeds {

void bindWorkspace(uint8_t *workspace) { pixels = workspace; }

void begin() {
  pinMode(BoardPins::AddressableLed, OUTPUT);
  digitalWrite(BoardPins::AddressableLed, LOW);
  clear();
  show();
}

void clear() { fill(RgbColor()); }

void show() {
  if (!pixels) return;
#if defined(__AVR__) && F_CPU >= 15400000UL && F_CPU <= 19000000UL
  // Fixed D6/PD6 800 kHz sender adapted from Adafruit_NeoPixel's 20-cycle
  // AVR timing loop (LGPL-3.0-or-later). Keeping the buffer here avoids the
  // generic NeoPixel heap allocation and preserves scarce AVR flash/RAM.
  static_assert(BoardPins::AddressableLed == 6,
                "Addressable LED sender expects Arduino D6/PD6");
  volatile uint8_t *port = &PORTD;
  const uint8_t pinMask = _BV(PORTD6);
  const uint8_t hi = static_cast<uint8_t>(*port | pinMask);
  const uint8_t lo = static_cast<uint8_t>(*port & ~pinMask);
  const uint8_t *ptr = pixels;
  uint16_t count = static_cast<uint16_t>(pixelCount) * 3;
  uint8_t byte = *ptr++;
  uint8_t next = lo;
  uint8_t bit = 8;
  const uint8_t savedSreg = SREG;
  cli();
  asm volatile(
      "head20%=:" "\n\t"
      "st   %a[port],  %[hi]" "\n\t"
      "sbrc %[byte],  7" "\n\t"
      "mov  %[next], %[hi]" "\n\t"
      "dec  %[bit]" "\n\t"
      "st   %a[port],  %[next]" "\n\t"
      "mov  %[next],  %[lo]" "\n\t"
      "breq nextbyte20%=" "\n\t"
      "rol  %[byte]" "\n\t"
      "rjmp .+0" "\n\t"
      "nop" "\n\t"
      "st   %a[port],  %[lo]" "\n\t"
      "nop" "\n\t"
      "rjmp .+0" "\n\t"
      "rjmp head20%=" "\n\t"
      "nextbyte20%=:" "\n\t"
      "ldi  %[bit], 8" "\n\t"
      "ld   %[byte], %a[ptr]+" "\n\t"
      "st   %a[port], %[lo]" "\n\t"
      "nop" "\n\t"
      "sbiw %[count], 1" "\n\t"
      "brne head20%=" "\n"
      : [port] "+e"(port), [byte] "+r"(byte), [bit] "+r"(bit),
        [next] "+r"(next), [count] "+w"(count)
      : [ptr] "e"(ptr), [hi] "r"(hi), [lo] "r"(lo));
  SREG = savedSreg;
  _delay_us(80);
#else
  // The production target is the 16 MHz AVR above. A deliberately simple
  // fallback keeps the hardware-facing API buildable for native simulators.
  (void)pixels;
#endif
}

bool setPixel(uint8_t index, const RgbColor &color) {
  if (!pixels || index >= pixelCount) {
    return false;
  }

  const uint16_t offset = static_cast<uint16_t>(index) * 3;
#if PCCONTROLLER_USE_WS2812B
  pixels[offset] = color.green;
  pixels[offset + 2] = color.blue;
#else
  pixels[offset] = color.blue;
  pixels[offset + 2] = color.green;
#endif
  pixels[offset + 1] = color.red;
  return true;
}

bool stagePixels(uint8_t index, const uint8_t *rgb, uint8_t length) {
  // Walk triples instead of pulling AVR integer division/modulo into dispatch.
  uint8_t remainder = length;
  while (remainder >= 3) remainder -= 3;
  if (!pixels || remainder || index >= pixelCount ||
      length > static_cast<uint16_t>(pixelCount - index) * 3) return false;
  uint8_t *out = pixels + static_cast<uint16_t>(index) * 3;
  while (length) {
#if PCCONTROLLER_USE_WS2812B
    out[0] = rgb[1];
    out[2] = rgb[2];
#else
    out[0] = rgb[2];
    out[2] = rgb[1];
#endif
    out[1] = rgb[0];
    out += 3;
    rgb += 3;
    length -= 3;
  }
  return true;
}

void fill(const RgbColor &color) {
  for (uint8_t index = 0; index < pixelCount; ++index) {
    setPixel(index, color);
  }
}

void brightness(uint8_t value) {
  (void)value;
}

uint8_t brightness() { return 255; }

void setBrightness(uint8_t value) { brightness(value); }

bool configure(uint8_t count) {
  if (count == 0 || count > PixelCount) return false;
  clear();
  show(); // Clear the previous tail before shortening the configured strip.
  pixelCount = count;
  clear();
  return true;
}

uint8_t count() { return pixelCount; }

} // namespace AddressableLeds
