#include "../../../Project/UartProtocol.h"

#include <algorithm>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

using ControllerProtocol::Frame;
using ControllerProtocol::UartProtocol;

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

std::vector<std::uint8_t>
encode(std::uint8_t opcode, std::uint8_t sequence,
       const std::vector<std::uint8_t> &payload,
       std::uint8_t revision = ControllerProtocol::EnvelopeRevision) {
  std::vector<std::uint8_t> raw{
      ControllerProtocol::Magic, revision, opcode,
      sequence, static_cast<std::uint8_t>(payload.size())};
  raw.insert(raw.end(), payload.begin(), payload.end());
  raw.push_back(UartProtocol::crc8(raw.data(),
                                   static_cast<std::uint8_t>(raw.size())));

  std::vector<std::uint8_t> encoded(1, 0);
  std::size_t codeIndex = 0;
  std::uint8_t code = 1;
  for (const auto value : raw) {
    if (value == 0) {
      encoded[codeIndex] = code;
      codeIndex = encoded.size();
      encoded.push_back(0);
      code = 1;
    } else {
      encoded.push_back(value);
      ++code;
    }
  }
  encoded[codeIndex] = code;
  encoded.push_back(0);
  return encoded;
}

std::vector<std::vector<std::uint8_t>>
decodeResponses(const std::vector<std::uint8_t> &encoded) {
  std::vector<std::vector<std::uint8_t>> frames;
  std::size_t begin = 0;
  while (begin < encoded.size()) {
    const auto delimiter = std::find(encoded.begin() + begin, encoded.end(), 0);
    require(delimiter != encoded.end(), "response frame is not delimited");
    const std::size_t end = static_cast<std::size_t>(delimiter - encoded.begin());
    std::vector<std::uint8_t> raw;
    std::size_t cursor = begin;
    while (cursor < end) {
      const auto code = encoded[cursor++];
      require(code != 0, "invalid response COBS code");
      for (std::uint8_t index = 1; index < code; ++index) {
        require(cursor < end, "truncated response COBS block");
        raw.push_back(encoded[cursor++]);
      }
      if (code != 0xFF && cursor < end) {
        raw.push_back(0);
      }
    }
    require(raw.size() >= 6 && raw.size() == 6U + raw[4],
            "invalid decoded response envelope");
    frames.push_back(raw);
    begin = end + 1;
  }
  return frames;
}

std::uint32_t trailingMicros(const std::vector<std::uint8_t> &raw) {
  require(raw[4] >= 4, "timed response is missing its MCU clock");
  const std::size_t offset = 5U + raw[4] - 4U;
  return static_cast<std::uint32_t>(raw[offset]) |
         static_cast<std::uint32_t>(raw[offset + 1]) << 8 |
         static_cast<std::uint32_t>(raw[offset + 2]) << 16 |
         static_cast<std::uint32_t>(raw[offset + 3]) << 24;
}

struct Capture {
  UartProtocol *protocol = nullptr;
  std::vector<std::vector<std::uint8_t>> payloads;
  bool nestedResponseKeptViewStable = true;
};

void captureFrame(const Frame &frame, void *context) {
  auto &capture = *static_cast<Capture *>(context);
  const std::vector<std::uint8_t> before(frame.payload,
                                          frame.payload + frame.payloadLength);
  capture.payloads.push_back(before);
  require(capture.protocol->sendAck(frame.sequence, frame.opcode),
          "nested ACK write failed");
  capture.nestedResponseKeptViewStable &=
      std::equal(before.begin(), before.end(), frame.payload);
}

void testAdvisoryRevisionDoesNotBlockSemanticFrames() {
  HardwareSerial serial;
  UartProtocol protocol(serial);
  Capture capture;
  capture.protocol = &protocol;
  protocol.begin(115200, captureFrame, &capture);

  const std::vector<std::uint8_t> expected{0x41, 0x42};
  serial.feed(encode(ControllerProtocol::DisplayText, 9, expected, 0x7E));
  protocol.service();

  require(capture.payloads.size() == 1 && capture.payloads[0] == expected,
          "advisory envelope revision blocked a valid semantic frame");
  require(protocol.framingErrors() == 0 && protocol.crcErrors() == 0,
          "advisory revision advanced an envelope error counter");
}

void testRepresentativeAndMaximumPayloads() {
  HardwareSerial serial;
  UartProtocol protocol(serial);
  Capture capture;
  capture.protocol = &protocol;
  protocol.begin(115200, captureFrame, &capture);
  require(serial.baud() == 115200, "serial baud was not configured");

  const std::vector<std::uint8_t> zeros{0, 0, 1, 0, 2, 0, 0};
  std::vector<std::uint8_t> maximum(ControllerProtocol::MaximumPayload);
  for (std::size_t index = 0; index < maximum.size(); ++index) {
    maximum[index] = index % 3 == 0 ? 0 : static_cast<std::uint8_t>(index);
  }
  serial.feed(encode(ControllerProtocol::DisplayText, 7, zeros));
  serial.feed(encode(ControllerProtocol::GetStatus, 8, maximum));
  protocol.service();

  require(capture.payloads.size() == 1 && capture.payloads[0] == zeros &&
              serial.available() > 0,
          "one UART service pass consumed more than one complete frame");
  protocol.service();
  require(capture.payloads.size() == 2,
          "second UART service pass did not dispatch its queued frame");
  require(capture.payloads[0] == zeros,
          "consecutive or trailing zero payload changed in-place");
  require(capture.payloads[1] == maximum,
          "maximum 48-byte payload changed in-place");
  require(capture.nestedResponseKeptViewStable,
          "nested response overwrote the active RX payload view");
  require(protocol.framingErrors() == 0 && protocol.crcErrors() == 0,
          "valid frames advanced error counters");
  require(!serial.written().empty(), "nested ACKs produced no serial output");
}

