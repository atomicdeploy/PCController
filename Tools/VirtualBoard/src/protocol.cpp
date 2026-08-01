#include "virtual_board/protocol.hpp"

#include <algorithm>
#include <sstream>
#include <stdexcept>

namespace pccontroller::wire {

std::uint8_t crc8(const std::uint8_t *data, std::size_t length) {
  std::uint8_t crc = 0;
  for (std::size_t index = 0; index < length; ++index) {
    crc ^= data[index];
    for (std::uint8_t bit = 0; bit < 8; ++bit) {
      crc = (crc & 0x80U) != 0U
                ? static_cast<std::uint8_t>((crc << 1U) ^ 0x07U)
                : static_cast<std::uint8_t>(crc << 1U);
    }
  }
  return crc;
}

std::vector<std::uint8_t> cobsEncode(const std::uint8_t *data,
                                     std::size_t length) {
  std::vector<std::uint8_t> encoded;
  encoded.reserve(length + length / 254U + 1U);
  encoded.push_back(0);

  std::size_t codeIndex = 0;
  std::uint8_t code = 1;
  for (std::size_t index = 0; index < length; ++index) {
    const std::uint8_t value = data[index];
    if (value == 0) {
      encoded[codeIndex] = code;
      codeIndex = encoded.size();
      encoded.push_back(0);
      code = 1;
      continue;
    }

    encoded.push_back(value);
    ++code;
    if (code == 0xFFU) {
      encoded[codeIndex] = code;
      codeIndex = encoded.size();
      encoded.push_back(0);
      code = 1;
    }
  }
  encoded[codeIndex] = code;
  return encoded;
}

bool cobsDecode(const std::uint8_t *data, std::size_t length,
                std::vector<std::uint8_t> &decoded) {
  decoded.clear();
  decoded.reserve(length);
  std::size_t read = 0;
  while (read < length) {
    const std::uint8_t code = data[read++];
    if (code == 0) {
      return false;
    }
    const std::size_t count = static_cast<std::size_t>(code - 1U);
    if (count > length - read) {
      return false;
    }
    decoded.insert(decoded.end(), data + read, data + read + count);
    read += count;
    if (code != 0xFFU && read < length) {
      decoded.push_back(0);
    }
  }
  return true;
}

std::vector<std::uint8_t> encode(const Frame &frame) {
  if (frame.payload.size() > kMaximumPayload) {
    throw std::invalid_argument("protocol payload exceeds 48 bytes");
  }

  std::vector<std::uint8_t> raw;
  raw.reserve(frame.payload.size() + kRawOverhead);
  raw.push_back(kMagic);
  raw.push_back(kVersion);
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
  if (raw[0] != kMagic || raw[1] != kVersion) {
    return {{}, DecodeError::Framing, "magic or protocol version mismatch"};
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

Frame makeAck(std::uint8_t sequence, std::uint8_t requestOpcode) {
  return {Ack, sequence, {requestOpcode, NoError}};
}

Frame makeError(std::uint8_t sequence, std::uint8_t requestOpcode,
                Error error) {
  return {ErrorResponse, sequence,
          {requestOpcode, static_cast<std::uint8_t>(error)}};
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
