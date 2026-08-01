#pragma once

#include <Arduino.h>

class PwmController;

enum class StatusLedMode : uint8_t {
  Off = 0,
  Boot,
  Ready,
  Learning,
  Warning,
  Fault,
  Custom,
};

enum class StatusLedCue : uint8_t {
  None = 0,
  DoorOpen,
  DoorClosed,
  Bluetooth,
  Menu,
  Radio,
  Save,
  Discard,
  Reset,
};

class StatusLedController {
public:
  void begin(PwmController &pwm, uint8_t brightness,
             uint32_t now = millis());
  void service(uint32_t now = millis());

  void setMode(StatusLedMode mode, uint32_t now = millis());
  StatusLedMode mode() const;
  void setBrightness(uint8_t brightness);
  uint8_t brightness() const;
  void setReadyColor(uint8_t color);
  void setCustom(uint8_t red, uint8_t green, uint8_t blue);
  void setPowerSignal(bool active);
  void playCue(StatusLedCue cue, uint16_t durationMs,
               uint32_t now = millis());

private:
  void render(uint8_t level, bool eased);
  static uint8_t scale(uint8_t value, uint8_t level);
  static uint16_t easedValue(uint16_t current, uint16_t target);

  PwmController *pwm_ = nullptr;
  StatusLedMode mode_ = StatusLedMode::Off;
  uint8_t brightness_ = 128;
  uint8_t readyPalette_ = 2;
  uint8_t customRed_ = 0;
  uint8_t customGreen_ = 0;
  uint8_t customBlue_ = 0;
  uint8_t pulse_ = 0;
  bool pulseRising_ = true;
  uint32_t lastStepAt_ = 0;
  uint32_t cueEndsAt_ = 0;
  StatusLedCue cue_ = StatusLedCue::None;
};

extern StatusLedController statusLeds;
