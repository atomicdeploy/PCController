#include "Ina219Sensor.h"

#include "CompactI2c.h"

namespace {
constexpr uint8_t ConfigRegister = 0x00;
constexpr uint8_t ShuntRegister = 0x01;
constexpr uint8_t BusRegister = 0x02;
constexpr uint8_t PowerRegister = 0x03;
constexpr uint8_t CurrentRegister = 0x04;
constexpr uint8_t CalibrationRegister = 0x05;

// 32 V range, gain /8, 64x bus + 128x shunt averaging, continuous mode.
// A full conversion is ~102 ms; current gets the stronger filtering.
constexpr uint16_t Config32V2A = 0x3F7F;
// 0.1 mA current LSB with the common 0.1 ohm shunt.
constexpr uint16_t Calibration32V2A = 4096;
} // namespace

bool Ina219Sensor::begin() {
  i2cBus.beginTransmission(address_);
  if (i2cBus.endTransmission() != 0) {
    return false;
  }
  return write16(CalibrationRegister, Calibration32V2A) &&
         write16(ConfigRegister, Config32V2A);
}

bool Ina219Sensor::read(Ina219Reading &reading) {
  // Some compatible devices clear calibration after reset; refreshing it also
  // matches the established Adafruit 32V/2A measurement sequence.
  if (!write16(CalibrationRegister, Calibration32V2A)) {
    return false;
  }

  uint16_t busRaw;
  uint16_t shuntRaw;
  uint16_t currentRaw;
  uint16_t powerRaw;
  if (!read16(BusRegister, busRaw) || !read16(ShuntRegister, shuntRaw) ||
      !read16(CurrentRegister, currentRaw) ||
      !read16(PowerRegister, powerRaw)) {
    return false;
  }

  reading.busMilliVolts =
      static_cast<int32_t>(static_cast<uint16_t>(busRaw >> 3)) * 4L;
  // Shunt LSB is 10 uV: divide signed raw by 100 for millivolts.
  const int16_t signedShunt = static_cast<int16_t>(shuntRaw);
  const int32_t shuntMilliVolts =
      signedShunt >= 0 ? (signedShunt + 50L) / 100L
                       : (signedShunt - 50L) / 100L;
  reading.supplyMilliVolts =
      reading.busMilliVolts + shuntMilliVolts;

  // Current LSB is 0.1 mA and power LSB is 2 mW.
  const int16_t signedCurrent = static_cast<int16_t>(currentRaw);
  reading.currentMilliAmps =
      signedCurrent >= 0 ? (signedCurrent + 5L) / 10L
                         : (signedCurrent - 5L) / 10L;
  reading.powerMilliWatts = static_cast<uint32_t>(powerRaw) * 2UL;
  return true;
}

bool Ina219Sensor::write16(uint8_t reg, uint16_t value) {
  i2cBus.beginTransmission(address_);
  i2cBus.write(reg);
  i2cBus.write(static_cast<uint8_t>(value >> 8));
  i2cBus.write(static_cast<uint8_t>(value));
  return i2cBus.endTransmission() == 0;
}

bool Ina219Sensor::read16(uint8_t reg, uint16_t &value) {
  i2cBus.beginTransmission(address_);
  i2cBus.write(reg);
  if (i2cBus.endTransmission(false) != 0 ||
      i2cBus.requestFrom(address_, static_cast<uint8_t>(2)) != 2) {
    return false;
  }
  value = static_cast<uint16_t>(i2cBus.read()) << 8;
  value |= i2cBus.read();
  return true;
}
