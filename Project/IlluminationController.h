#pragma once

#include <Arduino.h>

class PwmController;

enum class IlluminationMode : uint8_t {
  Off = 0,
  Auto,
  On,
};

class IlluminationController {
public:
  void begin(PwmController &pwm, bool doorOpen, uint32_t now = millis());
  void service(bool doorOpen, bool allowLedUpdate,
               uint32_t now = millis());

  void setMode(IlluminationMode mode);
  IlluminationMode mode() const;

  void setOnBrightness(uint8_t brightness);
  void setOffBrightness(uint8_t brightness);
  uint8_t onBrightness() const;
  uint8_t offBrightness() const;
  uint8_t currentBrightness() const;
  uint8_t targetBrightness(bool doorOpen) const;

private:
  IlluminationMode mode_ = IlluminationMode::Auto;
  uint8_t onBrightness_ = 128;
  uint8_t offBrightness_ = 0;
  uint8_t currentBrightness_ = 0;
  uint32_t lastFadeAt_ = 0;
  PwmController *pwm_ = nullptr;
  bool initialized_ = false;
};

extern IlluminationController illumination;
