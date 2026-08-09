#include <Arduino.h>

#include "Project/MacroAction.h"
#include "Project/MacroQueue.h"
#include "Project/FrontPanelModel.h"
#include "Project/UartProtocol.h"

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

void testSharedActionRegistry() {
  using namespace ControllerProtocol;
  require(MacroAction::MaximumPayload == 8 &&
              MacroAction::macroQueueableOpcode(Buzzer) &&
              MacroAction::payloadLength(Buzzer) == 4 &&
              MacroAction::payloadLength(PwmSet) == 3 &&
              MacroAction::payloadLength(RelaySide) == 2 &&
              MacroAction::payloadLength(RemoteKeyGesture) == 2,
          "ordinary action registry lost a required output/input opcode");
  require(!MacroAction::macroQueueableOpcode(Reset) &&
              !MacroAction::macroQueueableOpcode(MacroStart) &&
              !MacroAction::macroQueueableOpcode(RadioTransmit) &&
              !MacroAction::playbackAllowed(StatusEffect) &&
              MacroAction::validPlaybackPayload(RelaySide, 2) &&
              !MacroAction::validPlaybackPayload(RelaySide, 1) &&
              !MacroAction::recordable(StatusEffect, 48) &&
              !MacroAction::recordable(Buzzer, 3),
          "control-plane or truncated action entered the macro allow-list");
}

void testBoardCaptureAndExactPlayback() {
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);
  require(MacroQueue::Schema == 3 && queue.beginCapture(7, 1000),
          "board capture did not enter schema-3 Recording");

  const std::uint8_t side[] = {1, 2};
  const std::uint8_t buzzer[] = {0xB8, 0x01, 40, 0};
  require(queue.captureAction(ControllerProtocol::RelaySide, side,
                              sizeof(side), 1100) &&
              queue.captureAction(ControllerProtocol::Buzzer, buzzer,
                                  sizeof(buzzer), 1350) &&
              queue.retainedSteps() == 2 && queue.finishCapture() &&
              queue.captured(),
          "board capture did not retain two exact ordinary actions");

  require(queue.playCapture(5000) && !queue.hostDependent(),
          "local capture playback incorrectly depends on a live host");
  ControllerProtocol::Frame frame{};
  arduino_mock::nowMicros = 5098;
  require(!queue.dequeueDue(frame),
          "rebased first captured action ran before its playback epoch");
  arduino_mock::nowMicros = 5099;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelaySide &&
              frame.payloadLength == sizeof(side) && frame.payload[0] == 1 &&
              frame.payload[1] == 2,
          "first captured ordinary opcode/payload changed during playback");
  queue.completeStep(true);

  arduino_mock::nowMicros = 5348;
  require(!queue.dequeueDue(frame),
          "second captured action ran before its exact 250 us delta");
  arduino_mock::nowMicros = 5349;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::Buzzer &&
              frame.payloadLength == sizeof(buzzer),
          "second captured ordinary action missed its exact delta");
  queue.completeStep(true);
  require(!queue.active() && queue.captured() &&
              queue.retainedSteps() == 2,
          "completed local playback did not preserve captured ring data");

  require(queue.playCapture(7000),
          "captured macro could not replay a second time offline");
  arduino_mock::nowMicros = 7099;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelaySide,
          "second replay changed the first retained action");
  queue.completeStep(true);
  arduino_mock::nowMicros = 7349;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::Buzzer,
          "second replay changed the exact retained timing");
  queue.completeStep(true);
  require(queue.captured() && queue.retainedSteps() == 2,
          "second replay consumed the recoverable capture");

  const std::uint8_t fetch[] = {3, 0, 0};
  const ControllerProtocol::Frame fetchFrame{
      ControllerProtocol::MacroStep, 42, sizeof(fetch), fetch};
  const auto beforeFetch = serial.written().size();
  require(queue.handle(fetchFrame) &&
              serial.written().size() > beforeFetch,
          "capture could not be exported after repeated playback");
}

void testCaptureNeverOverwrites() {
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);
  require(queue.beginCapture(9, 0), "overflow fixture could not record");
  const std::uint8_t led[] = {0xFF, 1, 2, 3, 4};
  for (std::uint8_t index = 0; index < 11; ++index) {
    require(queue.captureAction(ControllerProtocol::AddressableLed, led,
                                sizeof(led), index * 10U),
            "bounded ring rejected a record before its capacity");
  }
  require(queue.captureAction(ControllerProtocol::RelayAllOff, nullptr, 0,
                              120) &&
              queue.retainedSteps() == 12,
          "exact 127-byte ring fill was rejected or wrapped");
  require(!queue.captureAction(ControllerProtocol::PwmAllOff, nullptr, 0,
                               130) &&
              queue.captured() && queue.retainedSteps() == 12,
          "exact-full ring overwrote/wrapped instead of sealing");
}

