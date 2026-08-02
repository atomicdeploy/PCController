#pragma once

#include <cstddef>
#include <cstdint>
#include <deque>
#include <vector>

using std::size_t;

class __FlashStringHelper;

inline std::uint8_t pgm_read_byte(const void *address) {
  return *static_cast<const std::uint8_t *>(address);
}

#ifndef _BV
#define _BV(bit) (1U << (bit))
#endif

#ifndef F_CPU
#define F_CPU 16000000UL
#endif

constexpr std::uint8_t LOW = 0;
constexpr std::uint8_t HIGH = 1;
constexpr std::uint8_t INPUT = 0;
constexpr std::uint8_t OUTPUT = 1;
constexpr std::uint8_t INPUT_PULLUP = 2;
constexpr std::uint8_t LSBFIRST = 0;
constexpr std::uint8_t MSBFIRST = 1;

// MiniCore physical-port aliases only need stable distinct values in tests.
constexpr std::uint8_t PIN_PB0 = 8;
constexpr std::uint8_t PIN_PB1 = 9;
constexpr std::uint8_t PIN_PB2 = 10;
constexpr std::uint8_t PIN_PB3 = 11;
constexpr std::uint8_t PIN_PB4 = 12;
constexpr std::uint8_t PIN_PB5 = 13;
constexpr std::uint8_t PIN_PC0 = 14;
constexpr std::uint8_t PIN_PC1 = 15;
constexpr std::uint8_t PIN_PC2 = 16;
constexpr std::uint8_t PIN_PC3 = 17;
constexpr std::uint8_t PIN_PC4 = 18;
constexpr std::uint8_t PIN_PC5 = 19;
constexpr std::uint8_t PIN_PD2 = 2;
constexpr std::uint8_t PIN_PD3 = 3;
constexpr std::uint8_t PIN_PD4 = 4;
constexpr std::uint8_t PIN_PD5 = 5;
constexpr std::uint8_t PIN_PD6 = 6;
constexpr std::uint8_t PIN_PD7 = 7;

namespace arduino_mock {
inline std::uint32_t nowMillis = 0;
inline std::uint32_t nowMicros = 1000;
inline std::uint8_t shiftInput = 0xFF;
inline std::uint8_t portOutput = 0;
inline std::uint8_t portMode = 0;
inline std::uint8_t portInput = 0xFF;

inline void resetHardware() {
  nowMillis = 0;
  nowMicros = 1000;
  shiftInput = 0xFF;
  portOutput = 0;
  portMode = 0;
  portInput = 0xFF;
}
} // namespace arduino_mock

inline std::uint32_t millis() { return arduino_mock::nowMillis; }
inline void delay(std::uint32_t value) { arduino_mock::nowMillis += value; }
inline void delayMicroseconds(std::uint32_t value) {
  arduino_mock::nowMicros += value;
}
inline void pinMode(std::uint8_t, std::uint8_t) {}
inline void digitalWrite(std::uint8_t, std::uint8_t) {}
inline int digitalRead(std::uint8_t) {
  return arduino_mock::portInput != 0 ? HIGH : LOW;
}
inline void shiftOut(std::uint8_t, std::uint8_t, std::uint8_t,
                     std::uint8_t) {}
inline std::uint8_t shiftIn(std::uint8_t, std::uint8_t, std::uint8_t) {
  return arduino_mock::shiftInput;
}
inline std::uint8_t digitalPinToInterrupt(std::uint8_t pin) { return pin; }
inline std::uint8_t digitalPinToPort(std::uint8_t) { return 1; }
inline std::uint8_t digitalPinToBitMask(std::uint8_t) { return 1; }
inline volatile std::uint8_t *portOutputRegister(std::uint8_t) {
  return &arduino_mock::portOutput;
}
inline volatile std::uint8_t *portModeRegister(std::uint8_t) {
  return &arduino_mock::portMode;
}
inline volatile std::uint8_t *portInputRegister(std::uint8_t) {
  return &arduino_mock::portInput;
}
inline void noInterrupts() {}
inline void interrupts() {}

// Minimal serial double used to compile the production AVR framing code on
// the host. It deliberately models only the HardwareSerial surface consumed
// by UartProtocol.
class HardwareSerial {
public:
  void begin(std::uint32_t baud) { baud_ = baud; }

  int available() const { return static_cast<int>(rx_.size()); }

  int read() {
    if (rx_.empty()) {
      return -1;
    }
    const auto value = rx_.front();
    rx_.pop_front();
    return value;
  }

  size_t write(std::uint8_t value) {
    tx_.push_back(value);
    return 1;
  }

  size_t write(const std::uint8_t *values, size_t length) {
    tx_.insert(tx_.end(), values, values + length);
    return length;
  }

  void feed(const std::vector<std::uint8_t> &values) {
    rx_.insert(rx_.end(), values.begin(), values.end());
  }

  std::uint32_t baud() const { return baud_; }
  const std::vector<std::uint8_t> &written() const { return tx_; }
  void clearWritten() { tx_.clear(); }

private:
  std::deque<std::uint8_t> rx_;
  std::vector<std::uint8_t> tx_;
  std::uint32_t baud_ = 0;
};

inline std::uint32_t micros() {
  return ++arduino_mock::nowMicros;
}
