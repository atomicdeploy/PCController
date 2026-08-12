#include "StatusLedController.h"

#if PCCONTROLLER_ENABLE_PCA9685 && PCCONTROLLER_ENABLE_STATUS_LED_ENGINE

#include <string.h>

#include "PwmController.h"

namespace {
PwmController *statusPwm;
}

void StatusLedController::begin(PwmController &pwm, uint8_t brightness,
                                uint32_t now, bool powerSignal) {
  statusPwm = &pwm;
  localBrightness_ = brightness;
  statusPwm->setPowerSignal(powerSignal);
  activeMode_ = StatusLedMode::Off;
  setMode(StatusLedMode::Boot, now);
  service(now);
}

void StatusLedController::service(uint32_t now) {
  if (statusPwm == nullptr) {
    return;
  }
  if (dirty_) {
    dirty_ = false;
    lastFrameAt_ = now;
    render();
    return;
  }
  if (effect() == StatusLedEffect::None ||
      static_cast<uint32_t>(now - lastFrameAt_) <
          StatusLedTiming::FrameIntervalMs) {
    return;
  }
  const uint32_t late = now - lastFrameAt_;
  lastFrameAt_ = late <= StatusLedTiming::FrameIntervalMs * 2UL
                     ? lastFrameAt_ + StatusLedTiming::FrameIntervalMs
                     : now;

  effectElapsedMs_ = static_cast<uint16_t>(
      effectElapsedMs_ + StatusLedTiming::FrameIntervalMs);
  const bool wrapped = effectElapsedMs_ >= effectPeriodMs_;
  if (wrapped) {
    effectElapsedMs_ = static_cast<uint16_t>(effectElapsedMs_ -
                                             effectPeriodMs_);
  }
  if (wrapped && activeMode_ == StatusLedMode::Custom &&
      hostDescriptor_[11] != 0 && --hostDescriptor_[11] == 0) {
    if (hostDescriptor_[0] ==
        static_cast<uint8_t>(StatusLedEffect::Transition)) {
      memcpy(hostDescriptor_ + 1, hostDescriptor_ + 4, 3);
    }
    hostDescriptor_[0] = 0;
  }
  render();
}

void StatusLedController::setMode(StatusLedMode mode, uint32_t now) {
  if (activeMode_ == mode) {
    return;
  }
  activeMode_ = mode;
  effectElapsedMs_ = 0;
  if (mode == StatusLedMode::Custom) {
    if (hostDescriptor_[0] != 0) {
      effectPeriodMs_ = static_cast<uint16_t>(hostDescriptor_[9]) |
                        static_cast<uint16_t>(hostDescriptor_[10]) << 8;
    }
  } else {
    if (mode != StatusLedMode::Off) {
      effectPeriodMs_ = 1600;
    }
  }
  dirty_ = true;
  (void)now;
}

void StatusLedController::setBrightness(uint8_t brightness) {
  localBrightness_ = brightness;
  if (activeMode_ != StatusLedMode::Custom) {
    dirty_ = true;
  }
}

void StatusLedController::setCustom(const uint8_t *payload, uint32_t now) {
  memset(hostDescriptor_, 0, sizeof(hostDescriptor_));
  memcpy(hostDescriptor_ + 1, payload, 3);
  hostDescriptor_[7] = payload[3];
  activeMode_ = StatusLedMode::Off;
  setMode(StatusLedMode::Custom, now);
}

bool StatusLedController::setEffect(const uint8_t *payload, uint32_t now) {
  const uint16_t periodMs = static_cast<uint16_t>(payload[9]) |
                            static_cast<uint16_t>(payload[10]) << 8;
  if (payload[0] == 0 ||
      payload[0] > static_cast<uint8_t>(StatusLedEffect::Transition) ||
      periodMs < StatusLedTiming::MinimumPeriodMs ||
      periodMs > StatusLedTiming::MaximumPeriodMs ||
      payload[8] > payload[7]) {
    return false;
  }
  // Host policy reconciliation may repeat an already-owned descriptor. Keep
  // the MCU phase continuous only for a byte-identical 12-byte descriptor;
  // changing colour, brightness, timing, or repeats must take effect even
  // when the effect kind itself is unchanged.
  if (activeMode_ == StatusLedMode::Custom &&
      memcmp(hostDescriptor_, payload, sizeof(hostDescriptor_)) == 0) {
    return true;
  }
  memcpy(hostDescriptor_, payload, sizeof(hostDescriptor_));
  activeMode_ = StatusLedMode::Off;
  setMode(StatusLedMode::Custom, now);
  return true;
}

