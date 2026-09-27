#include "CompactI2c.h"

#if defined(__AVR__)
#include <avr/io.h>
#include <util/twi.h>
#endif

CompactI2c i2cBus;

void CompactI2c::begin() {
#if defined(__AVR__)
  // Prescaler 1 and TWBR 72 produce 100 kHz SCL at the fixed 16 MHz clock.
  TWSR = 0;
  TWBR = static_cast<uint8_t>(((F_CPU / 100000UL) - 16UL) / 2UL);
  TWCR = _BV(TWEN);
#endif
  error_ = 0;
  rxLength_ = 0;
  rxIndex_ = 0;
  timeoutMicros_ = 25000;
  resetOnTimeout_ = true;
}

void CompactI2c::setWireTimeout(uint32_t timeoutMicros,
                                bool resetOnTimeout) {
  timeoutMicros_ = static_cast<uint16_t>(
      timeoutMicros > 65535UL ? 65535UL : timeoutMicros);
  resetOnTimeout_ = resetOnTimeout;
}

void CompactI2c::beginTransmission(uint8_t address) {
  error_ = 0;
#if defined(__AVR__)
  if (!start(address, false)) {
    error_ = 2;
  }
#else
  (void)address;
  error_ = 4;
#endif
}

size_t CompactI2c::write(uint8_t value) {
#if defined(__AVR__)
  if (error_ == 0 && !writeByte(value)) {
    error_ = 3;
  }
#else
  (void)value;
#endif
  return error_ == 0 ? 1 : 0;
}

size_t CompactI2c::write(const uint8_t *data, size_t length) {
  size_t written = 0;
  while (written < length && write(data[written]) != 0) {
    ++written;
  }
  return written;
}

uint8_t CompactI2c::endTransmission(bool sendStop) {
#if defined(__AVR__)
  // A failed no-STOP write cannot be followed by a valid repeated START.
  if (sendStop || error_ != 0) {
    stop();
  }
#else
  (void)sendStop;
#endif
  return error_;
}

uint8_t CompactI2c::requestFrom(uint8_t address, uint8_t length) {
  rxLength_ = 0;
  rxIndex_ = 0;
  if (length > BufferSize) {
    length = BufferSize;
  }
#if defined(__AVR__)
  if (length == 0 || !start(address, true)) {
    stop();
    return 0;
  }
  while (rxLength_ < length) {
    const bool acknowledge = rxLength_ + 1U < length;
    TWCR = static_cast<uint8_t>(_BV(TWINT) | _BV(TWEN) |
                                (acknowledge ? _BV(TWEA) : 0));
    if (!waitForInterrupt()) {
      break;
    }
    const uint8_t status = TW_STATUS;
    if (status != (acknowledge ? TW_MR_DATA_ACK : TW_MR_DATA_NACK)) {
      break;
    }
    rx_[rxLength_++] = TWDR;
  }
  stop();
#else
  (void)address;
#endif
  return rxLength_;
}

int CompactI2c::available() const {
  return static_cast<int>(rxLength_ - rxIndex_);
}

int CompactI2c::read() {
  return rxIndex_ < rxLength_ ? rx_[rxIndex_++] : -1;
}

#if defined(__AVR__)
bool CompactI2c::waitForInterrupt() {
  const uint32_t startedAt = micros();
  while ((TWCR & _BV(TWINT)) == 0) {
    if (static_cast<uint32_t>(micros() - startedAt) >= timeoutMicros_) {
      if (resetOnTimeout_) {
        resetPeripheral();
      }
      return false;
    }
  }
  return true;
}

bool CompactI2c::start(uint8_t address, bool read) {
  TWCR = static_cast<uint8_t>(_BV(TWINT) | _BV(TWSTA) | _BV(TWEN));
  if (!waitForInterrupt() ||
      (TW_STATUS != TW_START && TW_STATUS != TW_REP_START)) {
    return false;
  }
  TWDR = static_cast<uint8_t>((address << 1) | (read ? 1U : 0U));
  TWCR = static_cast<uint8_t>(_BV(TWINT) | _BV(TWEN));
  if (!waitForInterrupt()) {
    return false;
  }
  return TW_STATUS == (read ? TW_MR_SLA_ACK : TW_MT_SLA_ACK);
}

bool CompactI2c::writeByte(uint8_t value) {
  TWDR = value;
  TWCR = static_cast<uint8_t>(_BV(TWINT) | _BV(TWEN));
  return waitForInterrupt() && TW_STATUS == TW_MT_DATA_ACK;
}

void CompactI2c::stop() {
  TWCR = static_cast<uint8_t>(_BV(TWINT) | _BV(TWEN) | _BV(TWSTO));
  uint16_t remaining = 60000;
  while ((TWCR & _BV(TWSTO)) != 0 && --remaining != 0) {
  }
  if (remaining == 0 && resetOnTimeout_) {
    resetPeripheral();
  }
}

void CompactI2c::resetPeripheral() {
  TWCR = 0;
  TWCR = _BV(TWEN);
}
#endif
