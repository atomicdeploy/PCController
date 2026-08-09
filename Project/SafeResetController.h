#pragma once

#include <Arduino.h>

class PwmController;
class RelayController;
class StatusLedController;

// Coordinates an acknowledged reset without leaving a relay or MOSFET active.
// Status RGB is retained briefly for the shutdown cue, then cleared as well.
class SafeResetController {
public:
  void request(RelayController &relays, PwmController &pwm,
               StatusLedController &statusLeds,
               uint32_t now = millis());
  void service(RelayController &relays, PwmController &pwm,
               uint32_t now = millis());
  bool pending() const;

private:
  bool pending_ = false;
  uint32_t resetAt_ = 0;
};

// safeReset is the single acknowledged reboot coordinator.
extern SafeResetController safeReset;
