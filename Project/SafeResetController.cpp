#include "SafeResetController.h"

#include <avr/wdt.h>

#include "PwmController.h"
#include "RelayController.h"
#include "StatusLedController.h"

namespace {
// Reset cue duration and the mask covering every non-status PWM output.
constexpr uint16_t ResetCueMs = 240;
constexpr uint16_t NonStatusPwmMask = 0x1FFFU; // PWM channels 0..12.
}

SafeResetController safeReset;

void SafeResetController::request(RelayController &relays,
                                  PwmController &pwm,
                                  StatusLedController &statusLeds,
                                  uint32_t now) {
  relays.allOff(now);
  (void)pwm.clearMask(NonStatusPwmMask);
  statusLeds.playCue(StatusLedCue::Reset, ResetCueMs, now);
  resetAt_ = now + ResetCueMs;
  pending_ = true;
}

void SafeResetController::service(RelayController &relays,
                                  PwmController &pwm, uint32_t now) {
  if (!pending_ ||
      static_cast<int32_t>(now - resetAt_) < 0) {
    return;
  }

  relays.allOff(now);
  (void)pwm.tryAllOff();
  Serial.flush();
  wdt_enable(WDTO_15MS);
  for (;;) {
  }
}

bool SafeResetController::pending() const { return pending_; }
