#pragma once

#include <Arduino.h>

#include "../LocalLib/ShiftRegisters.h"

namespace RelayOutputs {

// The 74HC595 and relay stages are active-low. ShiftRegisters::setOutput()
// already translates logical active=true into a cleared output bit.
constexpr uint8_t R1SideADirection = 0;
constexpr uint8_t R2SideAEnable = 1;
constexpr uint8_t R3SideBDirection = 2;
constexpr uint8_t R4SideBEnable = 3;
constexpr uint8_t R5General1 = 4;
constexpr uint8_t R6General2 = 5;
constexpr uint8_t R7General3 = 6;
constexpr uint8_t R8General4 = 7;
constexpr uint8_t GeneralCount = 4;

} // namespace RelayOutputs

enum class RelaySide : uint8_t {
  A = 0,
  B = 1,
};

// Forward is the de-energized direction-relay state. Reverse energizes the
// direction relay. Neither direction is powered unless the side enable relay
// is also active.
enum class RelayDirection : uint8_t {
  Forward = 0,
  Reverse,
};

enum class RelaySequencePhase : uint8_t {
  Idle = 0,
  BreakBeforeDirection,
  DirectionSettling,
};

struct RelaySideStatus {
  RelayDirection requestedDirection;
  RelayDirection appliedDirection;
  RelaySequencePhase phase;
  bool requestedEnabled;
  bool appliedEnabled;
};

class RelayController {
public:
  explicit RelayController(ShiftRegisters &registers);

  // Forces enable relays off before clearing direction/general relays.
  void begin(uint32_t now = millis());
  void allOff(uint32_t now = millis());
  void service(uint32_t now = millis());
  // Revoking policy is fail-safe and immediately drops both motion enables.
  void setMotionAllowed(bool allowed, uint32_t now = millis());
  bool motionAllowed() const { return motionAllowed_; }
  void setBreakBeforeDirectionMs(uint8_t value) {
    breakBeforeDirectionMs_ = value == 0 ? 1 : value;
  }

  // Requests are nonblocking. A direction reversal while enabled is sequenced
  // as disable -> dead time -> direction -> settle time -> enable.
  bool requestSide(RelaySide side, RelayDirection direction, bool enabled,
                   uint32_t now = millis());
  void stopSide(RelaySide side, uint32_t now = millis());

  // General index 0..3 maps to R5..R8.
  bool setGeneral(uint8_t generalIndex, bool active);
  bool generalActive(uint8_t generalIndex) const;

  // Safe relay-by-relay test entry point. R1/R3 alter the requested direction;
  // R2/R4 alter enable, and R5..R8 are applied directly.
  bool requestRelayForTest(uint8_t relayNumber, bool active,
                           uint32_t now = millis());

  RelaySideStatus sideStatus(RelaySide side) const;
  bool sideBusy(RelaySide side) const;
  bool anySideBusy() const;
  uint8_t activeRelayMask() const;

  static constexpr uint8_t BreakBeforeDirectionMs = 1;
  static constexpr uint16_t DirectionSettleMs = 50;
  static constexpr uint16_t DirectionInterlockMs = 5;

private:
  struct SideState {
    RelayDirection requestedDirection = RelayDirection::Forward;
    RelayDirection appliedDirection = RelayDirection::Forward;
    RelaySequencePhase phase = RelaySequencePhase::Idle;
    bool requestedEnabled = false;
    bool appliedEnabled = false;
    uint32_t phaseDeadline = 0;
  };

  static uint8_t sideIndex(RelaySide side);
  static uint8_t directionBit(RelaySide side);
  static uint8_t enableBit(RelaySide side);

  void serviceSide(RelaySide side, uint32_t now,
                   bool &directionChangedThisService);
  void setRelay(uint8_t bit, bool active);
  void commit();

  ShiftRegisters &registers_;
  SideState sides_[2];
  uint32_t nextDirectionChangeAt_ = 0;
  uint8_t breakBeforeDirectionMs_ = BreakBeforeDirectionMs;
  bool motionAllowed_ = true;
};