void testHostileRawStreamIsRejected() {
  const std::uint8_t hostile[] = {
      ControllerProtocol::Reset, ControllerProtocol::I2cTransfer,
      ControllerProtocol::MacroStart};
  for (const auto opcode : hostile) {
    HardwareSerial serial;
    ControllerProtocol::UartProtocol protocol(serial);
    MacroQueue queue(protocol);
    const std::uint8_t start[] = {MacroQueue::Schema, 1, 0, 1, 0};
    const ControllerProtocol::Frame startFrame{
        ControllerProtocol::MacroStart, 1, sizeof(start), start};
    require(queue.handle(startFrame) && queue.active(),
            "hostile-stream fixture could not start buffering");
    const std::uint8_t append[] = {
        0, 0, 0, 1, 0, // APPEND offset=0, complete step count=1
        0, 0, 0, 0, opcode, 0};
    const ControllerProtocol::Frame appendFrame{
        ControllerProtocol::MacroStep, 2, sizeof(append), append};
    require(queue.handle(appendFrame) && !queue.active() &&
                queue.takeSafeStopRequest(),
            "hostile raw macro opcode survived firmware validation");
  }

  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue malformed(protocol);
  const std::uint8_t start[] = {MacroQueue::Schema, 1, 0, 1, 0};
  const ControllerProtocol::Frame startFrame{
      ControllerProtocol::MacroStart, 1, sizeof(start), start};
  require(malformed.handle(startFrame),
          "malformed-length fixture could not start");
  const std::uint8_t shortSide[] = {
      0, 0, 0, 1, 0, 0, 0, 0, 0, ControllerProtocol::RelaySide, 1, 0};
  const ControllerProtocol::Frame shortSideFrame{
      ControllerProtocol::MacroStep, 2, sizeof(shortSide), shortSide};
  require(malformed.handle(shortSideFrame) && !malformed.active() &&
              malformed.takeSafeStopRequest(),
          "wrong-length RelaySide raw record survived APPEND validation");
}

void testLifecycleAndStreamingStateValidation() {
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);

  const std::uint8_t emptyStart[] = {MacroQueue::Schema, 1, 0, 0, 0};
  const ControllerProtocol::Frame emptyStartFrame{
      ControllerProtocol::MacroStart, 1, sizeof(emptyStart), emptyStart};
  require(queue.handle(emptyStartFrame) && !queue.active(),
          "zero-step host macro entered an infinite Playing path");

  require(queue.beginCapture(2, 10),
          "recording-state APPEND fixture could not start");
  const std::uint8_t appendDuringCapture[] = {
      0, 0, 0, 1, 0, 0, 0, 0, 0, ControllerProtocol::RelayAllOff, 0};
  const ControllerProtocol::Frame appendDuringCaptureFrame{
      ControllerProtocol::MacroStep, 2, sizeof(appendDuringCapture),
      appendDuringCapture};
  require(queue.handle(appendDuringCaptureFrame) && queue.recording() &&
              queue.retainedSteps() == 0,
          "host APPEND mutated the board-owned Recording ring");
  const std::uint8_t side[] = {0, 1};
  require(queue.captureAction(ControllerProtocol::RelaySide, side,
                              sizeof(side), 20) &&
              queue.finishCapture(),
          "rejected host fragment damaged the local capture lifecycle");

  MacroQueue empty(protocol);
  require(empty.beginCapture(3, 0) && empty.finishCapture() &&
              empty.captured() && !empty.active() &&
              empty.beginCapture(4, 1),
          "empty capture did not publish a replaceable terminal state");
  require(!empty.captureAction(ControllerProtocol::RelayAllOff, nullptr, 0,
                               0x80000001UL) &&
              empty.captured(),
          "capture accepted a due time outside the signed scheduler window");

  for (const bool badCount : {false, true}) {
    MacroQueue streamed(protocol);
    const std::uint8_t start[] = {MacroQueue::Schema, 5, 0, 2, 0};
    const ControllerProtocol::Frame startFrame{
        ControllerProtocol::MacroStart, 3, sizeof(start), start};
    require(streamed.handle(startFrame), "stream validation could not start");
    std::uint8_t append[] = {
        0, 0, 0, static_cast<std::uint8_t>(badCount ? 2 : 1), 0,
        0, 0, 0, 0, ControllerProtocol::RelayAllOff, 0};
    if (!badCount) {
      append[8] = 0x80; // due=0x80000000, outside signed window
    }
    const ControllerProtocol::Frame appendFrame{
        ControllerProtocol::MacroStep, 4, sizeof(append), append};
    require(streamed.handle(appendFrame) && !streamed.active() &&
                streamed.takeSafeStopRequest(),
            badCount ? "forged complete-step count survived APPEND validation"
                     : "out-of-window raw due time survived APPEND validation");
  }
}

