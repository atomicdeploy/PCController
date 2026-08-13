#include "RelayController.h"

RelayController::RelayController(ShiftRegisters &registers)
    : sink_(registers), machine_(sink_) {}

void RelayController::begin(uint32_t now) { machine_.begin(now); }

void RelayController::allOff(uint32_t now) { machine_.allOff(now); }

void RelayController::service(uint32_t now) { machine_.service(now); }

void RelayController::setMotionAllowed(bool allowed, uint32_t now) {
  machine_.setMotionAllowed(allowed, now);
}

bool RelayController::requestSide(RelaySide side, RelayDirection direction,
                                  bool enabled, uint32_t now) {
  return machine_.requestSide(side, direction, enabled, now);
}

void RelayController::stopSide(RelaySide side, uint32_t now) {
  machine_.stopSide(side, now);
}

bool RelayController::setGeneral(uint8_t generalIndex, bool active) {
  return machine_.setGeneral(generalIndex, active, millis());
}

bool RelayController::generalActive(uint8_t generalIndex) const {
  return machine_.generalActive(generalIndex);
}

bool RelayController::requestRelayForTest(uint8_t relayNumber, bool active,
                                          uint32_t now) {
  return machine_.requestRelay(relayNumber, active, now);
}

RelaySideStatus RelayController::sideStatus(RelaySide side) const {
  return machine_.sideStatus(side);
}

bool RelayController::sideBusy(RelaySide side) const {
  return machine_.sideBusy(side);
}

bool RelayController::anySideBusy() const { return machine_.anySideBusy(); }

uint8_t RelayController::activeRelayMask() const {
  return sink_.activeRelayMask();
}
