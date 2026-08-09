#pragma once

#include <Arduino.h>

#include "../ProjectConfig.h"

class PwmController;

// IlluminationMode selects forced-off, forced-on, or door-driven brightness.
enum class IlluminationMode : uint8_t {
  Off = 0,
  Auto,
  On,
};

// Drives enclosure PWM toward Off/Auto/On targets with a nonblocking fade.
#if PCCONTROLLER_ENABLE_PCA9685 && \
    PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION
class IlluminationController {
public:
  // Starts from the electrically observed door target without a startup jump.
  void begin(PwmController &pwm, bool doorOpen, uint32_t now = millis());
  // Advances at most one fade step; allowLedUpdate pauses around host I2C leases.
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
  // Brightness fields are 0..255 and are expanded by PwmController.
  IlluminationMode mode_ = IlluminationMode::Auto;
  uint8_t onBrightness_ = 128;
  uint8_t offBrightness_ = 0;
  uint8_t currentBrightness_ = 0;
  uint32_t lastFadeAt_ = 0;
  PwmController *pwm_ = nullptr;
  bool initialized_ = false;
};
#else
class IlluminationController {
public:
  void begin(PwmController &, bool doorOpen, uint32_t = millis()) {
    currentBrightness_ = targetBrightness(doorOpen);
  }
  void service(bool, bool, uint32_t = millis()) {}
  void setMode(IlluminationMode mode) { mode_ = mode; }
  IlluminationMode mode() const { return mode_; }
  void setOnBrightness(uint8_t value) { onBrightness_ = value; }
  void setOffBrightness(uint8_t value) { offBrightness_ = value; }
  uint8_t onBrightness() const { return onBrightness_; }
  uint8_t offBrightness() const { return offBrightness_; }
  uint8_t currentBrightness() const { return currentBrightness_; }
  uint8_t targetBrightness(bool doorOpen) const {
    return mode_ == IlluminationMode::On
               ? onBrightness_
               : (mode_ == IlluminationMode::Auto && doorOpen
                      ? onBrightness_
                      : offBrightness_);
  }

private:
  IlluminationMode mode_ = IlluminationMode::Auto;
  uint8_t onBrightness_ = 128;
  uint8_t offBrightness_ = 0;
  uint8_t currentBrightness_ = 0;
};
#endif

// illumination is the single enclosure-light transition controller.
extern IlluminationController illumination;
