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
       const std::vector<std::uint8_t> &payload) {
  std::vector<std::uint8_t> raw{
      ControllerProtocol::Magic, ControllerProtocol::Version, opcode,
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

  require(capture.payloads.size() == 2, "valid frames were not dispatched");
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
      0x5A, ControllerProtocol::Version, ControllerProtocol::GetStatus, 3, 0};
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
            0xA7);

  serial.feed(std::vector<std::uint8_t>(encoded.begin() + split,
                                        encoded.end()));
  protocol.service();
  require(capture.payloads.size() == 1 && capture.payloads[0] == expected,
          "macro scratch corrupted a split serial frame");
}

} // namespace

int main() {
  try {
    testRepresentativeAndMaximumPayloads();
    testInvalidFramesAreRejected();
    testMacroScratchCannotCorruptSplitSerialFrame();
    std::cout << "firmware_uart_protocol_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_uart_protocol_tests: " << error.what() << '\n';
    return 1;
  }
}
