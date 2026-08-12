#pragma once

#include <Arduino.h>

#include "../ProjectConfig.h"

class PwmController;

// Persistent operational states. Custom means the last host descriptor owns
// the output; all other states are rendered locally, including host loss.
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

// Local cue IDs remain source-compatible with the front panel. The production
// engine deliberately does not let informational cues interrupt an effect.
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

// Values are the native STATUS_EFFECT wire contract.
enum class StatusLedEffect : uint8_t {
  None = 0,
  Breathe = 1,
  Flash = 2,
  Cycle = 3,
  Transition = 4,
};

namespace StatusLedTiming {
// 62.5 render opportunities per second. The engine uses elapsed MCU millis,
// never UART arrival cadence, and performs at most one PCA update per loop.
constexpr uint8_t FrameIntervalMs = 16;
constexpr uint16_t MinimumPeriodMs = 640;
constexpr uint16_t MaximumPeriodMs = 60000;
} // namespace StatusLedTiming

#if PCCONTROLLER_ENABLE_PCA9685 && PCCONTROLLER_ENABLE_STATUS_LED_ENGINE
// Allocation-free descriptor renderer for PCA9685 RGB channels 13..15.
class StatusLedController {
public:
  void begin(PwmController &pwm, uint8_t brightness,
             uint32_t now = millis(), bool powerSignal = true);
  void service(uint32_t now = millis());

  void setMode(StatusLedMode mode, uint32_t now = millis());
  StatusLedMode mode() const { return activeMode_; }
  void setBrightness(uint8_t brightness);
  uint8_t brightness() const {
    return activeMode_ == StatusLedMode::Custom ? hostDescriptor_[7]
                                                : localBrightness_;
  }
  // STATUS_RGB is four bytes; STATUS_EFFECT is the exact 12-byte descriptor.
  void setCustom(const uint8_t *payload, uint32_t now = millis());
  bool setEffect(const uint8_t *payload, uint32_t now = millis());
  void cancelEffect();
  StatusLedEffect effect() const {
    return activeMode_ == StatusLedMode::Custom
               ? static_cast<StatusLedEffect>(hostDescriptor_[0])
               : activeMode_ == StatusLedMode::Off ? StatusLedEffect::None
                                                   : StatusLedEffect::Breathe;
  }
  // Transient front-panel decoration is intentionally omitted from the byte-
  // tight engine. Safety/operational modes and host descriptors remain native.
  void playCue(StatusLedCue, uint16_t, uint32_t = millis()) {}

#if PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS
  uint8_t renderedRed() const { return renderedRed_; }
  uint8_t renderedGreen() const { return renderedGreen_; }
  uint8_t renderedBlue() const { return renderedBlue_; }
  uint8_t condition() const {
    return activeMode_ == StatusLedMode::Custom
               ? 0xFF
               : static_cast<uint8_t>(activeMode_);
  }
#endif
#if defined(PCCONTROLLER_NATIVE_TEST)
  uint16_t renderedFrames() const { return renderedFrames_; }
  const uint8_t *descriptorForTest() const { return hostDescriptor_; }
  uint16_t effectElapsedForTest() const { return effectElapsedMs_; }
  uint8_t localMinimumForTest() const {
    return static_cast<uint8_t>(localBrightness_ >> 4);
  }
#endif

private:
  void render();
  static uint8_t scale(uint8_t value, uint8_t level);
  static uint8_t interpolate(uint8_t from, uint8_t to, uint8_t phase);

  // Static singleton storage is zero-initialized; no constructor copy table.
  uint32_t lastFrameAt_;
  uint16_t effectElapsedMs_;
  uint16_t effectPeriodMs_;
  uint8_t hostDescriptor_[12];
  StatusLedMode activeMode_;
  uint8_t localBrightness_;
  bool dirty_;
#if PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS
  uint8_t renderedRed_;
  uint8_t renderedGreen_;
  uint8_t renderedBlue_;
#endif
#if defined(PCCONTROLLER_NATIVE_TEST)
  uint16_t renderedFrames_;
#endif
};
#else
class StatusLedController {
public:
  void begin(PwmController &, uint8_t, uint32_t = millis(), bool = true) {}
  void service(uint32_t = millis()) {}
  void setMode(StatusLedMode, uint32_t = millis()) {}
  StatusLedMode mode() const { return StatusLedMode::Off; }
  void setBrightness(uint8_t) {}
  uint8_t brightness() const { return 0; }
  void setCustom(const uint8_t *, uint32_t = millis()) {}
  bool setEffect(const uint8_t *, uint32_t = millis()) { return false; }
  void cancelEffect() {}
  StatusLedEffect effect() const { return StatusLedEffect::None; }
  void playCue(StatusLedCue, uint16_t, uint32_t = millis()) {}
};
#endif

extern StatusLedController statusLeds;
