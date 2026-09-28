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
  void setCustom(uint8_t red, uint8_t green, uint8_t blue,
                 uint8_t brightness, uint32_t now = millis());
  // Atomically owns a complete STATUS_EFFECT descriptor. Exact repeats retain
  // phase; changed descriptors replace in place without an owner-release gap.
  bool setEffect(const uint8_t *payload, uint32_t now = millis());
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
  bool persistentPriorityActive() const;
  static bool validProfile(const uint8_t *payload);
  void renderColor(uint8_t red, uint8_t green, uint8_t blue, uint8_t level);
  void renderEffect();
  void finishEffect();
  // Static storage zero-initializes the singleton. Avoiding per-member dynamic
  // initializers saves both flash copy data and constructor code on ATmega328P.
  PwmController *pwm_; // Non-owning shared PWM controller.
  StatusLedMode mode_;
  uint8_t fallbackBrightness_; // Stable setting, not descriptor-local.
  uint8_t effectPhase_;
  uint8_t renderedRed_;
  uint8_t renderedGreen_;
  uint8_t renderedBlue_;
  uint8_t condition_;
  uint16_t effectPeriodMs_; // Full duration of one 64-phase cycle.
  uint32_t effectCycleStartedAt_;
  uint32_t cueEndsAt_; // millis() deadline; zero means no active cue.
  StatusLedCue cue_;
  // Current rendered descriptor; repeats may count down without changing the
  // separately retained owner request.
  uint8_t active_[ProfilePayloadBytes];
  // The acknowledged manual owner survives temporary cue/safety rendering.
  // Keeping the exact descriptor also makes idempotence byte-exact.
  uint8_t requested_[ProfilePayloadBytes];
};

// statusLeds is the board-wide RGB state and cue compositor.
extern StatusLedController statusLeds;
