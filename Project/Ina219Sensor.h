#pragma once

#include <Arduino.h>

// Ina219Reading stores one coherent sample in integer electrical units.
struct Ina219Reading {
  // Integer telemetry units avoid floating point on the ATmega328P.
  int32_t supplyMilliVolts;
  int32_t busMilliVolts;
  int32_t currentMilliAmps;
  int32_t powerMilliWatts;
};

// Fixed 32 V/2 A INA219 profile with calibrated integer reads at I2C 0x40.
class Ina219Sensor {
public:
  explicit Ina219Sensor(uint8_t address) : address_(address) {}

  // Writes the averaging/gain/calibration profile and verifies bus access.
  bool begin();
  // Reads one coherent bus, shunt-derived supply, current, and power sample.
  bool read(Ina219Reading &reading);

private:
  bool write16(uint8_t reg, uint16_t value);
  bool read16(uint8_t reg, uint16_t &value);

  uint8_t address_; // Seven-bit I2C address.
};
