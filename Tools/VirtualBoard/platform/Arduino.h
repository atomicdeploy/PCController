#pragma once

// Shipping OS adapter for the Arduino surface used by the production firmware.
// No opcodes, control policy, menu state or scheduling decisions belong here.
#include <array>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <deque>
#include <stdexcept>
#include <vector>

using std::size_t;
class __FlashStringHelper;
#define PROGMEM
#define _BV(bit) (1U << (bit))
#ifndef F_CPU
#define F_CPU 16000000UL
#endif
inline uint8_t pgm_read_byte(const void *p) { return *static_cast<const uint8_t *>(p); }
inline uint32_t pgm_read_dword(const void *p) { uint32_t v; std::memcpy(&v, p, 4); return v; }
constexpr uint8_t LOW=0, HIGH=1, INPUT=0, OUTPUT=1, INPUT_PULLUP=2, LSBFIRST=0, MSBFIRST=1;
constexpr uint8_t PIN_PB0=8, PIN_PB1=9, PIN_PB2=10, PIN_PB3=11, PIN_PB4=12, PIN_PB5=13;
constexpr uint8_t PIN_PC0=14, PIN_PC1=15, PIN_PC2=16, PIN_PC3=17, PIN_PC4=18, PIN_PC5=19;
constexpr uint8_t PIN_PD2=2, PIN_PD3=3, PIN_PD4=4, PIN_PD5=5, PIN_PD6=6, PIN_PD7=7;

namespace NativeHal {
struct ResetRequested {};
[[noreturn]] void requestReset();
uint64_t clockMicros();
void advance(uint64_t us);
void configureClock(bool deterministic);
uint8_t resetCause();
uint8_t i2cBegin(uint8_t address);
bool i2cWrite(uint8_t value);
uint8_t i2cEnd();
uint8_t i2cRead(uint8_t address, uint8_t *values, uint8_t length);
void showStrip(const uint8_t *values, uint16_t length);
inline std::array<uint8_t,64> modes{}, levels{};
inline std::array<uint8_t,8> portInputs{255,255,255,255,255,255,255,255}, portOutputs{}, portModes{};
inline uint8_t shiftInput=0xFF, shiftOutput=0xFF;
inline bool tm1637Present=true, pwmPresent=true;
inline uint16_t toneFrequency=0;
inline std::array<int16_t,2> temperature{INT16_MIN,INT16_MIN};
inline std::array<bool,2> temperatureConfigured{};
inline bool powerAvailable=false;
inline int32_t supplyMilliVolts=0, currentMilliAmps=0, powerMilliWatts=0;
inline std::vector<uint8_t> strip;
struct RadioPacket { uint32_t code; uint16_t pulse; uint8_t bits, protocol; };
inline std::deque<RadioPacket> radioRx;
inline RadioPacket radioTx{};
}

inline uint32_t millis() { return static_cast<uint32_t>(NativeHal::clockMicros()/1000); }
inline uint32_t micros() { return static_cast<uint32_t>(NativeHal::clockMicros()); }
inline void delay(uint32_t ms) { NativeHal::advance(static_cast<uint64_t>(ms)*1000); }
inline void delayMicroseconds(uint32_t us) { NativeHal::advance(us); }
inline void pinMode(uint8_t pin, uint8_t mode) { NativeHal::modes.at(pin)=mode; }
inline void digitalWrite(uint8_t pin,uint8_t level) { NativeHal::levels.at(pin)=level; }
inline int digitalRead(uint8_t pin) {
  // Device-owned ACK slot, not a fabricated sensor measurement.
  if (pin==PIN_PB3 && NativeHal::modes.at(pin)==INPUT) return NativeHal::tm1637Present ? LOW : HIGH;
  return NativeHal::modes.at(pin)==OUTPUT ? NativeHal::levels.at(pin) : HIGH;
}
inline void shiftOut(uint8_t,uint8_t,uint8_t,uint8_t value) { NativeHal::shiftOutput=value; }
inline uint8_t shiftIn(uint8_t,uint8_t,uint8_t) { return NativeHal::shiftInput; }
inline uint8_t digitalPinToInterrupt(uint8_t pin) { return pin; }
inline uint8_t digitalPinToPort(uint8_t pin) { return pin/8; }
inline uint8_t digitalPinToBitMask(uint8_t pin) { return static_cast<uint8_t>(_BV(pin%8)); }
inline volatile uint8_t *portOutputRegister(uint8_t port) { return &NativeHal::portOutputs.at(port); }
inline volatile uint8_t *portModeRegister(uint8_t port) { return &NativeHal::portModes.at(port); }
inline volatile uint8_t *portInputRegister(uint8_t port) { return &NativeHal::portInputs.at(port); }
inline void noInterrupts() {}
inline void interrupts() {}
inline void tone(uint8_t,uint16_t hz) { NativeHal::toneFrequency=hz; }
inline void noTone(uint8_t) { NativeHal::toneFrequency=0; }

class HardwareSerial {
public:
  static constexpr size_t Capacity=65536;
  void begin(uint32_t) {}
  int available() const { return static_cast<int>(rx_.size()>63 ? 63 : rx_.size()); }
  int read() { if(rx_.empty()) return -1; auto b=rx_.front(); rx_.pop_front(); return b; }
  size_t write(uint8_t v) { return write(&v,1); }
  size_t write(const uint8_t *v,size_t n) {
    if(n>Capacity-tx_.size()) throw std::runtime_error("native UART output backpressure");
    tx_.insert(tx_.end(),v,v+n); return n;
  }
  void feed(const uint8_t *v,size_t n) {
    if(n>Capacity-rx_.size()) throw std::runtime_error("native UART input backpressure");
    rx_.insert(rx_.end(),v,v+n);
  }
  std::vector<uint8_t> drain() { std::vector<uint8_t> v; v.swap(tx_); return v; }
  void disconnect() { rx_.clear(); tx_.clear(); }
  void flush() {}
private:
  std::deque<uint8_t> rx_;
  std::vector<uint8_t> tx_;
};
inline HardwareSerial Serial;
