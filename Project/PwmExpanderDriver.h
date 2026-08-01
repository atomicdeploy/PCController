#pragma once

#include <Arduino.h>

// Minimal PCA9685 register driver; logical ownership remains in PwmController.
class PwmExpanderDriver {
public:
  explicit PwmExpanderDriver(uint8_t address) : address_(address) {}

  // Enables auto-increment and verifies the MODE1 register.
  bool begin();
  // Programs the common PWM prescaler using the requested frequency in hertz.
  bool setFrequency(uint16_t frequencyHz);
  // Writes one channel's 12-bit on/off counters; zero return means success.
  uint8_t setPWM(uint8_t channel, uint16_t on, uint16_t off);

private:
  bool write8(uint8_t reg, uint8_t value);
  bool read8(uint8_t reg, uint8_t &value);

  uint8_t address_; // Seven-bit I2C address; production hardware uses 0x41.
};
