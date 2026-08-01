#pragma once

#include <Arduino.h>

// Captures the hardware reset flags before normal C++ initialization and
// keeps a power-loss-safe, wear-levelled MCU-owned boot count in an EEPROM
// journal that is separate from controller settings and learned RF records.
class ResetTelemetry {
public:
  void begin();
  uint8_t cause() const;
  uint32_t count() const;

private:
  uint32_t count_ = 0;
  uint8_t cause_ = 0;
};

extern ResetTelemetry resetTelemetry;
