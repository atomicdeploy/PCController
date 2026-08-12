#include "Project/MacroQueue.h"

#include <iostream>
#include <stdexcept>

namespace {
void require(bool value, const char *message) {
  if (!value) throw std::runtime_error(message);
}

void testCaptureAndReplayUseOrdinaryOpcodes() {
  arduino_mock::resetHardware();
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);
  require(queue.beginCapture(7, 1000), "capture start was not handled");
  const uint8_t side[] = {0, 1};
  const uint8_t pwm[] = {2, 0x34, 0x12};
  require(queue.captureAction(ControllerProtocol::RelaySide, side,
                              sizeof(side), 1000),
          "side operation was not recorded");
  require(queue.captureAction(ControllerProtocol::PwmSet, pwm, sizeof(pwm),
                              2500),
          "PWM operation was not recorded");
  require(queue.finishCapture(), "capture did not finish");
  require(queue.playCapture(1000), "capture did not start playback");

  ControllerProtocol::Frame queued{};
  require(queue.dequeueDue(queued) && queued.opcode == ControllerProtocol::RelaySide &&
              queued.payloadLength == sizeof(side),
          "first captured ordinary opcode did not replay");
  queue.completeStep(true);
  arduino_mock::nowMicros = 2500;
  require(queue.dequeueDue(queued) && queued.opcode == ControllerProtocol::PwmSet &&
              queued.payloadLength == sizeof(pwm),
          "second captured ordinary opcode did not replay");
  queue.completeStep(true);
  const uint8_t query[] = {2};
  queue.handle(frame(ControllerProtocol::MacroStep, 4, query, sizeof(query)));
  require(!serial.written().empty(), "macro status produced no UART response");
}
} // namespace

int main() {
  try {
    testCaptureAndReplayUseOrdinaryOpcodes();
    std::cout << "firmware_macro_queue_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_macro_queue_tests: " << error.what() << '\n';
    return 1;
  }
}
