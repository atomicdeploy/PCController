#include <Arduino.h>

#include "Project/MacroQueue.h"
#include "Project/RelayController.h"

#include <cstdint>
#include <iostream>
#include <new>
#include <stdexcept>
#include <string>

namespace {

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

ControllerProtocol::Frame frame(std::uint8_t opcode, std::uint8_t sequence,
                                const std::uint8_t *payload,
                                std::uint8_t length) {
  return {opcode, sequence, length, payload};
}

// Firmware owns MacroQueue at static-storage duration, so the compact
// MacroRing is zeroed before MacroQueue::initialize() assigns its wire tag.
// Native tests must model that explicitly: a stack-allocated MacroQueue has
// indeterminate ring bytes and can make this adapter test compiler-layout
// dependent without representing the production object lifetime.
class StaticStorageMacroQueue {
public:
  StaticStorageMacroQueue() : protocol_(serial_) {
    arduino_mock::resetHardware();
    // Keep all timing deterministic while the mock's micros() advances once
    // per firmware call. The due record itself has a zero delta.
    arduino_mock::nowMicros = 42000;
    protocol_.begin(115200, nullptr);
    queue_ = new (storage_) MacroQueue(protocol_);
  }

  ~StaticStorageMacroQueue() { queue_->~MacroQueue(); }

