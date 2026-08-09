#pragma once

#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

#include "Project/ProtocolContract.h"

namespace pccontroller::wire {

constexpr std::uint8_t kMagic = 0xA5;
constexpr std::uint8_t kEnvelopeRevision = 1;
constexpr std::size_t kMaximumPayload = 48;
constexpr std::size_t kRawOverhead = 6;
constexpr std::size_t kMaximumRaw = kMaximumPayload + kRawOverhead;
constexpr std::size_t kMaximumEncoded = kMaximumRaw + 2;

// The simulator deliberately exposes the production platform-neutral names in
// its wire namespace. Do not add a second opcode/error registry here.
using namespace ControllerProtocol;

struct Frame {
  std::uint8_t opcode = 0;
  std::uint8_t sequence = 0;
  std::vector<std::uint8_t> payload;
};

enum class DecodeError {
  None,
  Framing,
  Crc,
};

struct DecodeResult {
  Frame frame;
  DecodeError error = DecodeError::None;
  std::string message;

  explicit operator bool() const { return error == DecodeError::None; }
};

struct DecodeBatch {
  std::vector<Frame> frames;
  std::size_t framingErrors = 0;
  std::size_t crcErrors = 0;
};

std::uint8_t crc8(const std::uint8_t *data, std::size_t length);
std::vector<std::uint8_t> cobsEncode(const std::uint8_t *data,
                                     std::size_t length);
bool cobsDecode(const std::uint8_t *data, std::size_t length,
                std::vector<std::uint8_t> &decoded);
std::vector<std::uint8_t> encode(const Frame &frame);
DecodeResult decode(const std::uint8_t *encoded, std::size_t length);

Frame makeAck(std::uint8_t sequence, std::uint8_t requestOpcode);
Frame makeError(std::uint8_t sequence, std::uint8_t requestOpcode,
                Error error);

class StreamDecoder {
public:
  DecodeBatch feed(const std::uint8_t *data, std::size_t length);
  void reset();

private:
  std::vector<std::uint8_t> encoded_;
  bool dropping_ = false;
};

} // namespace pccontroller::wire
