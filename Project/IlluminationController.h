#pragma once

#include <Arduino.h>

class PwmController;

// IlluminationMode selects forced-off, forced-on, or door-driven brightness.
enum class IlluminationMode : uint8_t {
  Off = 0,
  Auto,
  On,
};

// Drives enclosure PWM toward Off/Auto/On targets with a nonblocking fade.
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
  // Startup settings overwrite these zero-initialized fields before begin().
  // This keeps the whole static controller in BSS instead of flash-backed data.
  IlluminationMode mode_;
  uint8_t onBrightness_;
  uint8_t offBrightness_;
  uint8_t currentBrightness_;
  uint32_t lastFadeAt_;
  PwmController *pwm_;
  bool initialized_;
};

// illumination is the single enclosure-light transition controller.
extern IlluminationController illumination;
