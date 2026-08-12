#include "StatusLedController.h"

#include <EEPROM.h>
#include <string.h>

#include "EepromLayout.h"
#include "PwmController.h"
#include "StatusLedMath.h"
#include "UartProtocol.h"

namespace {
// One cooperative cadence and the compact condition numbering shared with Go.
constexpr uint16_t MinimumEffectPeriodMs = 640;
constexpr uint16_t MaximumEffectPeriodMs = 60000;
constexpr uint8_t EffectPhaseStep = 4;
constexpr uint8_t StatusModePaletteCount = 11;

} // namespace

StatusLedController statusLeds;

void StatusLedController::begin(PwmController &pwm, uint8_t brightness,
                                uint32_t now, bool powerSignal) {
  pwm_ = &pwm;
  brightness_ = brightness;
  fallbackBrightness_ = brightness;
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
    // Re-enter through the layer selector so a system Reset cue cannot discard
    // the retained board-owned descriptor if watchdog service is delayed.
    setMode(mode_, now);
  }

  if (effect_ != StatusLedEffect::None) {
    uint32_t elapsed = static_cast<uint32_t>(now - effectCycleStartedAt_);
    bool cycleAdvanced = false;
    if (elapsed >= effectPeriodMs_) {
      const uint32_t cycles = elapsed / effectPeriodMs_;
      if (effectRepeats_ != 0 && cycles >= effectRepeats_) {
        finishEffect();
        return;
      }
      if (effectRepeats_ != 0) {
        effectRepeats_ = static_cast<uint8_t>(effectRepeats_ - cycles);
      }
      effectCycleStartedAt_ += cycles * effectPeriodMs_;
      elapsed -= cycles * effectPeriodMs_;
      effectPhase_ = 0;
      cycleAdvanced = true;
    }
    uint8_t step = static_cast<uint8_t>(effectPhase_ >> 2);
    while (step < 63U) {
      const uint8_t next = static_cast<uint8_t>(step + 1U);
      const uint16_t deadline =
          StatusLedMath::phaseDeadline(effectPeriodMs_, next);
      if (elapsed < deadline) {
        break;
      }
      step = next;
    }
    const uint8_t phase = static_cast<uint8_t>(step * EffectPhaseStep);
    if (phase != effectPhase_ || cycleAdvanced) {
      effectPhase_ = phase;
      renderEffect();
    }
  }
}

void StatusLedController::setMode(StatusLedMode mode, uint32_t now) {
  mode_ = mode;
  if (persistentPriorityActive()) {
    cue_ = StatusLedCue::None;
  }
  if (cue_ != StatusLedCue::None) {
    // Update the base mode while keeping its informational overlay intact.
    return;
  }
  if (mode == StatusLedMode::Custom && requested_[10] != 0) {
    applyProfile(ManualCondition, requested_, now);
  } else {
    loadProfile(static_cast<uint8_t>(mode), now);
  }
}

StatusLedMode StatusLedController::mode() const { return mode_; }

void StatusLedController::setBrightness(uint8_t brightness) {
  brightness_ = brightness;
  fallbackBrightness_ = brightness;
  if (effect_ != StatusLedEffect::None) {
    renderEffect();
  } else {
    renderColor(customRed_, customGreen_, customBlue_, brightness_);
  }
}

uint8_t StatusLedController::brightness() const { return brightness_; }

void StatusLedController::setCustom(uint8_t red, uint8_t green, uint8_t blue,
                                    uint8_t brightness, uint32_t now) {
  requested_[0] = 0;
  requested_[1] = red;
  requested_[2] = green;
  requested_[3] = blue;
  requested_[7] = brightness;
  requested_[10] = 1; // Non-effect owner marker; ignored by applyProfile().
  if (!persistentPriorityActive() && cue_ != StatusLedCue::Reset) {
    mode_ = StatusLedMode::Custom;
    cue_ = StatusLedCue::None;
    applyProfile(ManualCondition, requested_, now);
  }
}

