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
              MacroAction::playbackAllowed(StatusEffect) &&
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

  require(queue.playCapture(5000), "captured macro did not enter playback");
  ControllerProtocol::Frame frame{};
  arduino_mock::nowMicros = 4998;
  require(!queue.dequeueDue(frame),
          "rebased first captured action ran before its playback epoch");
  arduino_mock::nowMicros = 4999;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelaySide &&
              frame.payloadLength == sizeof(side) && frame.payload[0] == 1 &&
              frame.payload[1] == 2,
          "first captured ordinary opcode/payload changed during playback");
  queue.completeStep(true);

  arduino_mock::nowMicros = 5248;
  require(!queue.dequeueDue(frame),
          "second captured action ran before its exact 250 us delta");
  arduino_mock::nowMicros = 5249;
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
  arduino_mock::nowMicros = 6999;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelaySide,
          "second replay changed the first retained action");
  queue.completeStep(true);
  arduino_mock::nowMicros = 7249;
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
  arduino_mock::nowMicros = 999;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelaySide &&
              frame.payload[0] == 0 && frame.payload[1] == 1,
          "front-panel A-up replay depends on menu/key mode");
  queue.completeStep(true);
  arduino_mock::nowMicros = 1049;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelaySide &&
              frame.payload[0] == 0 && frame.payload[1] == 2,
          "front-panel A-down replay depends on menu/key mode");
  queue.completeStep(true);
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
  arduino_mock::nowMicros = 999;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::RelayAllOff,
          "first rollover action was not rebased to playback start");
  queue.completeStep(true);
  arduino_mock::nowMicros = 1038;
  require(!queue.dequeueDue(frame),
          "rollover delta fired one microsecond early");
  arduino_mock::nowMicros = 1039;
  require(queue.dequeueDue(frame) &&
              frame.opcode == ControllerProtocol::PwmAllOff,
          "uint32 rollover changed the exact 40 us action delta");
  queue.completeStep(true);
}

} // namespace

int main() {
  try {
    testSharedActionRegistry();
    testBoardCaptureAndExactPlayback();
    testCaptureNeverOverwrites();
    testCaptureClockRollover();
    testHostileRawStreamIsRejected();
    testFrontPanelMotionCapturesSemanticSideActions();
    std::cout << "firmware_macro_queue_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_macro_queue_tests: " << error.what() << '\n';
    return 1;
  }
}
