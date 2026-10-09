#include "StatusLedController.h"

#if PCCONTROLLER_ENABLE_PCA9685

#include <EEPROM.h>
#include <string.h>

#include "EepromLayout.h"
#include "PwmController.h"
#include "UartProtocol.h"

namespace {
// One cooperative cadence and the compact condition numbering shared with Go.
constexpr uint16_t MinimumEffectPeriodMs = 640;
constexpr uint8_t EffectPhaseStep = 8;
constexpr uint8_t StatusModePaletteCount = 11;

} // namespace

StatusLedController statusLeds;

void StatusLedController::begin(PwmController &pwm, uint8_t brightness,
                                uint32_t now, bool powerSignal) {
  pwm_ = &pwm;
  brightness_ = brightness;
  pwm_->setPowerSignal(powerSignal);
  setMode(StatusLedMode::Boot, now);
}

void StatusLedController::service(uint32_t now) {
  if (pwm_ == nullptr) {
    return;
  }

  if (cue_ != StatusLedCue::None &&
      static_cast<int32_t>(now - cueEndsAt_) >= 0) {
    cue_ = StatusLedCue::None;
    loadProfile(static_cast<uint8_t>(mode_), now);
  }

  if (effect_ != StatusLedEffect::None) {
    if (static_cast<uint32_t>(now - lastEffectStepAt_) < effectStepMs_) {
      return;
    }
    lastEffectStepAt_ = now;
    const uint8_t next =
        static_cast<uint8_t>(effectPhase_ + EffectPhaseStep);
    if (next < effectPhase_ && effectRepeats_ != 0 && --effectRepeats_ == 0) {
      finishEffect();
      return;
    }
    effectPhase_ = next;
    renderEffect();
  }
}

void StatusLedController::setMode(StatusLedMode mode, uint32_t now) {
  mode_ = mode;
  if (mode == StatusLedMode::Boot || mode == StatusLedMode::Fault) {
    cue_ = StatusLedCue::None;
  }
  loadProfile(static_cast<uint8_t>(mode), now);
}

StatusLedMode StatusLedController::mode() const { return mode_; }

void StatusLedController::setBrightness(uint8_t brightness) {
  brightness_ = brightness;
  if (effect_ != StatusLedEffect::None) {
    renderEffect();
  } else {
    renderColor(customRed_, customGreen_, customBlue_, brightness_);
  }
}

uint8_t StatusLedController::brightness() const { return brightness_; }

void StatusLedController::setCustom(uint8_t red, uint8_t green, uint8_t blue) {
  effect_ = StatusLedEffect::None;
  customRed_ = red;
  customGreen_ = green;
  customBlue_ = blue;
  mode_ = StatusLedMode::Custom;
  cue_ = StatusLedCue::None;
  condition_ = ManualCondition;
  renderColor(red, green, blue, brightness_);
}

bool StatusLedController::setEffect(
    StatusLedEffect effect, uint8_t red, uint8_t green, uint8_t blue,
    uint8_t alternateRed, uint8_t alternateGreen, uint8_t alternateBlue,
    uint8_t brightness, uint8_t minimumBrightness, uint16_t periodMs,
    uint8_t repeats, uint32_t now) {
  if (effect == StatusLedEffect::None ||
      effect > StatusLedEffect::Transition ||
      periodMs < MinimumEffectPeriodMs ||
      minimumBrightness > brightness) {
    return false;
  }
  cue_ = StatusLedCue::None;
  condition_ = ManualCondition;
  effect_ = effect;
  customRed_ = red;
  customGreen_ = green;
  customBlue_ = blue;
  alternateRed_ = alternateRed;
  alternateGreen_ = alternateGreen;
  alternateBlue_ = alternateBlue;
  brightness_ = brightness;
  minimumBrightness_ = minimumBrightness;
  effectRepeats_ = repeats;
  effectPhase_ = 0;
  effectStepMs_ = static_cast<uint16_t>(periodMs >> 5);
  lastEffectStepAt_ = now;
  renderEffect();
  return true;
}

