#include "IlluminationController.h"

#include "PwmController.h"

namespace {

constexpr uint16_t FadeIntervalMs = 20;
constexpr uint8_t FadeStep = 4;

} // namespace

IlluminationController illumination;

void IlluminationController::begin(PwmController &pwm, bool doorOpen,
                                   uint32_t now) {
  pwm_ = &pwm;
  currentBrightness_ =
      mode_ == IlluminationMode::On ? onBrightness_ : offBrightness_;
  pwm_->setEnclosureIllumination(
      static_cast<uint16_t>(currentBrightness_) * 16U +
      currentBrightness_ / 16U);
  lastFadeAt_ = now;
  initialized_ = true;

  // AUTO deliberately begins at the configured closed/off level and lets the
  // normal service path fade toward the door-open target.
  (void)doorOpen;
}

void IlluminationController::service(bool doorOpen, bool allowLedUpdate,
                                     uint32_t now) {
  if (!initialized_ || pwm_ == nullptr) {
    return;
  }
  if (!allowLedUpdate) {
    // A host I2C lease pauses rather than accumulates fade intervals; resume
    // at the normal 20 ms cadence instead of compressing the transition.
    lastFadeAt_ = now;
    return;
  }
  if (static_cast<uint32_t>(now - lastFadeAt_) < FadeIntervalMs) {
    return;
  }

  const uint8_t target = targetBrightness(doorOpen);
  if (currentBrightness_ == target) {
    // Keep the fade clock current while idle. Otherwise a door edge after a
    // long stable period inherits a huge elapsed interval and compresses the
    // entire transition into a few loop iterations, appearing as a jump.
    lastFadeAt_ = now;
    return;
  }
  const uint32_t elapsed = static_cast<uint32_t>(now - lastFadeAt_);
  const uint32_t elapsedIntervals = elapsed / FadeIntervalMs;
  const uint8_t intervals = static_cast<uint8_t>(
      elapsedIntervals > 16 ? 16 : elapsedIntervals);
  lastFadeAt_ += static_cast<uint32_t>(intervals) * FadeIntervalMs;
  const uint16_t rawDistance =
      static_cast<uint16_t>(FadeStep) * intervals;
  const uint8_t distance =
      static_cast<uint8_t>(rawDistance > 255 ? 255 : rawDistance);

  if (currentBrightness_ < target) {
    const uint16_t next =
        static_cast<uint16_t>(currentBrightness_) + distance;
    currentBrightness_ =
        static_cast<uint8_t>(next > target ? target : next);
  } else {
    currentBrightness_ =
        static_cast<uint8_t>(currentBrightness_ - target > distance
                                 ? currentBrightness_ - distance
                                 : target);
  }

  pwm_->setEnclosureIllumination(
      static_cast<uint16_t>(currentBrightness_) * 16U +
      currentBrightness_ / 16U);
}

void IlluminationController::setMode(IlluminationMode mode) { mode_ = mode; }

IlluminationMode IlluminationController::mode() const { return mode_; }

void IlluminationController::setOnBrightness(uint8_t brightness) {
  onBrightness_ = brightness;
}

void IlluminationController::setOffBrightness(uint8_t brightness) {
  offBrightness_ = brightness;
}

uint8_t IlluminationController::onBrightness() const {
  return onBrightness_;
}

uint8_t IlluminationController::offBrightness() const {
  return offBrightness_;
}

uint8_t IlluminationController::currentBrightness() const {
  return currentBrightness_;
}

uint8_t
IlluminationController::targetBrightness(bool doorOpen) const {
  switch (mode_) {
    case IlluminationMode::Off:
      return offBrightness_;
    case IlluminationMode::On:
      return onBrightness_;
    case IlluminationMode::Auto:
      return doorOpen ? onBrightness_ : offBrightness_;
  }
  return offBrightness_;
}
