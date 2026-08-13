#include "Project/Core/RelayMotionMachine.h"

#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

using ControllerCore::RelayDirection;
using ControllerCore::RelayMotionMachine;
using ControllerCore::RelaySequencePhase;
using ControllerCore::RelaySide;

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

struct TraceSink {
  struct Frame {
    std::uint32_t at;
    std::uint8_t mask;
  };

  std::uint8_t mask = 0;
  std::vector<Frame> frames;

  std::uint8_t activeRelayMask() const { return mask; }
  void commitRelayMask(std::uint8_t value, std::uint32_t now) {
    mask = value;
    frames.push_back({now, value});
  }
  void clearFrames() { frames.clear(); }
};

using Machine = RelayMotionMachine<TraceSink>;

void expectFrame(const TraceSink &sink, std::size_t index, std::uint32_t at,
                 std::uint8_t mask, const char *message) {
  require(index < sink.frames.size(), message);
  require(sink.frames[index].at == at && sink.frames[index].mask == mask,
          message);
}

void testTwoFrameAllOffAndGeneralRelays() {
  TraceSink sink;
  sink.mask = 0xFF;
  Machine machine(sink);
  machine.begin(100);

  require(sink.frames.size() == 2,
          "all-off must commit the required enable-off and fully-off frames");
  expectFrame(sink, 0, 100, 0xF5,
              "all-off did not remove only R2/R4 before the final frame");
  expectFrame(sink, 1, 100, 0,
              "all-off did not clear the complete relay mask");
  require(machine.activeRelayMask() == 0 && sink.mask == 0,
          "all-off did not leave the mask electrically clear");

  sink.clearFrames();
  require(machine.setGeneral(0, true, 101), "R5 request was rejected");
  require(machine.setGeneral(3, true, 102), "R8 request was rejected");
  require(machine.generalActive(0) && machine.generalActive(3),
          "general relay state did not persist in the core mask");
  require(machine.activeRelayMask() == 0x90,
          "general relays did not map to R5/R8");
  require(!machine.setGeneral(4, true, 103),
          "out-of-range general relay was accepted");
}

void testExactReversalAndRetainedDirection() {
  TraceSink sink;
  Machine machine(sink);
  machine.begin(0);
  machine.setBreakBeforeDirectionMs(37);
  sink.clearFrames();

  require(machine.requestSide(RelaySide::A, RelayDirection::Forward, true, 1),
          "forward request was rejected");
  require(sink.mask == 0x02, "R2 did not enable Side A forward motion");
  require(machine.requestSide(RelaySide::A, RelayDirection::Reverse, true, 2),
          "reversal request was rejected");
  require(sink.mask == 0,
          "reversal did not drop Side A enable before changing direction");
  require(machine.sideStatus(RelaySide::A).phase ==
              RelaySequencePhase::BreakBeforeDirection,
          "reversal did not expose the active break phase");

  machine.service(38);
  require(sink.mask == 0, "configured 37 ms break ended one ms early");
  machine.service(39);
  require(sink.mask == 0x03,
          "direction then enable frames did not settle Side A reverse");
  require(sink.frames.size() == 4,
          "reversal did not produce enable-off, direction, enable trace");
  expectFrame(sink, 0, 1, 0x02, "forward enable trace drifted");
  expectFrame(sink, 1, 2, 0, "reversal enable-off trace drifted");
  expectFrame(sink, 2, 39, 0x01, "reversal direction trace drifted");
  expectFrame(sink, 3, 39, 0x03, "reversal re-enable trace drifted");

  machine.setRetainDirectionOnStop(true);
  machine.stopSide(RelaySide::A, 50);
  require(sink.mask == 0x01,
          "output-only stop failed to retain the applied direction relay");
  machine.setRetainDirectionOnStop(false);
  machine.stopSide(RelaySide::A, 55);
  machine.service(87);
  require(sink.mask == 0,
          "full-off stop failed to clear the retained direction relay");
}

void testBreakConfigurationBoundaries() {
  {
    TraceSink sink;
    Machine machine(sink);
    machine.begin(0);
    machine.setBreakBeforeDirectionMs(0);
    sink.clearFrames();

    require(machine.requestSide(RelaySide::A, RelayDirection::Forward, true,
                                1),
            "zero-break fixture could not start Side A");
    require(machine.requestSide(RelaySide::A, RelayDirection::Reverse, true,
                                2),
            "zero-break fixture could not request a reversal");
    machine.service(2);
    require(sink.mask == 0,
            "zero break bypassed the required one ms de-energized frame");
    machine.service(3);
    require(sink.mask == 0x03,
            "zero break did not clamp to exactly one ms before re-enable");
  }

  {
    TraceSink sink;
    Machine machine(sink);
    machine.begin(0);
    machine.setBreakBeforeDirectionMs(255);
    sink.clearFrames();

    require(machine.requestSide(RelaySide::A, RelayDirection::Forward, true,
                                1),
            "max-break fixture could not start Side A");
    require(machine.requestSide(RelaySide::A, RelayDirection::Reverse, true,
                                2),
            "max-break fixture could not request a reversal");
    machine.service(256);
    require(sink.mask == 0,
            "255 ms break ended one ms early");
    machine.service(257);
    require(sink.mask == 0x03,
            "255 ms break did not settle at its exact deadline");
  }
}

