#include "Project/MacroQueue.h"

#include <iostream>
#include <stdexcept>

namespace {
void require(bool value, const char *message) {
  if (!value) throw std::runtime_error(message);
}

ControllerProtocol::Frame frame(uint8_t opcode, uint8_t sequence,
                                const uint8_t *payload, uint8_t length) {
  return {opcode, sequence, length, payload};
}

void testCaptureAndReplayUseOrdinaryOpcodes() {
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);
  const uint8_t begin[] = {MacroQueue::Schema, 7, MacroQueue::CaptureInputs,
                           0, 0};
  require(queue.handle(frame(ControllerProtocol::MacroStart, 1, begin,
                             sizeof(begin))),
          "capture start was not handled");
  const uint8_t side[] = {0, 1};
  const uint8_t pwm[] = {2, 0x34, 0x12};
  require(queue.capture(ControllerProtocol::RelaySide, side, sizeof(side)),
          "side operation was not recorded");
  require(queue.capture(ControllerProtocol::PwmSet, pwm, sizeof(pwm)),
          "PWM operation was not recorded");
  const uint8_t stop[] = {5};
  queue.handle(frame(ControllerProtocol::MacroStep, 2, stop, sizeof(stop)));
  const uint8_t run[] = {1};
  queue.handle(frame(ControllerProtocol::MacroStep, 3, run, sizeof(run)));

  ControllerProtocol::Frame queued{};
  require(queue.dequeueDue(queued) && queued.opcode == ControllerProtocol::RelaySide &&
              queued.payloadLength == sizeof(side),
          "first captured ordinary opcode did not replay");
  queue.completeStep(true);
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