void StatusLedController::cancelEffect() {
  effect_ = StatusLedEffect::None;
  loadProfile(static_cast<uint8_t>(mode_), millis());
}

StatusLedEffect StatusLedController::effect() const { return effect_; }

uint8_t StatusLedController::renderedRed() const { return renderedRed_; }
uint8_t StatusLedController::renderedGreen() const { return renderedGreen_; }
uint8_t StatusLedController::renderedBlue() const { return renderedBlue_; }
uint8_t StatusLedController::condition() const { return condition_; }

void StatusLedController::setPowerSignal(bool active) {
  if (pwm_ != nullptr) {
    pwm_->setPowerSignal(active);
  }
}

void StatusLedController::playCue(StatusLedCue cue, uint16_t durationMs,
                                  uint32_t now) {
  // Never let an informational overlay obscure a persistent critical fault.
  if (mode_ == StatusLedMode::Fault) {
    return;
  }
  cue_ = cue;
  cueEndsAt_ = now + durationMs;
  loadProfile(static_cast<uint8_t>(StatusModePaletteCount - 1U +
                                   static_cast<uint8_t>(cue)), now);
}

bool StatusLedController::profile(uint8_t condition, uint8_t *payload) const {
  if (condition >= ProfileCount || payload == nullptr) {
    return false;
  }
  const int address = EepromLayout::StatusProfileAddress +
                      condition * EepromLayout::StatusProfileRecordBytes;
  for (uint8_t index = 0; index < ProfilePayloadBytes; ++index) {
    payload[index] = EEPROM.read(address + index);
  }
  const uint8_t storedCrc = EEPROM.read(address + ProfilePayloadBytes);
  const bool stored =
      storedCrc == ControllerProtocol::UartProtocol::crc8(
                       payload, ProfilePayloadBytes) &&
      validProfile(payload);
  if (!stored) {
    defaultProfile(condition, payload);
  }
  return stored;
}

bool StatusLedController::setProfile(uint8_t condition,
                                     const uint8_t *payload,
                                     uint32_t now) {
  if (condition >= ProfileCount || !validProfile(payload)) {
    return false;
  }
  const int address = EepromLayout::StatusProfileAddress +
                      condition * EepromLayout::StatusProfileRecordBytes;
  for (uint8_t index = 0; index < ProfilePayloadBytes; ++index) {
    EEPROM.update(address + index, payload[index]);
  }
  EEPROM.update(address + ProfilePayloadBytes,
                ControllerProtocol::UartProtocol::crc8(
                    payload, ProfilePayloadBytes));
  if (condition_ == condition) {
    applyProfile(condition, payload, now);
  }
  return true;
}

void StatusLedController::loadProfile(uint8_t condition, uint32_t now) {
  uint8_t payload[ProfilePayloadBytes];
  profile(condition, payload);
  applyProfile(condition, payload, now);
}

void StatusLedController::applyProfile(uint8_t condition,
                                       const uint8_t *payload, uint32_t now) {
  condition_ = condition;
  customRed_ = payload[1];
  customGreen_ = payload[2];
  customBlue_ = payload[3];
  alternateRed_ = payload[4];
  alternateGreen_ = payload[5];
  alternateBlue_ = payload[6];
  brightness_ = payload[7];
  minimumBrightness_ = payload[8];
  effect_ = static_cast<StatusLedEffect>(payload[0]);
  effectRepeats_ = payload[11];
  effectPhase_ = 0;
  if (effect_ == StatusLedEffect::None) {
    renderColor(customRed_, customGreen_, customBlue_, brightness_);
    return;
  }
  const uint16_t periodMs = static_cast<uint16_t>(payload[9]) |
                            static_cast<uint16_t>(payload[10]) << 8;
  effectStepMs_ = static_cast<uint16_t>(periodMs >> 5);
  lastEffectStepAt_ = now;
  renderEffect();
}

