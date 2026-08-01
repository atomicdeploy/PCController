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

RgbColor pixels[AddressableLeds::PixelCount];

} // namespace

namespace AddressableLeds {

void begin() {
  pinMode(BoardPins::AddressableLed, OUTPUT);
  digitalWrite(BoardPins::AddressableLed, LOW);
  clear();
  show();
}

void clear() { fill(RgbColor()); }

void show() {
#if defined(__AVR__) && F_CPU >= 15400000UL && F_CPU <= 19000000UL
  // Fixed D6/PD6 800 kHz sender adapted from Adafruit_NeoPixel's 20-cycle
  // AVR timing loop (LGPL-3.0-or-later). Keeping the buffer here avoids the
  // generic NeoPixel heap allocation and preserves scarce AVR flash/RAM.
  static_assert(BoardPins::AddressableLed == 6,
                "Addressable LED sender expects Arduino D6/PD6");
  uint8_t encoded[PixelCount * 3];
  uint8_t out = 0;
  for (uint8_t index = 0; index < PixelCount; ++index) {
#if PCCONTROLLER_USE_WS2812B
    encoded[out++] = pixels[index].green;
    encoded[out++] = pixels[index].red;
    encoded[out++] = pixels[index].blue;
#else
    encoded[out++] = pixels[index].blue;
    encoded[out++] = pixels[index].red;
    encoded[out++] = pixels[index].green;
#endif
  }

  volatile uint8_t *port = &PORTD;
  const uint8_t pinMask = _BV(PORTD6);
  const uint8_t hi = static_cast<uint8_t>(*port | pinMask);
  const uint8_t lo = static_cast<uint8_t>(*port & ~pinMask);
  const uint8_t *ptr = encoded;
  uint16_t count = sizeof(encoded);
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
  if (index >= PixelCount) {
    return false;
  }

  pixels[index] = color;
  return true;
}

void fill(const RgbColor &color) {
  for (uint8_t index = 0; index < PixelCount; ++index) {
    pixels[index] = color;
  }
}

void brightness(uint8_t value) {
  (void)value;
}

uint8_t brightness() { return 255; }

void setBrightness(uint8_t value) { brightness(value); }

RgbColor *buffer() { return pixels; }

uint8_t count() { return PixelCount; }

} // namespace AddressableLeds

void setupWS2811() { AddressableLeds::begin(); }
