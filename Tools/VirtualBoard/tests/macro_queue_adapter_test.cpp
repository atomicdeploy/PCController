#include <Arduino.h>

#include "Project/MacroQueue.h"

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

ControllerProtocol::Frame frame(std::uint8_t opcode, std::uint8_t sequence,
                                const std::uint8_t *payload,
                                std::uint8_t length) {
  return {opcode, sequence, length, payload};
}

void testAdapterUsesOrdinaryDispatchFrame() {
  arduino_mock::resetHardware();
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  protocol.begin(115200, nullptr);
  MacroQueue queue(protocol);

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

void testAdapterRejectsNestedMacroDispatch() {
  arduino_mock::resetHardware();
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  protocol.begin(115200, nullptr);
  MacroQueue queue(protocol);

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
    testAdapterRejectsNestedMacroDispatch();
    std::cout << "firmware_macro_queue_adapter_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_macro_queue_adapter_tests: " << error.what()
              << '\n';
    return 1;
  }
}
