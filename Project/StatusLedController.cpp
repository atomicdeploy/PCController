#include "StatusLedController.h"

#include <avr/pgmspace.h>

#include "PwmController.h"
#include "TransitionMath.h"

namespace {
// Cooperative breathing cadence, intensity step, and base-mode row count.
constexpr uint16_t PulseIntervalMs = 20;
constexpr uint8_t PulseStep = 4;
constexpr uint8_t StatusModePaletteCount = 11;

// Data-driven palette keeps every cue visually distinct while using less
// flash than a large branch tree. Rows 0..10 are StatusLedMode values; the
// remaining rows are StatusLedCue values 1..8.
const uint8_t StatusPalette[][3] PROGMEM = {
    {0, 0, 0},       // Off
    {255, 72, 0},    // Boot
    {255, 255, 255}, // Ready palette: white
    {190, 0, 255},   // 433 MHz learning
    {255, 96, 0},    // Warning / hot
    {255, 0, 0},     // Fault
    {0, 0, 0},       // Custom (supplied at runtime)
    {16, 72, 255},   // BT Audio connected
    {0, 255, 80},    // BT Audio unavailable phase A (phase B is Fault red)
    {16, 72, 255},   // BT Audio powered but waiting for a connection
    {255, 144, 0},   // Running, enclosure closed
    {255, 120, 12},  // Door open
    {0, 255, 80},    // Door closed
    {16, 72, 255},   // Bluetooth
    {0, 180, 255},   // Menu navigation
    {190, 0, 255},   // 433 MHz activity
    {0, 255, 24},    // Saved
    {255, 12, 0},    // Discarded
    {255, 48, 0},    // Graceful reset
};

// User order: red, blue, violet, green, white. Reuse cue/mode palette rows
// rather than duplicating RGB triples in flash.
const uint8_t ReadyPalette[] PROGMEM = {5, 3, 14, 11, 2};

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
  if (pwm_ == nullptr ||
      static_cast<uint32_t>(now - lastStepAt_) < PulseIntervalMs) {
    return;
  }
  lastStepAt_ = now;

  if (cue_ != StatusLedCue::None &&
      static_cast<int32_t>(now - cueEndsAt_) >= 0) {
    cue_ = StatusLedCue::None;
  }

  // Informational overlays converge directly toward their cue color and,
  // after expiry, toward the underlying mode color. This gives door/RF/BT/
  // menu events a damped hue transition instead of black-frame jumps.
  if (cue_ != StatusLedCue::None) {
    render(brightness_, true);
    return;
  }

  if (mode_ == StatusLedMode::Fault) {
    // Critical/offline/running-door warnings intentionally hard-flash; all
    // informational states and cues retain damped transitions.
    pulse_ = ((now / 250U) & 1U) != 0 ? brightness_ : 0;
    render(pulse_, false);
    return;
  }

  const bool breathing = mode_ == StatusLedMode::Learning ||
                         mode_ == StatusLedMode::Warning ||
                         mode_ == StatusLedMode::Disconnected ||
                         mode_ == StatusLedMode::Waiting;
  if (!breathing) {
    render(brightness_, true); // Continue a smooth post-cue restore.
    return;
  }
  if (pulseRising_) {
    const uint16_t next = static_cast<uint16_t>(pulse_) + PulseStep;
    pulse_ = static_cast<uint8_t>(next >= brightness_ ? brightness_ : next);
    if (pulse_ == brightness_) {
      pulseRising_ = false;
    }
  } else {
    pulse_ = pulse_ > PulseStep ? static_cast<uint8_t>(pulse_ - PulseStep) : 0;
    if (pulse_ == 0) {
      pulseRising_ = true;
    }
  }
  render(pulse_, true);
}

void StatusLedController::setMode(StatusLedMode mode, uint32_t now) {
  mode_ = mode;
  pulse_ = brightness_;
  pulseRising_ = false;
  lastStepAt_ = now;
  const bool immediate = mode == StatusLedMode::Boot ||
                         mode == StatusLedMode::Fault;
  if (immediate) {
    cue_ = StatusLedCue::None;
  }
  render(brightness_, !immediate);
}

StatusLedMode StatusLedController::mode() const { return mode_; }

void StatusLedController::setBrightness(uint8_t brightness) {
  brightness_ = brightness;
  render(brightness_, true);
}

uint8_t StatusLedController::brightness() const { return brightness_; }

void StatusLedController::setReadyColor(uint8_t color) {
  if (color > 4) {
    color = 0;
  }
  readyPalette_ = pgm_read_byte(ReadyPalette + color);
  if (mode_ == StatusLedMode::Ready) {
    render(brightness_, true);
  }
}

void StatusLedController::setCustom(uint8_t red, uint8_t green, uint8_t blue) {
  customRed_ = red;
  customGreen_ = green;
  customBlue_ = blue;
  setMode(StatusLedMode::Custom);
}

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
  lastStepAt_ = now - PulseIntervalMs;
  render(brightness_, true);
}

void StatusLedController::render(uint8_t level, bool eased) {
  if (pwm_ == nullptr) {
    return;
  }

  uint8_t red = 0;
  uint8_t green = 0;
  uint8_t blue = 0;
  if (cue_ == StatusLedCue::None && mode_ == StatusLedMode::Custom) {
    red = customRed_;
    green = customGreen_;
    blue = customBlue_;
  } else {
    uint8_t paletteIndex =
        cue_ == StatusLedCue::None
            ? (mode_ == StatusLedMode::Ready
                   ? readyPalette_
                   : static_cast<uint8_t>(mode_))
            : static_cast<uint8_t>(StatusModePaletteCount - 1U +
                                   static_cast<uint8_t>(cue_));
    if (cue_ == StatusLedCue::None &&
        mode_ == StatusLedMode::Disconnected && !pulseRising_) {
      paletteIndex = static_cast<uint8_t>(StatusLedMode::Fault);
    }
    const uint8_t *color = StatusPalette[paletteIndex];
    red = pgm_read_byte(color);
    green = pgm_read_byte(color + 1);
    blue = pgm_read_byte(color + 2);
  }
  const uint8_t red8 = scale(red, level);
  const uint8_t green8 = scale(green, level);
  const uint8_t blue8 = scale(blue, level);
  const uint16_t targetRed =
      static_cast<uint16_t>(red8) * 16U + red8 / 16U;
  const uint16_t targetGreen =
      static_cast<uint16_t>(green8) * 16U + green8 / 16U;
  const uint16_t targetBlue =
      static_cast<uint16_t>(blue8) * 16U + blue8 / 16U;
  if (!eased) {
    pwm_->setStatusRgb12(targetRed, targetGreen, targetBlue);
    return;
  }
  pwm_->setStatusRgb12(
      TransitionMath::easedChannel(
          pwm_->logicalValue(PwmChannels::StatusRed), targetRed),
      TransitionMath::easedChannel(
          pwm_->logicalValue(PwmChannels::StatusGreen), targetGreen),
      TransitionMath::easedChannel(
          pwm_->logicalValue(PwmChannels::StatusBlue), targetBlue));
}

uint8_t StatusLedController::scale(uint8_t value, uint8_t level) {
  return static_cast<uint8_t>(
      (static_cast<uint16_t>(value) * level + 127U) / 255U);
}