void testCrossSideInterlockAndPolicyStop() {
  TraceSink sink;
  Machine machine(sink);
  machine.begin(0);
  sink.clearFrames();

  require(machine.requestSide(RelaySide::A, RelayDirection::Reverse, true, 1),
          "Side A reverse request was rejected");
  require(machine.requestSide(RelaySide::B, RelayDirection::Reverse, true, 1),
          "Side B reverse request was rejected");
  require(sink.mask == 0x03,
          "cross-side direction interlock allowed an immediate B switch");
  machine.service(5);
  require(sink.mask == 0x03, "five ms direction interlock ended early");
  machine.service(6);
  require(sink.mask == 0x0F,
          "Side B did not change direction and enable after the interlock");

  // Queue both live reversals at once. One side may change direction per pass.
  require(machine.requestSide(RelaySide::A, RelayDirection::Forward, true, 10),
          "Side A queued reversal was rejected");
  require(machine.requestSide(RelaySide::B, RelayDirection::Forward, true, 10),
          "Side B queued reversal was rejected");
  require(sink.mask == 0x05,
          "both enables were not removed before the paired reversals");
  machine.service(11);
  require(sink.mask == 0x06,
          "first due side did not change direction then re-enable alone");
  machine.service(15);
  require(sink.mask == 0x06,
          "second direction changed before its global five ms interlock");
  machine.service(16);
  require(sink.mask == 0x0A,
          "second side did not settle after the global direction interlock");

  machine.setRetainDirectionOnStop(true);
  machine.setMotionAllowed(false, 20);
  require((sink.mask & 0x0A) == 0,
          "policy revoke did not immediately drop both motion enables");
  require(!machine.requestSide(RelaySide::A, RelayDirection::Forward, true, 21),
          "denied policy allowed a motion enable request");
  require(!machine.requestRelay(4, true, 21),
          "denied policy allowed the relay-test motion route");
  require(machine.setGeneral(1, true, 22),
          "motion policy incorrectly blocked independent R6 control");
  require((sink.mask & 0x20) != 0,
          "independent R6 did not remain available during policy deny");
}

void testPendingBreakAllOffAndRollover() {
  TraceSink sink;
  Machine machine(sink);
  machine.begin(0);
  machine.setBreakBeforeDirectionMs(37);
  sink.clearFrames();

  require(machine.requestSide(RelaySide::A, RelayDirection::Forward, true, 1),
          "pending-break fixture failed to start");
  require(machine.requestSide(RelaySide::A, RelayDirection::Reverse, true, 2),
          "pending-break fixture failed to reverse");
  machine.allOff(3);
  require(sink.frames.size() == 4,
          "all-off during a break did not retain exactly two additional frames");
  expectFrame(sink, 2, 3, 0,
              "all-off during break did not commit enable-off frame");
  expectFrame(sink, 3, 3, 0,
              "all-off during break did not commit final clear frame");
  require(machine.sideStatus(RelaySide::A).phase == RelaySequencePhase::Idle &&
              !machine.sideStatus(RelaySide::A).requestedEnabled,
          "all-off did not cancel the pending reversal state");

  TraceSink rolloverSink;
  Machine rollover(rolloverSink);
  rollover.begin(0xFFFFFFF0U);
  rollover.setBreakBeforeDirectionMs(37);
  rolloverSink.clearFrames();
  require(rollover.requestSide(RelaySide::A, RelayDirection::Forward, true,
                               0xFFFFFFF1U),
          "rollover forward request was rejected");
  require(rollover.requestSide(RelaySide::A, RelayDirection::Reverse, true,
                               0xFFFFFFF2U),
          "rollover reversal request was rejected");
  rollover.service(0x00000016U);
  require(rolloverSink.mask == 0,
          "rollover-safe deadline elapsed before the configured break");
  rollover.service(0x00000017U);
  require(rolloverSink.mask == 0x03,
          "rollover-safe deadline did not settle at the exact break boundary");
}

} // namespace

int main() {
  try {
    testTwoFrameAllOffAndGeneralRelays();
    testExactReversalAndRetainedDirection();
    testBreakConfigurationBoundaries();
    testCrossSideInterlockAndPolicyStop();
    testPendingBreakAllOffAndRollover();
    std::cout << "relay_motion_machine_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "relay_motion_machine_tests: " << error.what() << '\n';
    return 1;
  }
}
