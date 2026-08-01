#pragma once

#include <Arduino.h>

struct Ina219Reading {
  int32_t supplyMilliVolts;
  int32_t busMilliVolts;
  int32_t currentMilliAmps;
  int32_t powerMilliWatts;
};

class Ina219Sensor {
public:
  explicit Ina219Sensor(uint8_t address) : address_(address) {}

  bool begin();
  bool read(Ina219Reading &reading);

private:
  bool write16(uint8_t reg, uint16_t value);
  bool read16(uint8_t reg, uint16_t &value);

  uint8_t address_;
};
