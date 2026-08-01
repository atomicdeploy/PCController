#pragma once

#include <Arduino.h>

// Fixed-size AVR TWI master used by every on-board and host-proxied I2C
// transaction. The public bridge is deliberately capped at 16 bytes, so the
// generic Wire slave callbacks and duplicate 32-byte queues are unnecessary.
class CompactI2c {
public:
  static constexpr uint8_t BufferSize = 16;

  void begin();
  void setWireTimeout(uint32_t timeoutMicros, bool resetOnTimeout = true);

  void beginTransmission(uint8_t address);
  size_t write(uint8_t value);
  size_t write(const uint8_t *data, size_t length);
  uint8_t endTransmission(bool sendStop = true);

  uint8_t requestFrom(uint8_t address, uint8_t length);
  int available() const;
  int read();

private:
#if defined(__AVR__)
  bool waitForInterrupt();
  bool start(uint8_t address, bool read);
  bool writeByte(uint8_t value);
  void stop();
  void resetPeripheral();
#endif

  uint8_t error_ = 0;
  uint8_t rxLength_ = 0;
  uint8_t rxIndex_ = 0;
  uint8_t rx_[BufferSize]{};
  uint16_t timeoutMicros_ = 25000;
  bool resetOnTimeout_ = true;
};

extern CompactI2c i2cBus;
