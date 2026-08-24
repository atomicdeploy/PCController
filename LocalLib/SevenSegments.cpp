#include "SevenSegments.h"

#include "BoardPins.h"

#include <string.h>

#if defined(__AVR__)
#include <avr/io.h>
#include <util/delay.h>
#endif

namespace {

// TM1637 command bytes and segment masks used by the bit-banged transport.
constexpr uint8_t DataCommand = 0x40;
constexpr uint8_t AddressCommand = 0xC0;
constexpr uint8_t DisplayOffCommand = 0x80;
constexpr uint8_t DisplayOnCommand = 0x88;
constexpr uint8_t DecimalPoint = 0x80;
constexpr uint8_t MinusSegment = 0x40;

inline void busDelay() {
#if defined(__AVR__)
  _delay_us(3);
#else
  delayMicroseconds(3);
#endif
}

inline void clockLow() {
#if defined(__AVR__) && defined(PORTB) && defined(DDB5)
  PORTB &= static_cast<uint8_t>(~_BV(PORTB5));
  DDRB |= _BV(DDB5);
#else
  digitalWrite(BoardPins::Tm1637Clock, LOW);
#endif
}

inline void clockHigh() {
#if defined(__AVR__) && defined(PORTB) && defined(DDB5)
  PORTB |= _BV(PORTB5);
  DDRB |= _BV(DDB5);
#else
  digitalWrite(BoardPins::Tm1637Clock, HIGH);
#endif
}

inline void dataLow() {
#if defined(__AVR__) && defined(PORTB) && defined(DDB3)
  PORTB &= static_cast<uint8_t>(~_BV(PORTB3));
  DDRB |= _BV(DDB3);
#else
  pinMode(BoardPins::Tm1637Data, OUTPUT);
  digitalWrite(BoardPins::Tm1637Data, LOW);
#endif
}

inline void dataHigh() {
#if defined(__AVR__) && defined(PORTB) && defined(DDB3)
  PORTB |= _BV(PORTB3);
  DDRB |= _BV(DDB3);
#else
  pinMode(BoardPins::Tm1637Data, OUTPUT);
  digitalWrite(BoardPins::Tm1637Data, HIGH);
#endif
}

inline void releaseData() {
#if defined(__AVR__) && defined(PORTB) && defined(DDB3)
  PORTB &= static_cast<uint8_t>(~_BV(PORTB3));
  DDRB &= static_cast<uint8_t>(~_BV(DDB3));
#else
  pinMode(BoardPins::Tm1637Data, INPUT);
  digitalWrite(BoardPins::Tm1637Data, LOW);
#endif
}

void startBus() {
  clockHigh();
  dataHigh();
  busDelay();
  dataLow();
  busDelay();
  clockLow();
}

void stopBus() {
  clockLow();
  dataLow();
  busDelay();
  clockHigh();
  busDelay();
  dataHigh();
  busDelay();
}

void writeBusByte(uint8_t value) {
  for (uint8_t bit = 0; bit < 8; ++bit) {
    clockLow();
    (value & 0x01) != 0 ? dataHigh() : dataLow();
    busDelay();
    clockHigh();
    busDelay();
    value >>= 1;
  }

  // Release DIO while the TM1637 drives its ACK bit. An ACK is not required
  // for forward progress, but releasing the line prevents output contention.
  clockLow();
  releaseData();
  busDelay();
  clockHigh();
  busDelay();
  clockLow();
  dataLow();
}

} // namespace

SevenSegments display;

void SevenSegments::begin(uint8_t brightness) {
#if !defined(__AVR__)
  pinMode(BoardPins::Tm1637Clock, OUTPUT);
  pinMode(BoardPins::Tm1637Data, OUTPUT);
#endif
  clockHigh();
  dataHigh();
  begun_ = true;
  // One unequal cache byte forces clear() to write all four physical cells.
  cachedSegments_[0] = 0xFF;
  clear();
  // Zero is a valid display-off setting, so force the first command too.
  brightness_ = 0xFF;
  setBrightness(brightness);
}

void SevenSegments::clear() {
  const uint8_t segments[4] = {};
  commit(segments);
}

void SevenSegments::showText(const char *text) {
  uint8_t segments[4] = {};
  if (text != nullptr) {
    for (uint8_t index = 0; index < 4 && text[index] != '\0'; ++index) {
      segments[index] = encodeCharacter(text[index]);
    }
  }
  commit(segments);
}

void SevenSegments::showText(const __FlashStringHelper *text) {
  uint8_t segments[4] = {};
  const char *source = reinterpret_cast<const char *>(text);
  if (source != nullptr) {
    for (uint8_t index = 0; index < 4; ++index) {
      const char value = static_cast<char>(pgm_read_byte(source + index));
      if (value == '\0') {
        break;
      }
      segments[index] = encodeCharacter(value);
    }
  }
  commit(segments);
}

void SevenSegments::showInteger(int32_t value) { showScaled(value, 0); }

void SevenSegments::showFixed(int32_t scaledValue, uint8_t decimalPlaces) {
  showScaled(scaledValue, decimalPlaces);
}

