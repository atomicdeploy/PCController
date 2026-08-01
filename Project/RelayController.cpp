#include "RelayController.h"

namespace {

bool timeReached(uint32_t now, uint32_t deadline) {
  return static_cast<int32_t>(now - deadline) >= 0;
}

} // namespace

RelayController::RelayController(ShiftRegisters &registers)
    : registers_(registers) {}

void RelayController::begin(uint32_t now) { allOff(now); }

void RelayController::allOff(uint32_t now) {
  // Break power first. Commit this frame before changing either direction
  // relay, even if some other code changed the shared shift-register cache.
  registers_.setOutput(RelayOutputs::R2SideAEnable, false);
  registers_.setOutput(RelayOutputs::R4SideBEnable, false);
  commit();

  registers_.allOutputsOff();
  commit();

  for (SideState &side : sides_) {
    side.requestedDirection = RelayDirection::Forward;
    side.appliedDirection = RelayDirection::Forward;
    side.phase = RelaySequencePhase::Idle;
    side.requestedEnabled = false;
    side.appliedEnabled = false;
    side.phaseDeadline = now;
  }
  nextDirectionChangeAt_ = now;
}

void RelayController::service(uint32_t now) {
  // Avoid switching two mechanical direction relays in the exact same service
  // pass. The second side is handled on the next loop iteration.
  bool directionChangedThisService = false;
  serviceSide(RelaySide::A, now, directionChangedThisService);
  serviceSide(RelaySide::B, now, directionChangedThisService);
}

bool RelayController::requestSide(RelaySide side, RelayDirection direction,
                                  bool enabled, uint32_t now) {
  if ((side != RelaySide::A && side != RelaySide::B) ||
      (enabled && !motionAllowed_)) {
    return false;
  }
  SideState &state = sides_[sideIndex(side)];
  state.requestedDirection = direction;
  state.requestedEnabled = enabled;

  bool directionChangedThisService = false;
  serviceSide(side, now, directionChangedThisService);
  return true;
}

void RelayController::stopSide(RelaySide side, uint32_t now) {
  if (side != RelaySide::A && side != RelaySide::B) {
    return;
  }
  SideState &state = sides_[sideIndex(side)];
  state.requestedEnabled = false;
  bool directionChangedThisService = false;
  serviceSide(side, now, directionChangedThisService);
}

bool RelayController::setGeneral(uint8_t generalIndex, bool active) {
  if (generalIndex >= RelayOutputs::GeneralCount) {
    return false;
  }
  const uint8_t bit =
      static_cast<uint8_t>(RelayOutputs::R5General1 + generalIndex);
  const bool changed =
      ((registers_.activeOutputs() & _BV(bit)) != 0) != active;
  if (changed) {
    setRelay(bit, active);
    commit();
  }
  return true;
}

bool RelayController::generalActive(uint8_t generalIndex) const {
  if (generalIndex >= RelayOutputs::GeneralCount) {
    return false;
  }
  const uint8_t bit =
      static_cast<uint8_t>(RelayOutputs::R5General1 + generalIndex);
  return (registers_.activeOutputs() & _BV(bit)) != 0;
}

bool RelayController::requestRelayForTest(uint8_t relayNumber, bool active,
                                          uint32_t now) {
  if (relayNumber < 1 || relayNumber > 8 ||
      (active && relayNumber <= 4 && !motionAllowed_)) {
    return false;
  }
  switch (relayNumber) {
    case 1:
      return requestSide(RelaySide::A,
                         active ? RelayDirection::Reverse
                                : RelayDirection::Forward,
                         sides_[0].requestedEnabled, now);
    case 2:
      return requestSide(RelaySide::A, sides_[0].requestedDirection, active,
                         now);
    case 3:
      return requestSide(RelaySide::B,
                         active ? RelayDirection::Reverse
                                : RelayDirection::Forward,
                         sides_[1].requestedEnabled, now);
    case 4:
      return requestSide(RelaySide::B, sides_[1].requestedDirection, active,
                         now);
    default:
      return setGeneral(static_cast<uint8_t>(relayNumber - 5), active);
  }
}

RelaySideStatus RelayController::sideStatus(RelaySide side) const {
  const SideState &state = sides_[sideIndex(side)];
  return {
      state.requestedDirection,
      state.appliedDirection,
      state.phase,
      state.requestedEnabled,
      state.appliedEnabled,
  };
}

bool RelayController::sideBusy(RelaySide side) const {
  return sides_[sideIndex(side)].phase != RelaySequencePhase::Idle;
}

bool RelayController::anySideBusy() const {
  return sideBusy(RelaySide::A) || sideBusy(RelaySide::B);
}

uint8_t RelayController::activeRelayMask() const {
  return registers_.activeOutputs();
}

uint8_t RelayController::sideIndex(RelaySide side) {
  return side == RelaySide::B ? 1 : 0;
}

uint8_t RelayController::directionBit(RelaySide side) {
  return side == RelaySide::B ? RelayOutputs::R3SideBDirection
                              : RelayOutputs::R1SideADirection;
}

uint8_t RelayController::enableBit(RelaySide side) {
  return side == RelaySide::B ? RelayOutputs::R4SideBEnable
                              : RelayOutputs::R2SideAEnable;
}

void RelayController::serviceSide(RelaySide side, uint32_t now,
                                  bool &directionChangedThisService) {
  SideState &state = sides_[sideIndex(side)];

  switch (state.phase) {
    case RelaySequencePhase::BreakBeforeDirection:
    case RelaySequencePhase::DirectionSettling:
      if (!timeReached(now, state.phaseDeadline)) {
        return;
      }
      if (state.requestedDirection != state.appliedDirection) {
        if (directionChangedThisService ||
            !timeReached(now, nextDirectionChangeAt_)) {
          return;
        }
        setRelay(directionBit(side),
                 state.requestedDirection == RelayDirection::Reverse);
        commit();
        state.appliedDirection = state.requestedDirection;
        directionChangedThisService = true;
        nextDirectionChangeAt_ = now + DirectionInterlockMs;
        state.phaseDeadline = now + DirectionSettleMs;
        return;
      }
      state.phase = RelaySequencePhase::Idle;
      break;

    case RelaySequencePhase::Idle:
      break;
  }

  // Disabling always has priority and is committed before direction changes.
  if (state.appliedEnabled &&
      (!state.requestedEnabled ||
       state.requestedDirection != state.appliedDirection)) {
    setRelay(enableBit(side), false);
    commit();
    state.appliedEnabled = false;
    state.phase = RelaySequencePhase::BreakBeforeDirection;
    state.phaseDeadline = now + breakBeforeDirectionMs_;
    return;
  }

  if (!state.appliedEnabled &&
      state.requestedDirection != state.appliedDirection) {
    if (directionChangedThisService ||
        !timeReached(now, nextDirectionChangeAt_)) {
      return;
    }
    setRelay(directionBit(side),
             state.requestedDirection == RelayDirection::Reverse);
    commit();
    state.appliedDirection = state.requestedDirection;
    directionChangedThisService = true;
    nextDirectionChangeAt_ = now + DirectionInterlockMs;
    state.phase = RelaySequencePhase::DirectionSettling;
    state.phaseDeadline = now + DirectionSettleMs;
    return;
  }

  if (!state.appliedEnabled && state.requestedEnabled &&
      state.phase == RelaySequencePhase::Idle) {
    setRelay(enableBit(side), true);
    commit();
    state.appliedEnabled = true;
  }
}

void RelayController::setRelay(uint8_t bit, bool active) {
  registers_.setOutput(bit, active);
}

void RelayController::commit() { registers_.service(); }
