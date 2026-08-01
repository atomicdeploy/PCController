#pragma once

#include <Arduino.h>

class PwmExpanderDriver {
public:
  explicit PwmExpanderDriver(uint8_t address) : address_(address) {}

  bool begin();
  bool setFrequency(uint16_t frequencyHz);
  uint8_t setPWM(uint8_t channel, uint16_t on, uint16_t off);

private:
  bool write8(uint8_t reg, uint8_t value);
  bool read8(uint8_t reg, uint8_t &value);

  uint8_t address_;
};