bool StatusLedController::validProfile(const uint8_t *payload) {
  if (payload == nullptr || payload[0] >
                                static_cast<uint8_t>(StatusLedEffect::Transition) ||
      payload[8] > payload[7]) {
    return false;
  }
  const uint16_t periodMs = static_cast<uint16_t>(payload[9]) |
                            static_cast<uint16_t>(payload[10]) << 8;
  return payload[0] == 0 || periodMs >= MinimumEffectPeriodMs;
}

void StatusLedController::defaultProfile(uint8_t condition,
                                         uint8_t *payload) const {
  // The Go tooling owns and provisions the full factory profile table. The
  // firmware retains only a tiny safe fallback for corrupt/blank EEPROM: off
  // stays dark, hot/fault stays red, and other states remain visible blue.
  memset(payload, 0, ProfilePayloadBytes);
  payload[7] = brightness_;
  if (condition != static_cast<uint8_t>(StatusLedMode::Off) &&
      condition != static_cast<uint8_t>(StatusLedMode::Custom)) {
    const uint8_t channel =
        condition == static_cast<uint8_t>(StatusLedMode::Warning) ||
                condition == static_cast<uint8_t>(StatusLedMode::Fault)
            ? 1U
            : 3U;
    payload[channel] = 255;
  }
}

void StatusLedController::renderColor(uint8_t red, uint8_t green,
                                      uint8_t blue, uint8_t level) {
  const uint8_t red8 = scale(red, level);
  const uint8_t green8 = scale(green, level);
  const uint8_t blue8 = scale(blue, level);
  pwm_->setStatusRgb8(red8, green8, blue8);
  renderedRed_ = red8;
  renderedGreen_ = green8;
  renderedBlue_ = blue8;
}

void StatusLedController::renderEffect() {
  uint8_t red = customRed_;
  uint8_t green = customGreen_;
  uint8_t blue = customBlue_;
  uint8_t level = brightness_;
  const uint8_t triangle = effectPhase_ < 128U
                               ? static_cast<uint8_t>(effectPhase_ << 1)
                               : static_cast<uint8_t>((255U - effectPhase_) << 1);
  if (effect_ == StatusLedEffect::Flash) {
    if (effectPhase_ >= 128U) {
      red = alternateRed_;
      green = alternateGreen_;
      blue = alternateBlue_;
    }
  } else if (effect_ == StatusLedEffect::Breathe) {
    level = static_cast<uint8_t>(
        minimumBrightness_ +
        scale(static_cast<uint8_t>(brightness_ - minimumBrightness_), triangle));
  } else if (effect_ == StatusLedEffect::Transition) {
    red = interpolate(customRed_, alternateRed_, effectPhase_);
    green = interpolate(customGreen_, alternateGreen_, effectPhase_);
    blue = interpolate(customBlue_, alternateBlue_, effectPhase_);
  } else if (effect_ == StatusLedEffect::Cycle) {
    red = interpolate(customRed_, alternateRed_, triangle);
    green = interpolate(customGreen_, alternateGreen_, triangle);
    blue = interpolate(customBlue_, alternateBlue_, triangle);
  }
  renderColor(red, green, blue, level);
}

void StatusLedController::finishEffect() {
  const bool transition = effect_ == StatusLedEffect::Transition;
  effect_ = StatusLedEffect::None;
  if (transition) {
    customRed_ = alternateRed_;
    customGreen_ = alternateGreen_;
    customBlue_ = alternateBlue_;
  }
  renderColor(customRed_, customGreen_, customBlue_, brightness_);
}

uint8_t StatusLedController::interpolate(uint8_t from, uint8_t to,
                                         uint8_t phase) {
  const int16_t delta = static_cast<int16_t>(to) - from;
  return static_cast<uint8_t>(
      static_cast<int16_t>(from) + (delta * phase) / 256);
}

uint8_t StatusLedController::scale(uint8_t value, uint8_t level) {
  // The +1 expansion keeps both endpoints exact while compiling to a multiply
  // and byte select on AVR instead of pulling in a 16-bit divide helper.
  return static_cast<uint8_t>(
      (static_cast<uint16_t>(value) * (static_cast<uint16_t>(level) + 1U)) >>
      8);
}
#else
StatusLedController statusLeds;
#endif