void testInvalidFramesAreRejected() {
  HardwareSerial serial;
  UartProtocol protocol(serial);
  Capture capture;
  capture.protocol = &protocol;
  protocol.begin(115200, captureFrame, &capture);

  // COBS code five claims four following bytes but only two are present.
  serial.feed({5, 1, 2, 0});

  auto badCrc = encode(ControllerProtocol::GetStatus, 2, {});
  badCrc[badCrc.size() - 2] ^= 0x55;
  serial.feed(badCrc);

  // Decode/re-encode with the public helper shape, but an invalid envelope.
  std::vector<std::uint8_t> raw{
      0x5A, ControllerProtocol::EnvelopeRevision,
      ControllerProtocol::GetStatus, 3, 0};
  raw.push_back(UartProtocol::crc8(raw.data(),
                                   static_cast<std::uint8_t>(raw.size())));
  std::vector<std::uint8_t> malformed(1, 0);
  std::size_t codeIndex = 0;
  std::uint8_t code = 1;
  for (const auto value : raw) {
    if (value == 0) {
      malformed[codeIndex] = code;
      codeIndex = malformed.size();
      malformed.push_back(0);
      code = 1;
    } else {
      malformed.push_back(value);
      ++code;
    }
  }
  malformed[codeIndex] = code;
  malformed.push_back(0);
  serial.feed(malformed);

  // Invalid frames obey the same one-frame cooperative budget as valid host
  // commands, so a malicious burst cannot starve key/RF/safety service.
  protocol.service();
  protocol.service();
  protocol.service();
  require(capture.payloads.empty(), "invalid frame reached the handler");
  require(protocol.framingErrors() == 2,
          "malformed COBS/envelope framing errors were not counted");
  require(protocol.crcErrors() == 1, "bad CRC was not counted");
}

void testMacroScratchCannotCorruptSplitSerialFrame() {
  HardwareSerial serial;
  UartProtocol protocol(serial);
  Capture capture;
  capture.protocol = &protocol;
  protocol.begin(115200, captureFrame, &capture);

  const std::vector<std::uint8_t> expected{0, 4, 0, 5, 6, 0};
  const auto encoded = encode(ControllerProtocol::DisplayText, 19, expected);
  const auto split = encoded.size() / 2;
  serial.feed(std::vector<std::uint8_t>(encoded.begin(),
                                        encoded.begin() + split));
  protocol.service();
  require(capture.payloads.empty(), "partial frame dispatched before delimiter");

  // This is exactly what MacroQueue does between loop iterations: stage a
  // synchronous command while UART retains an unrelated partial COBS frame.
  auto *macroPayload = protocol.framePayloadScratch();
  std::fill(macroPayload, macroPayload + ControllerProtocol::MaximumPayload,
            static_cast<std::uint8_t>(0xA7));

  serial.feed(std::vector<std::uint8_t>(encoded.begin() + split,
                                        encoded.end()));
  protocol.service();
  require(capture.payloads.size() == 1 && capture.payloads[0] == expected,
          "macro scratch corrupted a split serial frame");
}

void testUndelimitedInputHasAByteBudget() {
  HardwareSerial serial;
  UartProtocol protocol(serial);
  Capture capture;
  capture.protocol = &protocol;
  protocol.begin(115200, captureFrame, &capture);

  const std::size_t excess = 11;
  serial.feed(std::vector<std::uint8_t>(
      static_cast<std::size_t>(UartProtocol::MaximumServiceBytes) + excess,
      0x55));
  protocol.service();
  require(serial.available() == static_cast<int>(excess) &&
              capture.payloads.empty(),
          "undelimited UART traffic exceeded its per-loop byte budget");
}

void testActionAndACKShareTheExactAcceptedEdge() {
  HardwareSerial serial;
  UartProtocol protocol(serial);
  const std::uint32_t acceptedAt = 0x89ABCDEF;
  for (const std::size_t actionLength : {std::size_t{0}, std::size_t{8}}) {
    serial.clearWritten();
    std::vector<std::uint8_t> action(4 + actionLength, 0);
    action[0] = 13; // ControllerEventType::Action
    action[1] = 2;  // InputEventSource::Host
    action[2] = actionLength == 0 ? ControllerProtocol::RelayAllOff
                                  : ControllerProtocol::RadioTransmit;
    action[3] = static_cast<std::uint8_t>(actionLength);
    require(protocol.sendEventAt(action.data(),
                                 static_cast<std::uint8_t>(action.size()),
                                 acceptedAt),
            "accepted Action evidence was not sent");
    require(protocol.sendAckAt(7, action[2], acceptedAt),
            "accepted ACK was not sent");
    const auto frames = decodeResponses(serial.written());
    require(frames.size() == 2 &&
                frames[0][2] == ControllerProtocol::Event &&
                frames[1][2] == ControllerProtocol::Ack &&
                trailingMicros(frames[0]) == acceptedAt &&
                trailingMicros(frames[1]) == acceptedAt,
            "Action and ACK timestamps differ from the accepted edge");
  }
}

} // namespace

int main() {
  try {
    testRepresentativeAndMaximumPayloads();
    testAdvisoryRevisionDoesNotBlockSemanticFrames();
    testInvalidFramesAreRejected();
    testMacroScratchCannotCorruptSplitSerialFrame();
    testUndelimitedInputHasAByteBudget();
    testActionAndACKShareTheExactAcceptedEdge();
    std::cout << "firmware_uart_protocol_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_uart_protocol_tests: " << error.what() << '\n';
    return 1;
  }
}
