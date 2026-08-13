#include "../../../Project/ProtocolCodec.h"

#include <array>
#include <cstdint>
#include <iostream>
#include <stdexcept>

namespace {

using ControllerProtocol::WireCodec::cobsDecode;
using ControllerProtocol::WireCodec::crc8;

void require(bool condition, const char *message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

void testCrc8AtmVectors() {
  const std::array<std::uint8_t, 9> check{{'1', '2', '3', '4', '5',
                                            '6', '7', '8', '9'}};
  const std::array<std::uint8_t, 6> envelope{{0xA5, 0x01, 0x17, 0x03,
                                               0x01, 0x80}};
  require(crc8(nullptr, 0) == 0, "empty CRC-8 vector changed");
  require(crc8(check.data(), static_cast<std::uint8_t>(check.size())) == 0xF4,
          "CRC-8/ATM check vector changed");
  require(crc8(envelope.data(),
               static_cast<std::uint8_t>(envelope.size())) == 0x1C,
          "native envelope CRC vector changed");
}

void testCobsRoundTripVector() {
  const std::array<std::uint8_t, 7> encoded{{2, 0x11, 3, 0x22, 0x33, 2,
                                              0x44}};
  const std::array<std::uint8_t, 6> expected{{0x11, 0, 0x22, 0x33, 0,
                                               0x44}};
  std::array<std::uint8_t, 8> output{{0xEE, 0xEE, 0xEE, 0xEE,
                                       0xEE, 0xEE, 0xEE, 0xEE}};
  const auto length = cobsDecode(encoded.data(),
                                 static_cast<std::uint8_t>(encoded.size()),
                                 output.data(),
                                 static_cast<std::uint8_t>(output.size()));
  require(length == expected.size(), "COBS decoded length changed");
  for (std::size_t index = 0; index < expected.size(); ++index) {
    require(output[index] == expected[index], "COBS decoded bytes changed");
  }
}

void testCobsRejectsMalformedAndCapacityOverflow() {
  const std::array<std::uint8_t, 1> zeroCode{{0}};
  const std::array<std::uint8_t, 2> truncated{{3, 0x42}};
  const std::array<std::uint8_t, 3> tooSmall{{3, 0x42, 0x43}};
  std::array<std::uint8_t, 3> output{{0, 0, 0}};

  require(cobsDecode(zeroCode.data(), 1, output.data(), 3) == 0,
          "COBS accepted a zero code");
  require(cobsDecode(truncated.data(), 2, output.data(), 3) == 0,
          "COBS accepted a truncated block");
  require(cobsDecode(tooSmall.data(), 3, output.data(), 1) == 0,
          "COBS accepted insufficient output capacity");
}

void testCobsMaxCodeDoesNotInsertZero() {
  std::array<std::uint8_t, 255> encoded{};
  std::array<std::uint8_t, 254> output{};
  encoded[0] = 0xFF;
  for (std::size_t index = 0; index < output.size(); ++index) {
    encoded[index + 1] = static_cast<std::uint8_t>(index + 1);
  }

  const auto length = cobsDecode(encoded.data(),
                                 static_cast<std::uint8_t>(encoded.size()),
                                 output.data(),
                                 static_cast<std::uint8_t>(output.size()));
  require(length == output.size(), "COBS max code length changed");
  for (std::size_t index = 0; index < output.size(); ++index) {
    require(output[index] == static_cast<std::uint8_t>(index + 1),
            "COBS max code inserted or changed a byte");
  }
}

} // namespace

int main() {
  try {
    testCrc8AtmVectors();
    testCobsRoundTripVector();
    testCobsRejectsMalformedAndCapacityOverflow();
    testCobsMaxCodeDoesNotInsertZero();
    std::cout << "firmware_protocol_codec_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_protocol_codec_tests: " << error.what() << '\n';
    return 1;
  }
}
