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
constexpr uint8_t StatusModePaletteCount = 11;

} // namespace

StatusLedController statusLeds;

void StatusLedController::begin(PwmController &pwm, uint8_t brightness,
                                uint16_t now, bool powerSignal) {
  pwm_ = &pwm;
  fallbackBrightness_ = brightness;
  pwm_->setPowerSignal(powerSignal);
  setMode(StatusLedMode::Boot, now);
}

void StatusLedController::service(uint16_t now) {
  if (pwm_ == nullptr) {
    return;
  }

  if (cue_ != StatusLedCue::None &&
      static_cast<int16_t>(now - cueEndsAt_) >= 0) {
    cue_ = StatusLedCue::None;
    // Re-enter through the priority selector so expiry restores the retained
    // manual request rather than the persisted Custom profile.
    setMode(mode_, now);
  }

  if (active_[0] != 0) {
    const uint16_t periodMs = static_cast<uint16_t>(active_[9]) |
                              static_cast<uint16_t>(active_[10]) << 8;
    const uint16_t tick = now;
    uint16_t elapsed = static_cast<uint16_t>(tick - effectCycleStartedAt_);
    if (elapsed >= periodMs) {
      const uint8_t cycles = static_cast<uint8_t>(elapsed / periodMs);
      if (active_[11] != 0) {
        if (cycles >= active_[11]) {
          finishEffect();
          return;
        }
        active_[11] = static_cast<uint8_t>(active_[11] - cycles);
      }
      elapsed = static_cast<uint16_t>(elapsed % periodMs);
      effectCycleStartedAt_ = static_cast<uint16_t>(tick - elapsed);
    }

    const uint8_t phase = static_cast<uint8_t>(
        (((static_cast<uint32_t>(elapsed) << 6) + 63U) / periodMs) << 2);
    if (phase != effectPhase_) {
      effectPhase_ = phase;
      renderEffect();
    }
  }
}

void StatusLedController::setMode(StatusLedMode mode, uint16_t now) {
  mode_ = mode;
  if (cue_ != StatusLedCue::None) {
    if (!persistentPriorityActive()) {
      return;
    }
    cue_ = StatusLedCue::None;
  }
  if (mode == StatusLedMode::Custom && requested_[10] != 0) {
    applyRequested(now);
    return;
  }
  loadProfile(static_cast<uint8_t>(mode), now);
}

StatusLedMode StatusLedController::mode() const { return mode_; }

void StatusLedController::setBrightness(uint8_t brightness) {
  fallbackBrightness_ = brightness;
  active_[7] = brightness;
  if (active_[0] != 0) {
    renderEffect();
  } else {
    renderColor(active_[1], active_[2], active_[3], active_[7]);
  }
}

uint8_t StatusLedController::brightness() const { return active_[7]; }

void StatusLedController::setCustom(uint8_t red, uint8_t green, uint8_t blue,
                                    uint8_t brightness, uint16_t now) {
  requested_[0] = 0;
  requested_[1] = red;
  requested_[2] = green;
  requested_[3] = blue;
  requested_[7] = brightness;
  requested_[10] = 1; // Static-owner marker; effect-zero ignores period.
  applyRequested(now);
}

bool StatusLedController::setEffect(const uint8_t *payload, uint16_t now) {
  if (!validProfile(payload) || payload[0] == 0) {
    return false;
  }
  if (requested_[10] != 0 &&
      memcmp(requested_, payload, ProfilePayloadBytes) == 0) {
    return true;
  }
  memcpy(requested_, payload, ProfilePayloadBytes);
  applyRequested(now);
  return true;
}

void StatusLedController::applyRequested(uint16_t now) {
  if (persistentPriorityActive() || cue_ == StatusLedCue::Reset) {
    return;
  }
  mode_ = StatusLedMode::Custom;
  cue_ = StatusLedCue::None;
  applyProfile(ManualCondition, requested_, now);
}

void StatusLedController::cancelEffect() {
  requested_[10] = 0;
  if (condition_ == ManualCondition) {
    active_[0] = 0;
  }
  // The lifecycle clears Custom on its next pass. Preserve the last rendered
  // frame until then so release cannot flash a persisted fallback profile.
}

StatusLedEffect StatusLedController::effect() const {
  return static_cast<StatusLedEffect>(active_[0]);
}

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
                                  uint16_t now) {
  // Learning/Warning/Fault always dominate. Routine informational cues never
  // steal a manual owner, while Reset remains visible before watchdog reboot.
  if (persistentPriorityActive() ||
      (mode_ == StatusLedMode::Custom &&
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
                                     uint16_t now) {
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

void StatusLedController::loadProfile(uint8_t condition, uint16_t now) {
  uint8_t payload[ProfilePayloadBytes];
  profile(condition, payload);
  applyProfile(condition, payload, now);
}

void StatusLedController::applyProfile(uint8_t condition,
                                       const uint8_t *payload, uint16_t now) {
  condition_ = condition;
  memcpy(active_, payload, ProfilePayloadBytes);
  effectPhase_ = 0;
  if (active_[0] == 0) {
    renderColor(active_[1], active_[2], active_[3], active_[7]);
    return;
  }
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
  if (payload[0] == 0) {
    return true;
  }
  const uint16_t periodMs = static_cast<uint16_t>(payload[9]) |
                            static_cast<uint16_t>(payload[10]) << 8;
  if (static_cast<uint16_t>(periodMs - MinimumEffectPeriodMs) > 59360U) {
    return false;
  }
  return true;
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
  uint8_t red = active_[1];
  uint8_t green = active_[2];
  uint8_t blue = active_[3];
  uint8_t level = active_[7];
  const uint8_t triangle = effectPhase_ < 128U
                               ? static_cast<uint8_t>(effectPhase_ << 1)
                               : static_cast<uint8_t>((255U - effectPhase_) << 1);
  if (active_[0] == static_cast<uint8_t>(StatusLedEffect::Flash)) {
    if (effectPhase_ >= 128U) {
      red = active_[4];
      green = active_[5];
      blue = active_[6];
    }
  } else if (active_[0] == static_cast<uint8_t>(StatusLedEffect::Breathe)) {
    level = static_cast<uint8_t>(
        active_[8] +
        StatusLedMath::scale(
            static_cast<uint8_t>(active_[7] - active_[8]), triangle));
  } else if (active_[0] ==
             static_cast<uint8_t>(StatusLedEffect::Transition)) {
    red = StatusLedMath::interpolate(active_[1], active_[4], effectPhase_);
    green = StatusLedMath::interpolate(active_[2], active_[5], effectPhase_);
    blue = StatusLedMath::interpolate(active_[3], active_[6], effectPhase_);
  } else if (active_[0] == static_cast<uint8_t>(StatusLedEffect::Cycle)) {
    red = StatusLedMath::interpolate(active_[1], active_[4], triangle);
    green = StatusLedMath::interpolate(active_[2], active_[5], triangle);
    blue = StatusLedMath::interpolate(active_[3], active_[6], triangle);
  }
  renderColor(red, green, blue, level);
}

void StatusLedController::finishEffect() {
  const bool transition =
      active_[0] == static_cast<uint8_t>(StatusLedEffect::Transition);
  active_[0] = 0;
  if (transition) {
    memcpy(active_ + 1, active_ + 4, 3);
  }
  renderColor(active_[1], active_[2], active_[3], active_[7]);
  if (condition_ == ManualCondition) {
    requested_[0] = 0;
    if (transition) {
      memcpy(requested_ + 1, requested_ + 4, 3);
    }
    requested_[10] = 1;
  }
}
