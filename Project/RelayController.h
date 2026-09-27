#pragma once

#include <Arduino.h>

#include "../LocalLib/ShiftRegisters.h"
#include "Core/RelayMotionMachine.h"

// Preserve the firmware-facing names while the sequencing implementation lives
// in the target-neutral ControllerCore namespace.
namespace RelayOutputs {
constexpr uint8_t R1SideADirection = ControllerCore::RelayOutputs::SideADirection;
constexpr uint8_t R2SideAEnable = ControllerCore::RelayOutputs::SideAEnable;
constexpr uint8_t R3SideBDirection = ControllerCore::RelayOutputs::SideBDirection;
constexpr uint8_t R4SideBEnable = ControllerCore::RelayOutputs::SideBEnable;
constexpr uint8_t R5General1 = ControllerCore::RelayOutputs::GeneralFirst;
constexpr uint8_t R6General2 = R5General1 + 1U;
constexpr uint8_t R7General3 = R5General1 + 2U;
constexpr uint8_t R8General4 = R5General1 + 3U;
constexpr uint8_t GeneralCount = ControllerCore::RelayOutputs::GeneralCount;
} // namespace RelayOutputs

using RelaySide = ControllerCore::RelaySide;
using RelayDirection = ControllerCore::RelayDirection;
using RelaySequencePhase = ControllerCore::RelaySequencePhase;
using RelaySideStatus = ControllerCore::RelaySideStatus;

// ShiftRegisterRelaySink is the AVR-only output adapter for the shared core.
class ShiftRegisterRelaySink {
public:
  explicit ShiftRegisterRelaySink(ShiftRegisters &registers)
      : registers_(registers) {}

  uint8_t activeRelayMask() const { return registers_.activeOutputs(); }
  void commitRelayMask(uint8_t activeMask, uint32_t) {
    registers_.setActiveOutputs(activeMask);
    registers_.service();
  }

private:
  ShiftRegisters &registers_;
};

// RelayController is the thin AVR adapter for the portable motion sequencer.
class RelayController {
public:
  explicit RelayController(ShiftRegisters &registers);

  // Forces enable relays off before clearing direction/general relays.
  void begin(uint32_t now = millis());
  void allOff(uint32_t now = millis());
  void service(uint32_t now = millis());
  // Revoking policy is fail-safe and immediately drops both motion enables.
  void setMotionAllowed(bool allowed, uint32_t now = millis());
  bool motionAllowed() const { return machine_.motionAllowed(); }
  void setBreakBeforeDirectionMs(uint8_t value) {
    machine_.setBreakBeforeDirectionMs(value);
  }
  void setRetainDirectionOnStop(bool retain) {
    machine_.setRetainDirectionOnStop(retain);
  }

  // Requests are nonblocking. A live reversal is sequenced strictly as
  // disable -> configured break -> direction -> enable.
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

  static constexpr uint8_t BreakBeforeDirectionMs =
      ControllerCore::RelayBreakBeforeDirectionMs;
  static constexpr uint8_t DirectionInterlockMs =
      ControllerCore::RelayDirectionInterlockMs;

private:
  ShiftRegisterRelaySink sink_;
  ControllerCore::RelayMotionMachine<ShiftRegisterRelaySink> machine_;
};
