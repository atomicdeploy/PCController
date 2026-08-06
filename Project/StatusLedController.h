#pragma once

#include <Arduino.h>

class PwmController;

// StatusLedMode selects the persistent operational RGB presentation.
enum class StatusLedMode : uint8_t {
  Off = 0,
  Boot,
  Ready,
  Learning,
  Warning,
  Fault,
  Custom,
  Connected,
  Disconnected,
  Waiting,
  Running,
};

// StatusLedCue selects a temporary informational or warning overlay.
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

// One compact procedural engine covers every host-controlled animation. The
// numeric values are the native STATUS_EFFECT payload contract.
enum class StatusLedEffect : uint8_t {
  None = 0,
  Breathe = 1,
  Flash = 2,
  Cycle = 3,
  Transition = 4,
};

// Composes base state and transient cues onto PWM RGB channels 13..15.
class StatusLedController {
public:
  static constexpr uint8_t ProfileCount = 19;
  static constexpr uint8_t ProfilePayloadBytes = 12;
  static constexpr uint8_t ManualCondition = 0xFF;
  // Claims PWM output plus Power/On signal and starts the boot animation.
  void begin(PwmController &pwm, uint8_t brightness,
             uint32_t now = millis(), bool powerSignal = true);
  // Advances breathing/easing without blocking other services.
  void service(uint32_t now = millis());

  void setMode(StatusLedMode mode, uint32_t now = millis());
  StatusLedMode mode() const;
  void setBrightness(uint8_t brightness);
  uint8_t brightness() const;
  void setCustom(uint8_t red, uint8_t green, uint8_t blue);
  bool setEffect(StatusLedEffect effect, uint8_t red, uint8_t green,
                 uint8_t blue, uint8_t alternateRed,
                 uint8_t alternateGreen, uint8_t alternateBlue,
                 uint8_t brightness, uint8_t minimumBrightness,
                 uint16_t periodMs, uint8_t repeats,
                 uint32_t now = millis());
  void cancelEffect();
  StatusLedEffect effect() const;
  uint8_t renderedRed() const;
  uint8_t renderedGreen() const;
  uint8_t renderedBlue() const;
  uint8_t condition() const;
  bool profile(uint8_t condition, uint8_t *payload) const;
  bool setProfile(uint8_t condition, const uint8_t *payload,
                  uint32_t now = millis());
  void setPowerSignal(bool active);
  // Overlays an informational transition before smoothly restoring base state.
  void playCue(StatusLedCue cue, uint16_t durationMs,
               uint32_t now = millis());

private:
  void loadProfile(uint8_t condition, uint32_t now);
  void defaultProfile(uint8_t condition, uint8_t *payload) const;
  void applyProfile(uint8_t condition, const uint8_t *payload, uint32_t now);
  static bool validProfile(const uint8_t *payload);
  void renderColor(uint8_t red, uint8_t green, uint8_t blue, uint8_t level);
  void renderEffect();
  void finishEffect();
  static uint8_t scale(uint8_t value, uint8_t level);
  static uint8_t interpolate(uint8_t from, uint8_t to, uint8_t phase);

  // Static storage zero-initializes the singleton. Avoiding per-member dynamic
  // initializers saves both flash copy data and constructor code on ATmega328P.
  PwmController *pwm_; // Non-owning shared PWM controller.
  StatusLedMode mode_;
  uint8_t brightness_;
  uint8_t customRed_;
  uint8_t customGreen_;
  uint8_t customBlue_;
  uint8_t alternateRed_;
  uint8_t alternateGreen_;
  uint8_t alternateBlue_;
  uint8_t minimumBrightness_;
  uint8_t effectPhase_;
  uint8_t effectRepeats_;
  uint8_t renderedRed_;
  uint8_t renderedGreen_;
  uint8_t renderedBlue_;
  uint8_t condition_;
  StatusLedEffect effect_;
  uint16_t effectStepMs_;
  uint32_t lastEffectStepAt_;
  uint32_t cueEndsAt_; // millis() deadline; zero means no active cue.
  StatusLedCue cue_;
};

// statusLeds is the board-wide RGB state and cue compositor.
extern StatusLedController statusLeds;