void testFrontPanelMotionCapturesSemanticSideActions() {
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);
  require(queue.beginCapture(11, 100),
          "front-panel semantic fixture could not record");
  const MotionKeyBinding up = motionKeyBinding(MENU_PREVIOUS);
  const MotionKeyBinding down = motionKeyBinding(MENU_NEXT);
  const std::uint8_t upPayload[] = {up.side,
                                    static_cast<std::uint8_t>(up.reverse ? 2 : 1)};
  const std::uint8_t downPayload[] = {
      down.side, static_cast<std::uint8_t>(down.reverse ? 2 : 1)};
  require(queue.captureAction(ControllerProtocol::RelaySide, upPayload,
                              sizeof(upPayload), 110) &&
              queue.captureAction(ControllerProtocol::RelaySide, downPayload,
                                  sizeof(downPayload), 160) &&
              queue.finishCapture() && queue.playCapture(1000),
          "front-panel A up/down did not enter semantic capture");
  ControllerProtocol::Frame frame{};
  arduino_mock::nowMicros = 1009;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelaySide &&
              frame.payload[0] == 0 && frame.payload[1] == 1,
          "front-panel A-up replay depends on menu/key mode");
  queue.completeStep(true);
  arduino_mock::nowMicros = 1059;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelaySide &&
              frame.payload[0] == 0 && frame.payload[1] == 2,
          "front-panel A-down replay depends on menu/key mode");
  queue.completeStep(true);
}

void testMotionExitChordNeverCapturesOpposingStart() {
  using ControllerProtocol::RelayAllOff;
  using ControllerProtocol::RelaySide;

  require(unifiedInputIntent(MENU_DECREASE, false) ==
                  UnifiedInputIntent::Macro &&
              unifiedInputIntent(MENU_INCREASE, false) ==
                  UnifiedInputIntent::Motion,
          "K3/K4 no longer select macro capture and motion control");

  const MenuAction chordOrders[][2] = {
      {MENU_PREVIOUS, MENU_NEXT},
      {MENU_NEXT, MENU_PREVIOUS},
      {MENU_DECREASE, MENU_INCREASE},
      {MENU_INCREASE, MENU_DECREASE},
  };
  for (const auto &chord : chordOrders) {
    const std::uint8_t simultaneous = static_cast<std::uint8_t>(
        (1U << chord[0]) | (1U << chord[1]));
    require(motionKeyCompletesExitChord(simultaneous, chord[0]) &&
                motionKeyCompletesExitChord(simultaneous, chord[1]),
            "simultaneous raw motion chord allowed a relay twitch");
  }
  for (std::uint8_t order = 0; order < 4; ++order) {
    HardwareSerial serial;
    ControllerProtocol::UartProtocol protocol(serial);
    MacroQueue queue(protocol);

    // K3 starts capture and K4 enters motion. Neither page-navigation action
    // is recorded; only accepted output opcodes belong in the capture.
    require(queue.beginCapture(static_cast<std::uint8_t>(20 + order), 100),
            "K3 could not start the exit-chord capture fixture");
    const auto first = chordOrders[order][0];
    const auto second = chordOrders[order][1];
    std::uint8_t activeSnapshot =
        static_cast<std::uint8_t>(1U << first);
    std::uint8_t commandedMask = 0;
    require(!motionKeyCompletesExitChord(activeSnapshot, first),
            "first motion Down was mistaken for an exit chord");

    const MotionKeyBinding firstBinding = motionKeyBinding(first);
    const std::uint8_t start[] = {
        firstBinding.side,
        static_cast<std::uint8_t>(firstBinding.reverse ? 2 : 1)};
    require(queue.captureAction(RelaySide, start, sizeof(start), 110),
            "first immediate motion Down was not captured");
    commandedMask |= static_cast<std::uint8_t>(1U << first);

    // The partner Down completes the physical UI chord. Runtime suppresses
    // its opposing relay request/capture, then serviceMotionExit records the
    // real safety stop and global all-off at the same MCU timestamp.
    activeSnapshot |= static_cast<std::uint8_t>(1U << second);
    require(motionKeyCompletesExitChord(activeSnapshot, second),
            "same-side partner Down did not complete the exit chord");
    require((commandedMask & static_cast<std::uint8_t>(1U << second)) == 0,
            "suppressed partner Down created a phantom pressed motion bit");
    const std::uint8_t stop[] = {firstBinding.side, 0};
    require(queue.captureAction(RelaySide, stop, sizeof(stop), 160) &&
                queue.captureAction(RelayAllOff, nullptr, 0, 160),
            "motion exit safety actions were not captured");
    commandedMask = 0; // serviceMotionExit owns the all-off mask clear.
    require(commandedMask == 0 && queue.recording() &&
                queue.retainedSteps() == 3,
            "motion exit left a pressed bit or stopped K3 recording early");

    // Both physical Up events observe the cleared mask and add no phantom
    // stop. Only the later K3 lifecycle ends recording.
    for (const auto released : {first, second}) {
      require((commandedMask & static_cast<std::uint8_t>(1U << released)) == 0,
              "released exit key retained a motion bit");
    }
    require(queue.recording() && queue.finishCapture() &&
                queue.playCapture(1000),
            "K3 did not remain responsible for stop/replay lifecycle");

    ControllerProtocol::Frame frame{};
    arduino_mock::nowMicros = 1009;
    require(queue.dequeueDue(frame) && frame.opcode == RelaySide &&
                frame.payload[0] == firstBinding.side &&
                frame.payload[1] == start[1],
            "first real direction changed during exit-chord replay");
    queue.completeStep(true);
    arduino_mock::nowMicros = 1059;
    require(queue.dequeueDue(frame) && frame.opcode == RelaySide &&
                frame.payload[0] == firstBinding.side &&
                frame.payload[1] == 0,
            "opposing start survived instead of the safety stop");
    queue.completeStep(true);
    require(queue.active(),
            "same-due all-off was drained in the stop dispatch turn");
    require(queue.dequeueDue(frame) && frame.opcode == RelayAllOff,
            "global all-off was not retained for the next dispatch turn");
    queue.completeStep(true);
  }
}

