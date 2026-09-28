#include <Arduino.h>

#include "Project/MacroQueue.h"
#include "Project/RelayController.h"

#include <cstdint>
#include <iostream>
#include <new>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

std::vector<std::uint8_t> appliedMasks;
std::vector<std::uint32_t> appliedMicros;
MacroQueue *recordingQueue = nullptr;

void observeAppliedRelay(std::uint8_t mask, std::uint32_t atUs) {
  appliedMasks.push_back(mask);
  appliedMicros.push_back(atUs);
  if (recordingQueue) recordingQueue->recordRelay(mask, atUs);
}

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
  if (command.opcode == ControllerProtocol::RelaySet && command.payloadLength == 1) {
    return relays.requestMask(command.payload[0], now);
  }
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

  const std::uint8_t begin[] = {7, 0, 1, 0};
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

  const std::uint8_t begin[] = {9, 0, 4, 0};
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

  const std::uint8_t begin[] = {10, 0, 1, 0};
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

  const std::uint8_t begin[] = {8, 0, 1, 0};
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

void testRecordingObservesAppliedEdgesAndPreservesRelayGuards() {
  StaticStorageMacroQueue fixture;
  MacroQueue &queue = fixture.queue();
  ShiftRegisters registers;
  RelayController relays(registers);
  relays.begin(0);
  appliedMasks.clear();
  appliedMicros.clear();
  recordingQueue = &queue;
  relays.setAppliedObserver(observeAppliedRelay);
  const uint8_t record[] = {3, 12};
  queue.handle(frame(ControllerProtocol::MacroStep, 1, record, sizeof(record)), 0);
  require(queue.active() && queue.recording() && queue.claimSharedWorkspace() == nullptr,
          "recording did not protect shared strip storage");
  relays.setGeneral(0, true);
  relays.setGeneral(0, true);
  relays.setGeneral(0, false);
  require(appliedMasks.size() == 2 && appliedMasks[0] == 0x10 && appliedMasks[1] == 0 &&
              appliedMicros[1] > appliedMicros[0],
          "latch observer coalesced on/off edges or included unchanged writes");
  const uint8_t stop[] = {4};
  queue.handle(frame(ControllerProtocol::MacroStep, 2, stop, sizeof(stop)));
  queue.handle(frame(ControllerProtocol::MacroStep, 2, stop, sizeof(stop)));
  require(!queue.active() && queue.claimSharedWorkspace() == nullptr,
          "stopped recorder failed to retain its profile");
  const uint8_t replay[] = {6};
  queue.handle(frame(ControllerProtocol::MacroStep, 3, replay, sizeof(replay)));
  ControllerProtocol::Frame due{};
  arduino_mock::nowMicros += 100000;
  uint8_t dispatched = 0;
  while (queue.dequeueDue(due)) {
    queue.completeStep(dispatchOrdinaryRelayFrame(due, relays, 100));
    require(++dispatched <= 3, "retained replay failed to terminate");
  }
  require(dispatched == 3 && !queue.active() && appliedMasks.size() == 4 &&
              appliedMasks[2] == 0x10 && appliedMasks[3] == 0,
          "retained recording did not replay physical relay edges through dispatcher");
  const uint8_t clear[] = {7};
  queue.handle(frame(ControllerProtocol::MacroStep, 4, clear, sizeof(clear)));
  require(queue.claimSharedWorkspace() != nullptr, "explicit clear left RAM reserved");
  recordingQueue = nullptr;
}

void testAggregateMaskSingleLatchAndMotionSafety() {
  ShiftRegisters registers;
  RelayController relays(registers);
  relays.begin(0);
  appliedMasks.clear();
  relays.setAppliedObserver(observeAppliedRelay);
  require(relays.requestMask(0xF0, 100) && appliedMasks.size() == 1 &&
              appliedMasks[0] == 0xF0, "general mask did not share one latch edge");
  relays.setMotionAllowed(false, 100);
  require(!relays.requestMask(0x0F, 100) && relays.activeRelayMask() == 0xF0,
          "denied motion snapshot partially changed general outputs");
  relays.setMotionAllowed(true, 100);
  require(relays.requestMask(0x0A, 100), "forward motion mask rejected");
  require(relays.requestMask(0x0F, 101) && relays.activeRelayMask() == 0,
          "mask reversal bypassed enable-off break");
  relays.service(101 + RelayController::BreakBeforeDirectionMs);
  require(relays.activeRelayMask() == 3, "aggregate reversal changed both directions at once");
  relays.service(101 + RelayController::BreakBeforeDirectionMs +
                 RelayController::DirectionInterlockMs);
  require(relays.activeRelayMask() == 15, "aggregate reversal failed to complete safe sequence");
}

} // namespace

int main() {
  try {
    testAdapterUsesOrdinaryDispatchFrame();
    testQueuedRelayFramesReachMotionInterlock();
    testQueuedMotionRelayHonorsPolicy();
    testAdapterRejectsNestedMacroDispatch();
    testRecordingObservesAppliedEdgesAndPreservesRelayGuards();
    testAggregateMaskSingleLatchAndMotionSafety();
    std::cout << "firmware_macro_queue_adapter_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_macro_queue_adapter_tests: " << error.what()
              << '\n';
    return 1;
  }
}