void StatusLedController::cancelEffect() {
  hostDescriptor_[0] = 0;
  activeMode_ = StatusLedMode::Off;
  dirty_ = true;
}

void StatusLedController::render() {
  const bool custom = activeMode_ == StatusLedMode::Custom;
  const uint8_t effect = static_cast<uint8_t>(this->effect());
  const uint8_t *descriptor = hostDescriptor_;
  uint8_t level = custom ? descriptor[7] : localBrightness_;
  const uint8_t phase = effect == 0
                            ? 0
                            : static_cast<uint8_t>(
                                  (static_cast<uint32_t>(effectElapsedMs_)
                                   << 8) /
                                  effectPeriodMs_);
  // This parabola is smooth at its midpoint and continuous at cycle wrap.
  const uint8_t wave = static_cast<uint8_t>(
      (static_cast<uint16_t>(phase) * (255U - phase)) >> 6);
  uint8_t amount = 0;
  if (effect == static_cast<uint8_t>(StatusLedEffect::Breathe)) {
    const uint8_t minimum = custom ? descriptor[8]
                                   : static_cast<uint8_t>(level >> 4);
    level = static_cast<uint8_t>(
        minimum + scale(level - minimum, wave));
  } else if (effect == static_cast<uint8_t>(StatusLedEffect::Cycle)) {
    amount = wave;
  } else if (effect == static_cast<uint8_t>(StatusLedEffect::Transition)) {
    amount = phase;
  } else if (effect == static_cast<uint8_t>(StatusLedEffect::Flash) &&
             phase >= 128U) {
    amount = 255;
  }

  uint8_t rgb[3] = {};
  if (!custom) {
    if (activeMode_ == StatusLedMode::Fault ||
        activeMode_ == StatusLedMode::Warning ||
        activeMode_ == StatusLedMode::Running) {
      rgb[0] = 255;
      rgb[1] = activeMode_ == StatusLedMode::Running ? 72 : 0;
    } else if (activeMode_ == StatusLedMode::Learning ||
               activeMode_ == StatusLedMode::Connected) {
      rgb[1] = 255;
    } else if (activeMode_ != StatusLedMode::Off) {
      rgb[2] = 255;
    }
  } else {
    for (uint8_t channel = 0; channel < 3; ++channel) {
      rgb[channel] = interpolate(descriptor[channel + 1],
                                 descriptor[channel + 4], amount);
    }
  }
  for (uint8_t channel = 0; channel < 3; ++channel) {
    rgb[channel] = scale(rgb[channel], level);
  }
  statusPwm->setStatusRgb8(rgb[0], rgb[1], rgb[2]);
#if PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS
  renderedRed_ = rgb[0];
  renderedGreen_ = rgb[1];
  renderedBlue_ = rgb[2];
#endif
#if defined(PCCONTROLLER_NATIVE_TEST)
  ++renderedFrames_;
#endif
}

uint8_t StatusLedController::interpolate(uint8_t from, uint8_t to,
                                         uint8_t phase) {
  return to >= from
             ? static_cast<uint8_t>(from + scale(to - from, phase))
             : static_cast<uint8_t>(from - scale(from - to, phase));
}

uint8_t StatusLedController::scale(uint8_t value, uint8_t level) {
  return static_cast<uint8_t>(
      (static_cast<uint16_t>(value) * (static_cast<uint16_t>(level) + 1U)) >>
      8);
}

#endif

StatusLedController statusLeds;
