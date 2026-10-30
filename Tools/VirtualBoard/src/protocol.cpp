#include "virtual_board/protocol.hpp"
#include "../../../Project/ProtocolCodec.h"

#include <algorithm>
#include <sstream>
#include <stdexcept>

namespace pccontroller::wire {

std::uint8_t crc8(const std::uint8_t *data, std::size_t length) {
  if (length > kMaximumRaw) throw std::invalid_argument("CRC input exceeds protocol bound");
  return ControllerProtocol::WireCodec::crc8(data, static_cast<uint8_t>(length));
}

std::vector<std::uint8_t> cobsEncode(const std::uint8_t *data,
                                     std::size_t length) {
  if (length > kMaximumRaw) throw std::invalid_argument("COBS input exceeds protocol bound");
  std::vector<uint8_t> encoded(kMaximumEncoded);
  encoded.resize(ControllerProtocol::WireCodec::cobsEncode(data,static_cast<uint8_t>(length),encoded.data(),static_cast<uint8_t>(encoded.size())));
  return encoded;
}

bool cobsDecode(const std::uint8_t *data, std::size_t length,
                std::vector<std::uint8_t> &decoded) {
  if (length > kMaximumEncoded) return false;
  decoded.resize(kMaximumRaw);
  const auto n=ControllerProtocol::WireCodec::cobsDecode(data,static_cast<uint8_t>(length),decoded.data(),static_cast<uint8_t>(decoded.size()));
  decoded.resize(n);
  return n!=0;
}

std::vector<std::uint8_t> encode(const Frame &frame) {
  if (frame.payload.size() > kMaximumPayload) {
    throw std::invalid_argument("protocol payload exceeds 48 bytes");
  }

  std::vector<std::uint8_t> raw;
  raw.reserve(frame.payload.size() + kRawOverhead);
  raw.push_back(kMagic);
  raw.push_back(kReservedEnvelopeByte);
  raw.push_back(frame.opcode);
  raw.push_back(frame.sequence);
  raw.push_back(static_cast<std::uint8_t>(frame.payload.size()));
  raw.insert(raw.end(), frame.payload.begin(), frame.payload.end());
  raw.push_back(crc8(raw.data(), raw.size()));

  auto encoded = cobsEncode(raw.data(), raw.size());
  encoded.push_back(0);
  return encoded;
}

DecodeResult decode(const std::uint8_t *encoded, std::size_t length) {
  if (length != 0 && encoded[length - 1] == 0) {
    --length;
  }
  if (length == 0 || length > kMaximumEncoded) {
    return {{}, DecodeError::Framing, "empty or oversized encoded frame"};
  }

  std::vector<std::uint8_t> raw;
  if (!cobsDecode(encoded, length, raw)) {
    return {{}, DecodeError::Framing, "malformed COBS frame"};
  }
  if (raw.size() < kRawOverhead || raw.size() > kMaximumRaw) {
    return {{}, DecodeError::Framing, "raw frame length is invalid"};
  }
  if (raw[0] != kMagic) {
    return {{}, DecodeError::Framing, "frame magic mismatch"};
  }
  if (raw[1] != kReservedEnvelopeByte) {
    return {{}, DecodeError::Framing, "foreign protocol namespace"};
  }
  const std::size_t payloadLength = raw[4];
  if (payloadLength > kMaximumPayload ||
      raw.size() != payloadLength + kRawOverhead) {
    return {{}, DecodeError::Framing, "payload length mismatch"};
  }
  const std::uint8_t expected = crc8(raw.data(), raw.size() - 1U);
  if (raw.back() != expected) {
    std::ostringstream message;
    message << "CRC mismatch";
    return {{}, DecodeError::Crc, message.str()};
  }

  Frame frame;
  frame.opcode = raw[2];
  frame.sequence = raw[3];
  frame.payload.assign(raw.begin() + 5, raw.end() - 1);
  return {std::move(frame), DecodeError::None, {}};
}

DecodeBatch StreamDecoder::feed(const std::uint8_t *data,
                                std::size_t length) {
  DecodeBatch batch;
  for (std::size_t index = 0; index < length; ++index) {
    const std::uint8_t value = data[index];
    if (value == 0) {
      if (!dropping_ && !encoded_.empty()) {
        DecodeResult result = decode(encoded_.data(), encoded_.size());
        if (result) {
          batch.frames.push_back(std::move(result.frame));
        } else if (result.error == DecodeError::Crc) {
          ++batch.crcErrors;
        } else {
          ++batch.framingErrors;
        }
      }
      encoded_.clear();
      dropping_ = false;
      continue;
    }
    if (dropping_) {
      continue;
    }
    if (encoded_.size() >= kMaximumEncoded) {
      encoded_.clear();
      dropping_ = true;
      ++batch.framingErrors;
      continue;
    }
    encoded_.push_back(value);
  }
  return batch;
}

void StreamDecoder::reset() {
  encoded_.clear();
  dropping_ = false;
}

} // namespace pccontroller::wire
