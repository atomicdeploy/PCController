#include "Project/MacroQueue.h"
#include "Project/ControllerEvents.h"

#include <algorithm>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {
void require(bool value, const std::string &message) {
  if (!value) {
    throw std::runtime_error(message);
  }
}

ControllerProtocol::Frame frame(std::uint8_t opcode, std::uint8_t sequence,
                                const std::uint8_t *payload,
                                std::uint8_t length) {
  return {opcode, sequence, length, payload};
}

std::vector<std::uint8_t>
decodeSingleResponse(const std::vector<std::uint8_t> &encoded) {
  const auto delimiter = std::find(encoded.begin(), encoded.end(), 0);
  require(delimiter != encoded.end(),
          "response is not a delimited COBS frame");
  std::vector<std::uint8_t> raw;
  std::size_t cursor = 0;
  const std::size_t end = static_cast<std::size_t>(delimiter - encoded.begin());
  while (cursor < end) {
    const std::uint8_t code = encoded[cursor++];
    require(code != 0, "response contains an invalid COBS code");
    for (std::uint8_t index = 1; index < code; ++index) {
      require(cursor < end, "response COBS block is truncated");
      raw.push_back(encoded[cursor++]);
    }
    if (code != 0xFF && cursor < end) {
      raw.push_back(0);
    }
  }
  require(raw.size() >= 6 && raw[0] == ControllerProtocol::Magic,
          "response envelope is invalid");
  require(raw.size() == static_cast<std::size_t>(6 + raw[4]),
          "response payload length is invalid");
  return raw;
}

std::vector<std::uint8_t>
request(MacroQueue &queue, HardwareSerial &serial, std::uint8_t opcode,
        std::uint8_t sequence, const std::vector<std::uint8_t> &payload) {
  serial.clearWritten();
  require(queue.handle(frame(opcode, sequence, payload.data(),
                             static_cast<std::uint8_t>(payload.size()))),
          "macro request was not handled");
  return decodeSingleResponse(serial.written());
}

std::uint8_t responseOpcode(const std::vector<std::uint8_t> &raw) {
  return raw[2];
}

std::uint8_t macroState(const std::vector<std::uint8_t> &raw) {
  require(responseOpcode(raw) == ControllerProtocol::MacroStatusResponse &&
              raw[4] == 21 && raw[5] == static_cast<std::uint8_t>(ControllerEventType::Macro) &&
              raw[6] == MacroQueue::Schema,
          "response is not a schema-3 macro status");
  return raw[7];
}

std::uint32_t macroStartedAt(const std::vector<std::uint8_t> &raw) {
  require(raw.size() >= 22, "macro status is truncated before startedAtUs");
  return static_cast<std::uint32_t>(raw[18]) |
         static_cast<std::uint32_t>(raw[19]) << 8 |
         static_cast<std::uint32_t>(raw[20]) << 16 |
         static_cast<std::uint32_t>(raw[21]) << 24;
}

void testCaptureFetchReplayAndRetention() {
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  MacroQueue queue(protocol);

  require(queue.beginCapture(7, 1000), "capture did not start");
  const std::uint8_t side[] = {0, 1};
  require(queue.captureAction(ControllerProtocol::RelaySide, side,
                              sizeof(side), 1100),
          "ordinary motion opcode was not captured");
  require(queue.finishCapture(), "capture did not finish");

  const std::vector<std::uint8_t> fetch{3, 7, 0, 0};
  const auto firstPage = request(queue, serial, ControllerProtocol::MacroStep,
                                 1, fetch);
  require(responseOpcode(firstPage) == ControllerProtocol::MacroStatusResponse &&
              firstPage[5] == MacroQueue::Schema && firstPage[6] == 3 &&
              firstPage[7] == 7,
          "FETCH [3,id,offsetLE16] did not return the retained capture");

  const auto wrongIdentity = request(
      queue, serial, ControllerProtocol::MacroStep, 2, {3, 8, 0, 0});
  require(responseOpcode(wrongIdentity) == ControllerProtocol::ErrorResponse,
          "FETCH accepted a replacement capture identity");

  // A host playback start racing capture export must be rejected without
  // mutating the retained bytes. This covers both Captured and Exported.
  const auto blockedStart = request(
      queue, serial, ControllerProtocol::MacroStart, 3,
      {MacroQueue::Schema, 9, 0, 1, 0});
  require(responseOpcode(blockedStart) == ControllerProtocol::ErrorResponse,
          "host playback erased an uncleared retained capture");
  const auto pageAfterRace = request(queue, serial,
                                     ControllerProtocol::MacroStep, 4, fetch);
  require(pageAfterRace[4] == firstPage[4] &&
              std::equal(pageAfterRace.begin() + 5, pageAfterRace.end() - 1,
                         firstPage.begin() + 5),
          "play-vs-fetch race changed the retained capture page");

  require(queue.playCapture(2000), "retained capture did not play");
  const auto playing = request(queue, serial, ControllerProtocol::MacroStep,
                               5, {2});
  require(macroState(playing) == MacroQueue::Playing &&
              macroStartedAt(playing) == 2000,
          "local replay monitoring did not expose its MCU playback epoch");
  arduino_mock::nowMicros = 2200;
  ControllerProtocol::Frame queued{};
  require(queue.dequeueDue(queued) &&
              queued.opcode == ControllerProtocol::RelaySide &&
              queued.payloadLength == sizeof(side),
          "captured ordinary opcode did not replay");
  queue.completeStep(true);
  const auto completed = request(queue, serial, ControllerProtocol::MacroStep,
                                 6, {2});
  require(macroState(completed) == MacroQueue::Captured,
          "completed local replay hid the retained capture from reconnect");
  require(macroStartedAt(completed) == 1000,
          "retained capture identity was not restored after local replay");
  require(responseOpcode(request(queue, serial, ControllerProtocol::MacroStep,
                                 7, fetch)) ==
              ControllerProtocol::MacroStatusResponse,
          "capture was not fetchable after completed local replay");

  require(queue.playCapture(3000), "retained capture did not replay again");
  queue.cancel(false);
  const auto cancelled = request(queue, serial, ControllerProtocol::MacroStep,
                                 8, {2});
  require(macroState(cancelled) == MacroQueue::Captured,
          "cancelled local replay hid the retained capture from reconnect");

  const auto exported = request(
      queue, serial, ControllerProtocol::MacroStep, 8,
      {4, 7, 0xE8, 0x03, 0, 0});
  require(responseOpcode(exported) == ControllerProtocol::Ack,
          "capture export acknowledgement failed");
  require(queue.playCapture(4000), "exported capture was not replayable");
  queue.cancel(false);
  const auto exportedAfterCancel = request(
      queue, serial, ControllerProtocol::MacroStep, 9, {2});
  require(macroState(exportedAfterCancel) == MacroQueue::Exported,
          "cancelled replay lost the exported retained state");

  const auto cleared = request(
      queue, serial, ControllerProtocol::MacroStep, 10,
      {5, 7, 0xE8, 0x03, 0, 0});
  require(responseOpcode(cleared) == ControllerProtocol::Ack,
          "identity-guarded capture clear failed");
  const auto allowedStart = request(
      queue, serial, ControllerProtocol::MacroStart, 11,
      {MacroQueue::Schema, 9, 0, 1, 0});
  require(responseOpcode(allowedStart) == ControllerProtocol::Ack,
          "host playback stayed blocked after explicit capture clear");
}
} // namespace

int main() {
  try {
    testCaptureFetchReplayAndRetention();
    std::cout << "firmware_macro_queue_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_macro_queue_tests: " << error.what() << '\n';
    return 1;
  }
}
