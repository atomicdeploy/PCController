#include "PwmExpanderDriver.h"

#include "CompactI2c.h"

namespace {
constexpr uint8_t Mode1Register = 0x00;
constexpr uint8_t FirstChannelRegister = 0x06;
constexpr uint8_t PrescaleRegister = 0xFE;
constexpr uint8_t Mode1Restart = 0x80;
constexpr uint8_t Mode1AutoIncrement = 0x20;
constexpr uint8_t Mode1Sleep = 0x10;
} // namespace

bool PwmExpanderDriver::begin() {
  i2cBus.beginTransmission(address_);
  if (i2cBus.endTransmission() != 0) {
    return false;
  }
  if (!write8(Mode1Register, Mode1AutoIncrement)) {
    return false;
  }
  delay(1);
  return true;
}

bool PwmExpanderDriver::setFrequency(uint16_t frequencyHz) {
  if (frequencyHz < 24) {
    frequencyHz = 24;
  } else if (frequencyHz > 1526) {
    frequencyHz = 1526;
  }

  // Rounded 25 MHz / (4096 * frequency) - 1, kept integer-only.
  uint32_t denominator = static_cast<uint32_t>(frequencyHz) * 4096UL;
  uint16_t prescale =
      static_cast<uint16_t>((25000000UL + denominator / 2UL) / denominator);
  prescale = prescale == 0 ? 0 : static_cast<uint16_t>(prescale - 1);
  if (prescale < 3) {
    prescale = 3;
  } else if (prescale > 255) {
    prescale = 255;
  }

  uint8_t oldMode;
  if (!read8(Mode1Register, oldMode) ||
      !write8(Mode1Register,
              static_cast<uint8_t>((oldMode & 0x7F) | Mode1Sleep)) ||
      !write8(PrescaleRegister, static_cast<uint8_t>(prescale)) ||
      !write8(Mode1Register, oldMode)) {
    return false;
  }
  delayMicroseconds(500);
  return write8(Mode1Register,
                static_cast<uint8_t>(oldMode | Mode1Restart |
                                     Mode1AutoIncrement));
}

uint8_t PwmExpanderDriver::setPWM(uint8_t channel, uint16_t on,
                                  uint16_t off) {
  if (channel >= 16 || on > 4096 || off > 4096) {
    return 4;
  }
  i2cBus.beginTransmission(address_);
  i2cBus.write(static_cast<uint8_t>(FirstChannelRegister + channel * 4U));
  i2cBus.write(static_cast<uint8_t>(on));
  i2cBus.write(static_cast<uint8_t>(on >> 8));
  i2cBus.write(static_cast<uint8_t>(off));
  i2cBus.write(static_cast<uint8_t>(off >> 8));
  return i2cBus.endTransmission();
}

bool PwmExpanderDriver::write8(uint8_t reg, uint8_t value) {
  i2cBus.beginTransmission(address_);
  i2cBus.write(reg);
  i2cBus.write(value);
  return i2cBus.endTransmission() == 0;
}

bool PwmExpanderDriver::read8(uint8_t reg, uint8_t &value) {
  i2cBus.beginTransmission(address_);
  i2cBus.write(reg);
  if (i2cBus.endTransmission(false) != 0 ||
      i2cBus.requestFrom(address_, static_cast<uint8_t>(1)) != 1) {
    return false;
  }
  value = i2cBus.read();
  return true;
}