void testCaptureClockRollover() {
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);
  require(queue.beginCapture(10, 0xFFFFFFF0UL) &&
              queue.captureAction(ControllerProtocol::RelayAllOff, nullptr, 0,
                                  0xFFFFFFF8UL) &&
              queue.captureAction(ControllerProtocol::PwmAllOff, nullptr, 0,
                                  0x00000020UL) &&
              queue.finishCapture() && queue.playCapture(1000),
          "rollover capture fixture could not start");
  ControllerProtocol::Frame frame{};
  arduino_mock::nowMicros = 1007;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelayAllOff,
          "first rollover action was not rebased to playback start");
  queue.completeStep(true);
  arduino_mock::nowMicros = 1046;
  require(!queue.dequeueDue(frame),
          "rollover delta fired one microsecond early");
  arduino_mock::nowMicros = 1047;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::PwmAllOff,
          "uint32 rollover changed the exact 40 us action delta");
  queue.completeStep(true);
}

void testCapturePersistenceAcknowledgementIsIdentityGuarded() {
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);
  require(queue.beginCapture(7, 100),
          "capture ACK fixture could not start");
  const std::uint8_t side[] = {1, 1};
  require(queue.captureAction(ControllerProtocol::RelaySide, side,
                              sizeof(side), 120) &&
              queue.finishCapture(),
          "capture ACK fixture could not seal");

  std::uint8_t clear[] = {4, 7, 99, 0, 0, 0};
  ControllerProtocol::Frame clearFrame{
      ControllerProtocol::MacroStep, 8, sizeof(clear), clear};
  require(queue.handle(clearFrame) && queue.captured(),
          "stale capture ACK cleared a retained recording");
  clear[2] = 100;
  require(queue.handle(clearFrame) && queue.captured() &&
              queue.playCapture(1000),
          "matching export ACK destroyed board-local replay data");
  ControllerProtocol::Frame replay{};
  arduino_mock::nowMicros = 1019;
  require(queue.dequeueDue(replay),
          "export-acknowledged capture could not replay");
  queue.completeStep(true);
  require(queue.captured(),
          "export state was lost after local replay");

  clear[0] = 5;
  require(queue.handle(clearFrame) && !queue.captured() &&
              queue.takeSafeStopRequest(),
          "explicit identity-guarded clear retained capture/outputs");

  require(queue.beginCapture(7, 200) &&
              queue.captureAction(ControllerProtocol::RelaySide, side,
                                  sizeof(side), 220) &&
              queue.finishCapture(),
          "ID-reuse capture fixture could not seal");
  clear[0] = 4;
  clear[2] = 100;
  require(queue.handle(clearFrame) && queue.captured(),
          "old ID/start ACK deleted a newer reused-ID capture");
}

} // namespace

int main() {
  try {
    testSharedActionRegistry();
    testBoardCaptureAndExactPlayback();
    testCaptureNeverOverwrites();
    testCaptureClockRollover();
    testCapturePersistenceAcknowledgementIsIdentityGuarded();
    testHostileRawStreamIsRejected();
    testLifecycleAndStreamingStateValidation();
    testFrontPanelMotionCapturesSemanticSideActions();
    testMotionExitChordNeverCapturesOpposingStart();
    std::cout << "firmware_macro_queue_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_macro_queue_tests: " << error.what() << '\n';
    return 1;
  }
}
