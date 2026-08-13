#pragma once

#include <stdint.h>

namespace ControllerCore {

constexpr uint8_t RelayBreakBeforeDirectionMs = 1;
constexpr uint8_t RelayDirectionInterlockMs = 5;

// Fixed ControllerBoardMini relay mapping. Motion uses direction/enable pairs;
// the remaining four outputs are independent user/general relays.
namespace RelayOutputs {
enum : uint8_t {
  SideADirection = 0,
  SideAEnable = 1,
  SideBDirection = 2,
  SideBEnable = 3,
  GeneralFirst = 4,
  GeneralCount = 4,
  EnableMask = (1U << SideAEnable) | (1U << SideBEnable),
};
} // namespace RelayOutputs

// RelaySide identifies one of the two motion direction/enable pairs.
enum class RelaySide : uint8_t {
  A = 0,
  B = 1,
};

// Forward leaves the direction relay inactive; Reverse energizes it.
enum class RelayDirection : uint8_t {
  Forward = 0,
  Reverse,
};

// A side is either settled or waiting through its configured reversal break.
enum class RelaySequencePhase : uint8_t {
  Idle = 0,
  BreakBeforeDirection,
};

// Snapshot of requested versus electrically-applied motion state.
struct RelaySideStatus {
  RelayDirection requestedDirection;
  RelayDirection appliedDirection;
  RelaySequencePhase phase;
  bool requestedEnabled;
  bool appliedEnabled;
};

// RelayMotionMachine is the allocation-free, target-neutral motion sequencer.
// Sink must provide activeRelayMask() and commitRelayMask(mask, now).
template <typename Sink> class RelayMotionMachine {
public:
  static constexpr uint8_t BreakBeforeDirectionMs =
      RelayBreakBeforeDirectionMs;
  static constexpr uint8_t DirectionInterlockMs = RelayDirectionInterlockMs;

  explicit RelayMotionMachine(Sink &sink) : sink_(sink) {}

  // Start from a two-frame all-off state so power always drops before direction.
  void begin(uint32_t now) { allOff(now); }

  // Force both enables low, commit that frame, then clear every relay output.
  void allOff(uint32_t now) {
    activeRelayMask_ = sink_.activeRelayMask();
    activeRelayMask_ = static_cast<uint8_t>(activeRelayMask_ &
                                             ~RelayOutputs::EnableMask);
    commit(now);
    activeRelayMask_ = 0;
    commit(now);

    for (uint8_t index = 0; index < 2; ++index) {
      sides_[index].flags = 0;
      sides_[index].phaseDeadline = now;
    }
    nextDirectionChangeAt_ = now;
  }

  // Service at most one direction relay per pass, preserving the 5 ms interlock.
  void service(uint32_t now) {
    bool directionChangedThisService = false;
    serviceSide(RelaySide::A, now, directionChangedThisService);
    serviceSide(RelaySide::B, now, directionChangedThisService);
  }

  // Revoking motion policy immediately applies the configured safe stop path.
  void setMotionAllowed(bool allowed, uint32_t now) {
    setFlag(FlagMotionAllowed, allowed);
    if (!allowed) {
      stopSide(RelaySide::A, now);
      stopSide(RelaySide::B, now);
    }
  }

  bool motionAllowed() const { return flag(FlagMotionAllowed); }

  void setBreakBeforeDirectionMs(uint8_t value) {
    breakBeforeDirectionMs_ = value == 0 ? BreakBeforeDirectionMs : value;
  }

  void setRetainDirectionOnStop(bool retain) {
    setFlag(FlagRetainDirectionOnStop, retain);
  }

  // Requests are nonblocking: disable -> break -> direction -> enable.
  bool requestSide(RelaySide side, RelayDirection direction, bool enabled,
                   uint32_t now) {
    if (!validSide(side) || (enabled && !motionAllowed())) {
      return false;
    }
    SideState &state = sides_[sideIndex(side)];
    setRequestedDirection(state, direction);
    setRequestedEnabled(state, enabled);

    bool directionChangedThisService = false;
    serviceSide(side, now, directionChangedThisService);
    return true;
  }

  // Stops remain available even after a motion policy denial.
  void stopSide(RelaySide side, uint32_t now) {
    if (!validSide(side)) {
      return;
    }
    SideState &state = sides_[sideIndex(side)];
    setRequestedEnabled(state, false);
    if (!flag(FlagRetainDirectionOnStop)) {
      setRequestedDirection(state, RelayDirection::Forward);
    }
    bool directionChangedThisService = false;
    serviceSide(side, now, directionChangedThisService);
  }

  // General index 0..3 maps directly to R5..R8 and bypasses motion policy.
  bool setGeneral(uint8_t generalIndex, bool active, uint32_t now) {
    if (generalIndex >= RelayOutputs::GeneralCount) {
      return false;
    }
    const uint8_t bit = static_cast<uint8_t>(RelayOutputs::GeneralFirst +
                                              generalIndex);
    if (bitActive(bit) == active) {
      return true;
    }
    setBit(bit, active);
    commit(now);
    return true;
  }

  bool generalActive(uint8_t generalIndex) const {
    if (generalIndex >= RelayOutputs::GeneralCount) {
      return false;
    }
    return bitActive(static_cast<uint8_t>(RelayOutputs::GeneralFirst +
                                          generalIndex));
  }

  // Relay-by-relay test route: motion bits remain sequenced through requestSide.
  bool requestRelay(uint8_t relayNumber, bool active, uint32_t now) {
    if (relayNumber < 1 || relayNumber > 8 ||
        (active && relayNumber <= 4 && !motionAllowed())) {
      return false;
    }
    switch (relayNumber) {
    case 1:
      return requestSide(RelaySide::A,
                         active ? RelayDirection::Reverse
                                : RelayDirection::Forward,
                         requestedEnabled(sides_[0]), now);
    case 2:
      return requestSide(RelaySide::A, requestedDirection(sides_[0]), active,
                         now);
    case 3:
      return requestSide(RelaySide::B,
                         active ? RelayDirection::Reverse
                                : RelayDirection::Forward,
                         requestedEnabled(sides_[1]), now);
    case 4:
      return requestSide(RelaySide::B, requestedDirection(sides_[1]), active,
                         now);
    default:
      return setGeneral(static_cast<uint8_t>(relayNumber - 5), active, now);
    }
  }

  RelaySideStatus sideStatus(RelaySide side) const {
    const SideState &state = sides_[sideIndex(side)];
    RelaySideStatus result = {requestedDirection(state),
                               appliedDirection(state), phase(state),
                               requestedEnabled(state),
                               appliedEnabled(state)};
    return result;
  }

  bool sideBusy(RelaySide side) const {
    return phase(sides_[sideIndex(side)]) != RelaySequencePhase::Idle;
  }

  bool anySideBusy() const {
    return sideBusy(RelaySide::A) || sideBusy(RelaySide::B);
  }

  uint8_t activeRelayMask() const { return activeRelayMask_; }

private:
  // Each side is exactly one packed flag byte plus its rollover-safe deadline.
  struct SideState {
    uint8_t flags = 0;
    uint32_t phaseDeadline = 0;
  };

  enum : uint8_t {
    SideRequestedDirection = 1U << 0,
    SideAppliedDirection = 1U << 1,
    SideRequestedEnabled = 1U << 2,
    SideAppliedEnabled = 1U << 3,
    SideBreakBeforeDirection = 1U << 4,
    FlagMotionAllowed = 1U << 0,
    FlagRetainDirectionOnStop = 1U << 1,
  };

  static bool timeReached(uint32_t now, uint32_t deadline) {
    return static_cast<int32_t>(now - deadline) >= 0;
  }

  static bool validSide(RelaySide side) {
    return side == RelaySide::A || side == RelaySide::B;
  }

  static uint8_t sideIndex(RelaySide side) {
    return side == RelaySide::B ? 1U : 0U;
  }

  static uint8_t directionBit(RelaySide side) {
    return side == RelaySide::B ? RelayOutputs::SideBDirection
                                : RelayOutputs::SideADirection;
  }

  static uint8_t enableBit(RelaySide side) {
    return side == RelaySide::B ? RelayOutputs::SideBEnable
                                : RelayOutputs::SideAEnable;
  }

  static bool sideFlag(const SideState &state, uint8_t mask) {
    return (state.flags & mask) != 0;
  }

  static void setSideFlag(SideState &state, uint8_t mask, bool enabled) {
    if (enabled) {
      state.flags = static_cast<uint8_t>(state.flags | mask);
    } else {
      state.flags = static_cast<uint8_t>(state.flags & ~mask);
    }
  }

  static RelayDirection requestedDirection(const SideState &state) {
    return sideFlag(state, SideRequestedDirection) ? RelayDirection::Reverse
                                                    : RelayDirection::Forward;
  }

  static RelayDirection appliedDirection(const SideState &state) {
    return sideFlag(state, SideAppliedDirection) ? RelayDirection::Reverse
                                                  : RelayDirection::Forward;
  }

  static bool requestedEnabled(const SideState &state) {
    return sideFlag(state, SideRequestedEnabled);
  }

  static bool appliedEnabled(const SideState &state) {
    return sideFlag(state, SideAppliedEnabled);
  }

  static RelaySequencePhase phase(const SideState &state) {
    return sideFlag(state, SideBreakBeforeDirection)
               ? RelaySequencePhase::BreakBeforeDirection
               : RelaySequencePhase::Idle;
  }

  static void setRequestedDirection(SideState &state,
                                    RelayDirection direction) {
    setSideFlag(state, SideRequestedDirection,
                direction == RelayDirection::Reverse);
  }

  static void setAppliedDirection(SideState &state, RelayDirection direction) {
    setSideFlag(state, SideAppliedDirection,
                direction == RelayDirection::Reverse);
  }

  static void setRequestedEnabled(SideState &state, bool enabled) {
    setSideFlag(state, SideRequestedEnabled, enabled);
  }

  static void setAppliedEnabled(SideState &state, bool enabled) {
    setSideFlag(state, SideAppliedEnabled, enabled);
  }

  static void setPhase(SideState &state, RelaySequencePhase value) {
    setSideFlag(state, SideBreakBeforeDirection,
                value == RelaySequencePhase::BreakBeforeDirection);
  }

  bool flag(uint8_t mask) const { return (flags_ & mask) != 0; }

  void setFlag(uint8_t mask, bool enabled) {
    if (enabled) {
      flags_ = static_cast<uint8_t>(flags_ | mask);
    } else {
      flags_ = static_cast<uint8_t>(flags_ & ~mask);
    }
  }

  bool bitActive(uint8_t bit) const {
    return (activeRelayMask_ & static_cast<uint8_t>(1U << bit)) != 0;
  }

  void setBit(uint8_t bit, bool active) {
    const uint8_t bitMask = static_cast<uint8_t>(1U << bit);
    if (active) {
      activeRelayMask_ = static_cast<uint8_t>(activeRelayMask_ | bitMask);
    } else {
      activeRelayMask_ = static_cast<uint8_t>(activeRelayMask_ & ~bitMask);
    }
  }

  void commit(uint32_t now) { sink_.commitRelayMask(activeRelayMask_, now); }

  void serviceSide(RelaySide side, uint32_t now,
                   bool &directionChangedThisService) {
    SideState &state = sides_[sideIndex(side)];

    if (phase(state) == RelaySequencePhase::BreakBeforeDirection) {
      if (!timeReached(now, state.phaseDeadline)) {
        return;
      }
      setPhase(state, RelaySequencePhase::Idle);
    }

    // Disabling is always physically committed before a direction change.
    if (appliedEnabled(state) &&
        (!requestedEnabled(state) ||
         requestedDirection(state) != appliedDirection(state))) {
      setBit(enableBit(side), false);
      commit(now);
      setAppliedEnabled(state, false);
      setPhase(state, RelaySequencePhase::BreakBeforeDirection);
      state.phaseDeadline = now + breakBeforeDirectionMs_;
      return;
    }

    if (!appliedEnabled(state) &&
        requestedDirection(state) != appliedDirection(state)) {
      if (directionChangedThisService ||
          !timeReached(now, nextDirectionChangeAt_)) {
        return;
      }
      setBit(directionBit(side),
             requestedDirection(state) == RelayDirection::Reverse);
      commit(now);
      setAppliedDirection(state, requestedDirection(state));
      directionChangedThisService = true;
      nextDirectionChangeAt_ = now + DirectionInterlockMs;
    }

    if (!appliedEnabled(state) && requestedEnabled(state) &&
        phase(state) == RelaySequencePhase::Idle) {
      setBit(enableBit(side), true);
      commit(now);
      setAppliedEnabled(state, true);
    }
  }

  Sink &sink_;
  SideState sides_[2];
  uint32_t nextDirectionChangeAt_ = 0;
  uint8_t activeRelayMask_ = 0;
  uint8_t breakBeforeDirectionMs_ = BreakBeforeDirectionMs;
  uint8_t flags_ = FlagMotionAllowed;
};

} // namespace ControllerCore
