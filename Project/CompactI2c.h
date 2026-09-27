#pragma once

#include <Arduino.h>

// Fixed-size AVR TWI master used by every on-board and host-proxied I2C
// transaction. The public bridge is deliberately capped at 16 bytes, so the
// generic Wire slave callbacks and duplicate 32-byte queues are unnecessary.
class CompactI2c {
public:
  static constexpr uint8_t BufferSize = 16;

  // Enables the AVR TWI peripheral as an I2C master.
  void begin();
  // Bounds bus waits; resetOnTimeout also recovers the peripheral on expiry.
  void setWireTimeout(uint32_t timeoutMicros, bool resetOnTimeout = true);

  // Starts a Wire-style write transaction for the supplied 7-bit address.
  void beginTransmission(uint8_t address);
  // Writes one byte into the active transaction and reports bytes accepted.
  size_t write(uint8_t value);
  // Bulk writes from ordinary SRAM. PROGMEM callers must read each byte with
  // pgm_read_byte() and use the single-byte overload, or add a flash-aware API.
  size_t write(const uint8_t *data, size_t length);
  // Ends the active transaction and returns its Wire-style error code.
  uint8_t endTransmission(bool sendStop = true);

  // Reads up to BufferSize bytes and returns the number actually received.
  uint8_t requestFrom(uint8_t address, uint8_t length);
  // Returns the unread receive-buffer byte count.
  int available() const;
  // Returns the next received byte, or -1 when none remains.
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
  uint16_t timeoutMicros_ = 0;
  bool resetOnTimeout_ = false;
};

// i2cBus is the single board-wide bounded TWI master.
extern CompactI2c i2cBus;
