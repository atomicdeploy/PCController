#include <Arduino.h>

#include "Project/Core/RelayMotionMachine.h"
#include "Project/RelayController.h"

#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

struct TraceSink {
  std::uint8_t mask = 0;

  std::uint8_t activeRelayMask() const { return mask; }
  void commitRelayMask(std::uint8_t value, std::uint32_t) { mask = value; }
};

using CoreMachine = ControllerCore::RelayMotionMachine<TraceSink>;

void testPhysicalKeyWiringOrder() {
  arduino_mock::resetHardware();
  ShiftRegisters registers;
  registers.begin();

  for (std::uint8_t physical = 0; physical < 4; ++physical) {
    arduino_mock::shiftInput = static_cast<std::uint8_t>(~_BV(physical));
    registers.service();
    const std::uint8_t logical = static_cast<std::uint8_t>(3U - physical);
    require(registers.activeInputs() == _BV(logical),
            "physical K1..K4 wiring was not reversed into logical order");
  }

  arduino_mock::shiftInput = static_cast<std::uint8_t>(~_BV(4));
  registers.service();
  require(registers.activeInputs() == _BV(4),
          "reversing front keys disturbed the first system input");

  arduino_mock::shiftInput = 0xFF;
  registers.service();
  registers.setVirtualInput(0, true);
  require(registers.activeInputs() == _BV(0),
          "virtual key injection was incorrectly reversed with hardware");
}

void requireStatusEqual(const RelaySideStatus &actual,
                        const ControllerCore::RelaySideStatus &expected,
                        const char *message) {
  require(actual.requestedDirection == expected.requestedDirection &&
              actual.appliedDirection == expected.appliedDirection &&
              actual.phase == expected.phase &&
              actual.requestedEnabled == expected.requestedEnabled &&
              actual.appliedEnabled == expected.appliedEnabled,
          message);
}

void requireParity(const RelayController &avr, const CoreMachine &core,
                   const char *message) {
  require(avr.activeRelayMask() == core.activeRelayMask(), message);
  requireStatusEqual(avr.sideStatus(RelaySide::A),
                     core.sideStatus(ControllerCore::RelaySide::A), message);
  requireStatusEqual(avr.sideStatus(RelaySide::B),
                     core.sideStatus(ControllerCore::RelaySide::B), message);
  require(avr.motionAllowed() == core.motionAllowed(), message);
}

void testProductionAdapterMatchesCoreTrace() {
  arduino_mock::resetHardware();
  ShiftRegisters registers;
  RelayController avr(registers);
  TraceSink trace;
  CoreMachine core(trace);

  avr.begin(0);
  core.begin(0);
  avr.setBreakBeforeDirectionMs(37);
  core.setBreakBeforeDirectionMs(37);
  requireParity(avr, core, "adapter diverged at initialization");

  require(avr.requestSide(RelaySide::A, RelayDirection::Forward, true, 1) ==
              core.requestSide(ControllerCore::RelaySide::A,
                               ControllerCore::RelayDirection::Forward, true,
                               1),
          "adapter acceptance diverged on Side A forward");
  requireParity(avr, core, "adapter diverged after Side A forward");

  require(avr.requestSide(RelaySide::A, RelayDirection::Reverse, true, 2) ==
              core.requestSide(ControllerCore::RelaySide::A,
                               ControllerCore::RelayDirection::Reverse, true,
                               2),
          "adapter acceptance diverged on Side A reversal");
  requireParity(avr, core, "adapter diverged after reversal enable-off");
  avr.service(38);
  core.service(38);
  requireParity(avr, core, "adapter diverged before exact break deadline");
  avr.service(39);
  core.service(39);
  requireParity(avr, core, "adapter diverged after exact break deadline");

  require(avr.requestSide(RelaySide::B, RelayDirection::Reverse, true, 40) ==
              core.requestSide(ControllerCore::RelaySide::B,
                               ControllerCore::RelayDirection::Reverse, true,
                               40),
          "adapter acceptance diverged on Side B request");
  requireParity(avr, core, "adapter diverged during global interlock");
  avr.service(44);
  core.service(44);
  requireParity(avr, core, "adapter ended global interlock early");
  avr.service(45);
  core.service(45);
  requireParity(avr, core, "adapter diverged after global interlock");

  avr.setRetainDirectionOnStop(true);
  core.setRetainDirectionOnStop(true);
  avr.setMotionAllowed(false, 50);
  core.setMotionAllowed(false, 50);
  requireParity(avr, core, "adapter diverged on policy-safe output stop");

  arduino_mock::nowMillis = 51;
  require(avr.setGeneral(2, true) == core.setGeneral(2, true, 51),
          "adapter acceptance diverged on independent R7");
  requireParity(avr, core, "adapter diverged on independent relay update");

  avr.allOff(60);
  core.allOff(60);
  requireParity(avr, core, "adapter diverged on two-frame all-off");
}

} // namespace

int main() {
  try {
    testPhysicalKeyWiringOrder();
    testProductionAdapterMatchesCoreTrace();
    std::cout << "relay_controller_adapter_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "relay_controller_adapter_tests: " << error.what() << '\n';
    return 1;
  }
}