bool StatusLedController::setEffect(const uint8_t *payload, uint32_t now) {
  if (!validProfile(payload) || payload[0] == 0) {
    return false;
  }
  if (requested_[10] != 0 &&
      memcmp(requested_, payload, ProfilePayloadBytes) == 0) {
    return true;
  }
  memcpy(requested_, payload, ProfilePayloadBytes);
  if (!persistentPriorityActive() && cue_ != StatusLedCue::Reset) {
    mode_ = StatusLedMode::Custom;
    cue_ = StatusLedCue::None;
    applyProfile(ManualCondition, requested_, now);
  }
  return true;
}

void StatusLedController::cancelEffect() {
  requested_[10] = 0;
  if (condition_ == ManualCondition) {
    effect_ = StatusLedEffect::None;
  }
  // The caller clears the host override before the next lifecycle pass. Keep
  // the last rendered frame here so cancellation cannot flash the Custom
  // fallback profile between the host and board owners.
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
  // Safety is always visible. Routine informational overlays never steal a
  // board-owned manual presentation, but the watchdog reset cue must remain
  // visible during its final safe-off interval.
  const uint8_t mode = static_cast<uint8_t>(mode_);
  if (mode == static_cast<uint8_t>(StatusLedMode::Boot) ||
      static_cast<uint8_t>(mode -
                           static_cast<uint8_t>(StatusLedMode::Learning)) <
          3U ||
      (mode == static_cast<uint8_t>(StatusLedMode::Custom) &&
       cue != StatusLedCue::Reset)) {
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
  effectPeriodMs_ = periodMs;
  effectCycleStartedAt_ = now;
  renderEffect();
}

bool StatusLedController::persistentPriorityActive() const {
  const uint8_t value = static_cast<uint8_t>(mode_);
  return value == static_cast<uint8_t>(StatusLedMode::Boot) ||
         static_cast<uint8_t>(value -
                              static_cast<uint8_t>(StatusLedMode::Learning)) <
             3U;
}

bool StatusLedController::validProfile(const uint8_t *payload) {
  if (payload == nullptr || payload[0] >
                                static_cast<uint8_t>(StatusLedEffect::Transition) ||
      payload[8] > payload[7]) {
    return false;
  }
  const uint16_t periodMs = static_cast<uint16_t>(payload[9]) |
                            static_cast<uint16_t>(payload[10]) << 8;
  return payload[0] == 0 ||
         (periodMs >= MinimumEffectPeriodMs &&
          periodMs <= MaximumEffectPeriodMs);
}

void StatusLedController::defaultProfile(uint8_t condition,
                                         uint8_t *payload) const {
  // The Go tooling owns and provisions the full factory profile table. The
  // firmware retains only a tiny safe fallback for corrupt/blank EEPROM: off
  // stays dark, hot/fault stays red, and other states remain visible blue.
  memset(payload, 0, ProfilePayloadBytes);
  payload[7] = fallbackBrightness_;
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
  const uint8_t red8 = StatusLedMath::scale(red, level);
  const uint8_t green8 = StatusLedMath::scale(green, level);
  const uint8_t blue8 = StatusLedMath::scale(blue, level);
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
        StatusLedMath::scale(
            static_cast<uint8_t>(brightness_ - minimumBrightness_), triangle));
  } else if (effect_ == StatusLedEffect::Transition) {
    red = StatusLedMath::interpolate(customRed_, alternateRed_, effectPhase_);
    green =
        StatusLedMath::interpolate(customGreen_, alternateGreen_, effectPhase_);
    blue =
        StatusLedMath::interpolate(customBlue_, alternateBlue_, effectPhase_);
  } else if (effect_ == StatusLedEffect::Cycle) {
    red = StatusLedMath::interpolate(customRed_, alternateRed_, triangle);
    green = StatusLedMath::interpolate(customGreen_, alternateGreen_, triangle);
    blue = StatusLedMath::interpolate(customBlue_, alternateBlue_, triangle);
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
  if (condition_ == ManualCondition) {
    requested_[0] = 0;
    if (transition) {
      memcpy(requested_ + 1, requested_ + 4, 3);
    }
    requested_[10] = 1;
  }
}