  MacroQueue &queue() { return *queue_; }

private:
  HardwareSerial serial_;
  ControllerProtocol::UartProtocol protocol_;
  alignas(MacroQueue) std::uint8_t storage_[sizeof(MacroQueue)] = {};
  MacroQueue *queue_ = nullptr;
};

// ProtocolRuntime.inc.h sends every dequeued macro frame through the ordinary
// protocol dispatcher. Keep this narrow native seam equivalent to that
// dispatcher's RelaySet branch so the regression crosses the production
// MacroQueue, RelayController adapter, and RelayMotionMachine state machine.
bool dispatchOrdinaryRelayFrame(const ControllerProtocol::Frame &command,
                                RelayController &relays, std::uint32_t now) {
  if (command.opcode != ControllerProtocol::RelaySet ||
      command.payloadLength < 2 || command.payload[0] >= 8 ||
      command.payload[1] > 1) {
    return false;
  }
  return relays.requestRelayForTest(
      static_cast<std::uint8_t>(command.payload[0] + 1U),
      command.payload[1] != 0, now);
}

void testAdapterUsesOrdinaryDispatchFrame() {
  StaticStorageMacroQueue fixture;
  MacroQueue &queue = fixture.queue();

  const std::uint8_t begin[] = {MacroQueue::Schema, 7, 0, 1, 0};
  require(queue.handle(frame(ControllerProtocol::MacroStart, 1, begin,
                             sizeof(begin))) &&
              queue.active(),
          "macro BEGIN did not enter the bounded adapter");

  // APPEND one due-now ordinary display opcode. MacroQueue must stage it in
  // UartProtocol scratch and send it to the normal dispatcher rather than
  // owning any display or safety behavior itself.
  const std::uint8_t append[] = {
      0, 0, 0, 1, 0,
      0, 0, 0, 0, ControllerProtocol::DisplayText, 2, 'O', 'K'};
  require(queue.handle(frame(ControllerProtocol::MacroStep, 2, append,
                             sizeof(append))),
          "macro APPEND was rejected by the AVR adapter");
  const std::uint8_t run[] = {1};
  require(queue.handle(frame(ControllerProtocol::MacroStep, 3, run,
                             sizeof(run))),
          "macro RUN was rejected by the AVR adapter");

  ControllerProtocol::Frame due{};
  require(queue.dequeueDue(due) &&
              due.opcode == ControllerProtocol::DisplayText &&
              due.sequence == MacroQueue::ExecutionSequence &&
              due.payloadLength == 2 && due.payload[0] == 'O' &&
              due.payload[1] == 'K',
          "due macro command did not preserve the ordinary dispatcher frame");
  queue.completeStep(true);
  require(!queue.active(),
          "terminal macro dispatch did not leave the AVR adapter idle");
}

void testQueuedRelayFramesReachMotionInterlock() {
  StaticStorageMacroQueue fixture;
  MacroQueue &queue = fixture.queue();
  ShiftRegisters registers;
  RelayController relays(registers);
  relays.begin(100);

  const std::uint8_t begin[] = {MacroQueue::Schema, 9, 0, 4, 0};
  require(queue.handle(frame(ControllerProtocol::MacroStart, 7, begin,
                             sizeof(begin))),
          "relay macro BEGIN was rejected");

  // Turn on each side's enable relay (R2/R4), then request its reverse
  // direction (R1/R3) while still live. All records leave MacroQueue as
  // ordinary RelaySet frames; the relay state machine must enforce disable ->
  // break -> reverse -> enable and only one direction change per service pass.
  const std::uint8_t append[] = {
      0, 0, 0, 4, 0,
      0, 0, 0, 0, ControllerProtocol::RelaySet, 2, 1, 1,
      0, 0, 0, 0, ControllerProtocol::RelaySet, 2, 0, 1,
      0, 0, 0, 0, ControllerProtocol::RelaySet, 2, 3, 1,
      0, 0, 0, 0, ControllerProtocol::RelaySet, 2, 2, 1};
  require(queue.handle(frame(ControllerProtocol::MacroStep, 8, append,
                             sizeof(append))),
          "relay macro APPEND was rejected");
  const std::uint8_t run[] = {1};
  require(queue.handle(frame(ControllerProtocol::MacroStep, 9, run,
                             sizeof(run))),
          "relay macro RUN was rejected");

  ControllerProtocol::Frame due{};
  require(queue.dequeueDue(due), "queued R2 enable did not become due");
  require(dispatchOrdinaryRelayFrame(due, relays, 100),
          "ordinary dispatcher rejected queued R2 enable");
  queue.completeStep(true);
  require(relays.activeRelayMask() == _BV(RelayOutputs::R2SideAEnable),
          "queued R2 enable did not reach RelayMotionMachine");

  require(queue.dequeueDue(due), "queued R1 reversal did not become due");
  require(dispatchOrdinaryRelayFrame(due, relays, 100),
          "ordinary dispatcher rejected queued R1 reversal");
  queue.completeStep(true);
  const RelaySideStatus sideABreaking = relays.sideStatus(RelaySide::A);
  require(relays.activeRelayMask() == 0 &&
              sideABreaking.requestedDirection == RelayDirection::Reverse &&
              sideABreaking.appliedDirection == RelayDirection::Forward &&
              !sideABreaking.appliedEnabled &&
              sideABreaking.phase ==
                  RelaySequencePhase::BreakBeforeDirection,
          "queued live reversal bypassed the relay break phase");

  require(queue.dequeueDue(due), "queued R4 enable did not become due");
  require(dispatchOrdinaryRelayFrame(due, relays, 100),
          "ordinary dispatcher rejected queued R4 enable");
  queue.completeStep(true);
  require(relays.activeRelayMask() == _BV(RelayOutputs::R4SideBEnable),
          "queued R4 enable did not reach RelayMotionMachine");

  require(queue.dequeueDue(due), "queued R3 reversal did not become due");
  require(dispatchOrdinaryRelayFrame(due, relays, 100),
          "ordinary dispatcher rejected queued R3 reversal");
  queue.completeStep(true);
  require(!queue.active() && relays.activeRelayMask() == 0 &&
              relays.sideStatus(RelaySide::B).phase ==
                  RelaySequencePhase::BreakBeforeDirection,
          "queued Side B reversal bypassed the relay break phase");

  relays.service(100 + RelayController::BreakBeforeDirectionMs - 1U);
  require(relays.activeRelayMask() == 0,
          "queued live reversal ended the break early");
  relays.service(100 + RelayController::BreakBeforeDirectionMs);
  require(relays.activeRelayMask() ==
              (_BV(RelayOutputs::R1SideADirection) |
               _BV(RelayOutputs::R2SideAEnable)),
          "one-pass gate did not apply only Side A at the break boundary");
  relays.service(100 + RelayController::BreakBeforeDirectionMs +
                 RelayController::DirectionInterlockMs - 1U);
  require(relays.activeRelayMask() ==
              (_BV(RelayOutputs::R1SideADirection) |
               _BV(RelayOutputs::R2SideAEnable)),
          "queued Side B reversal violated the global direction interlock");
  relays.service(100 + RelayController::BreakBeforeDirectionMs +
                 RelayController::DirectionInterlockMs);
  require(relays.activeRelayMask() ==
              (_BV(RelayOutputs::R1SideADirection) |
               _BV(RelayOutputs::R2SideAEnable) |
               _BV(RelayOutputs::R3SideBDirection) |
               _BV(RelayOutputs::R4SideBEnable)),
          "queued Side B reversal missed the exact interlock boundary");
}

void testQueuedMotionRelayHonorsPolicy() {
  StaticStorageMacroQueue fixture;
  MacroQueue &queue = fixture.queue();
  ShiftRegisters registers;
  RelayController relays(registers);
  relays.begin(200);
  relays.setMotionAllowed(false, 200);

  const std::uint8_t begin[] = {MacroQueue::Schema, 10, 0, 1, 0};
  const std::uint8_t append[] = {
      0, 0, 0, 1, 0,
      0, 0, 0, 0, ControllerProtocol::RelaySet, 2, 1, 1};
  const std::uint8_t run[] = {1};
  require(queue.handle(frame(ControllerProtocol::MacroStart, 10, begin,
                             sizeof(begin))) &&
              queue.handle(frame(ControllerProtocol::MacroStep, 11, append,
                                 sizeof(append))) &&
              queue.handle(frame(ControllerProtocol::MacroStep, 12, run,
                                 sizeof(run))),
          "policy-denied relay macro fixture was rejected");

  ControllerProtocol::Frame due{};
  require(queue.dequeueDue(due),
          "policy-denied queued motion command did not become due");
  const bool accepted = dispatchOrdinaryRelayFrame(due, relays, 200);
  queue.completeStep(accepted);
  require(!accepted && !queue.active() && relays.activeRelayMask() == 0,
          "queued motion command bypassed disabled motion policy");
}

void testAdapterRejectsNestedMacroDispatch() {
  StaticStorageMacroQueue fixture;
  MacroQueue &queue = fixture.queue();

  const std::uint8_t begin[] = {MacroQueue::Schema, 8, 0, 1, 0};
  const std::uint8_t append[] = {
      0, 0, 0, 1, 0,
      0, 0, 0, 0, ControllerProtocol::MacroStart, 0};
  const std::uint8_t run[] = {1};
  require(queue.handle(frame(ControllerProtocol::MacroStart, 4, begin,
                             sizeof(begin))) &&
              queue.handle(frame(ControllerProtocol::MacroStep, 5, append,
                                 sizeof(append))) &&
              queue.handle(frame(ControllerProtocol::MacroStep, 6, run,
                                 sizeof(run))),
          "nested macro fixture could not enter playback");
  ControllerProtocol::Frame due{};
  require(!queue.dequeueDue(due) && !queue.active(),
          "nested macro control opcode bypassed the ordinary safety path");
}

} // namespace

int main() {
  try {
    testAdapterUsesOrdinaryDispatchFrame();
    testQueuedRelayFramesReachMotionInterlock();
    testQueuedMotionRelayHonorsPolicy();
    testAdapterRejectsNestedMacroDispatch();
    std::cout << "firmware_macro_queue_adapter_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_macro_queue_adapter_tests: " << error.what()
              << '\n';
    return 1;
  }
}