void SevenSegments::showUnavailable() {
  const uint8_t segments[4] = {
      MinusSegment, MinusSegment, MinusSegment, MinusSegment};
  commit(segments);
}

void SevenSegments::setBrightness(uint8_t brightness) {
  if (brightness > 7) {
    brightness = 7;
  }
  if (brightness_ == brightness) {
    return;
  }
  brightness_ = brightness;
  ++revision_;
  if (begun_) {
    // Zero is a true display-off level; values 1..7 retain their prior TM1637
    // intensity mapping so existing nonzero EEPROM settings do not get dimmer.
    sendCommand(brightness_ == 0
                    ? DisplayOffCommand
                    : static_cast<uint8_t>(DisplayOnCommand | brightness_));
  }
}

void SevenSegments::serviceBrightness(uint8_t target, uint32_t now) {
  if (target > 7) {
    target = 7;
  }
  const uint16_t tick = static_cast<uint16_t>(now);
  if (brightness_ == target ||
      static_cast<uint16_t>(tick - brightnessChangedAt_) < 70U) {
    return;
  }
  brightnessChangedAt_ = tick;
  setBrightness(static_cast<uint8_t>(brightness_ +
                                     (brightness_ < target ? 1 : -1)));
}

uint8_t SevenSegments::encodeCharacter(char value) {
  switch (value) {
  case 'b':
    return 0x7C;
  case 'c':
    return 0x58;
  case 'd':
    return 0x5E;
  case 'h':
    return 0x74;
  case 'n':
    return 0x54;
  case 'o':
    return 0x5C;
  case 'r':
    return 0x50;
  case 't':
    return 0x78;
  case 'u':
    return 0x1C;
  default:
    break;
  }
  if (value >= 'a' && value <= 'z') {
    value = static_cast<char>(value - ('a' - 'A'));
  }

  switch (value) {
  case '0':
  case 'O':
    return 0x3F;
  case '1':
  case 'I':
    return 0x06;
  case '2':
  case 'Z':
    return 0x5B;
  case '3':
    return 0x4F;
  case '4':
    return 0x66;
  case '5':
  case 'S':
    return 0x6D;
  case '6':
  case 'G':
    return 0x7D;
  case '7':
    return 0x07;
  case '8':
    return 0x7F;
  case '9':
    return 0x6F;
  case 'A':
    return 0x77;
  case 'B':
    return 0x7C;
  case 'C':
    return 0x39;
  case 'D':
    return 0x5E;
  case 'E':
    return 0x79;
  case 'F':
    return 0x71;
  case 'H':
  case 'K':
  case 'X':
    return 0x76;
  case 'J':
    return 0x1E;
  case 'L':
    return 0x38;
  case 'M':
    return 0x37;
  case 'N':
    return 0x54;
  case 'P':
    return 0x73;
  case 'Q':
    return 0x67;
  case 'R':
    return 0x50;
  case 'T':
    return 0x78;
  case 'U':
  case 'V':
    return 0x3E;
  case 'Y':
    return 0x6E;
  case '-':
    return MinusSegment;
  default:
    return 0;
  }
}

void SevenSegments::showScaled(int32_t value, uint8_t decimalPlaces) {
  if (decimalPlaces > 2 || value == INT32_MIN) {
    showUnavailable();
    return;
  }

  const bool negative = value < 0;
  uint32_t magnitude =
      negative ? static_cast<uint32_t>(-value) : static_cast<uint32_t>(value);
  uint8_t segments[4] = {};
  int8_t position = 3;
  uint8_t digits = 0;
  do {
    if (position < 0) {
      showUnavailable();
      return;
    }
    segments[position--] = encodeCharacter(
        static_cast<char>('0' + static_cast<uint8_t>(magnitude % 10)));
    magnitude /= 10;
    ++digits;
  } while (magnitude != 0 || digits <= decimalPlaces);

  if (decimalPlaces != 0) {
    segments[3 - decimalPlaces] |= DecimalPoint;
  }
  if (negative) {
    if (position < 0) {
      showUnavailable();
      return;
    }
    segments[position] = MinusSegment;
  }
  commit(segments);
}

void SevenSegments::commit(const uint8_t segments[4]) {
  if (memcmp(cachedSegments_, segments, sizeof(cachedSegments_)) == 0) {
    return;
  }
  writeSegments(segments);
  memcpy(cachedSegments_, segments, sizeof(cachedSegments_));
  ++revision_;
}

void SevenSegments::sendCommand(uint8_t command) {
#if defined(PCCONTROLLER_NATIVE_TEST)
  lastCommand_ = command;
#endif
  startBus();
  writeBusByte(command);
  stopBus();
}

void SevenSegments::writeSegments(const uint8_t segments[4]) {
  if (!begun_) {
    return;
  }
  sendCommand(DataCommand);

  startBus();
  writeBusByte(AddressCommand);
  for (uint8_t index = 0; index < 4; ++index) {
    writeBusByte(segments[index]);
  }
  stopBus();
}
