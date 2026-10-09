#include "UartProtocol.h"

#include <string.h>

namespace ControllerProtocol {

UartProtocol::UartProtocol(HardwareSerial &serial) : serial_(serial) {}

void UartProtocol::begin(uint32_t baud, FrameHandler handler, void *context) {
  handler_ = handler;
  context_ = context;
  receiveLength_ = 0;
  dropping_ = false;
  serial_.begin(baud);
}

void UartProtocol::service() {
  while (serial_.available() > 0) {
    const uint8_t value = static_cast<uint8_t>(serial_.read());
    if (value == 0) {
      if (!dropping_ && receiveLength_ != 0) {
        processEncodedFrame();
      }
      receiveLength_ = 0;
      dropping_ = false;
      continue;
    }

    if (dropping_) {
      continue;
    }
    if (receiveLength_ >= sizeof(receive_)) {
      dropping_ = true;
      receiveLength_ = 0;
      if (framingErrors_ != UINT16_MAX) {
        ++framingErrors_;
      }
      continue;
    }
    receive_[receiveLength_++] = value;
  }
}

bool UartProtocol::send(uint8_t opcode, uint8_t sequence,
                        const uint8_t *payload, uint8_t payloadLength) {
  const bool timedEvent = opcode == Event && payloadLength != 0;
  const bool timed = timedEvent || opcode == Ack ||
                     (opcode == ErrorResponse && sequence == 0xFE);
  return sendTimestamped(opcode, sequence, payload, payloadLength, timedEvent,
                         timed, timed ? micros() : 0);
}

bool UartProtocol::sendEventAt(const uint8_t *payload, uint8_t payloadLength,
                               uint32_t capturedAtUs) {
  return sendTimestamped(Event, 0, payload, payloadLength, true, true,
                         capturedAtUs);
}

bool UartProtocol::sendTimestamped(uint8_t opcode, uint8_t sequence,
                                   const uint8_t *payload,
                                   uint8_t payloadLength, bool timedEvent,
                                   bool timed, uint32_t capturedAtUs) {
  if (payloadLength > static_cast<uint8_t>(MaximumPayload -
                                           (timed ? 4 : 0)) ||
      (payloadLength != 0 && payload == nullptr)) {
    return false;
  }

  raw_[0] = Magic;
  raw_[1] = EnvelopeRevision;
  raw_[2] = opcode;
  raw_[3] = sequence;
  raw_[4] = static_cast<uint8_t>(payloadLength + (timed ? 4 : 0));
  if (payloadLength != 0) {
    memcpy(raw_ + 5, payload, payloadLength);
  }
  if (timed) {
    // Schema-2 events set the type high bit and carry the MCU clock, so event
    // ordering never depends on USB/network arrival jitter.
    if (timedEvent) {
      raw_[5] |= 0x80;
    }
    memcpy(raw_ + 5 + payloadLength, &capturedAtUs, sizeof(capturedAtUs));
  }
  const uint8_t rawLength = static_cast<uint8_t>(raw_[4] + RawOverhead);
  raw_[rawLength - 1] = crc8(raw_, static_cast<uint8_t>(rawLength - 1));

  if (!writeCobs(raw_, rawLength)) {
    return false;
  }
  serial_.write(static_cast<uint8_t>(0));
  return true;
}

bool UartProtocol::sendAck(uint8_t sequence, uint8_t requestOpcode) {
  const uint8_t payload[] = {requestOpcode, NoError};
  return send(Ack, sequence, payload, sizeof(payload));
}

bool UartProtocol::sendError(uint8_t sequence, uint8_t requestOpcode,
                             Error error) {
  if (responseErrors_ != UINT16_MAX) {
    ++responseErrors_;
  }
  const uint8_t payload[] = {requestOpcode, static_cast<uint8_t>(error)};
  return send(ErrorResponse, sequence, payload, sizeof(payload));
}

uint16_t UartProtocol::framingErrors() const { return framingErrors_; }

uint16_t UartProtocol::crcErrors() const { return crcErrors_; }

uint16_t UartProtocol::responseErrors() const { return responseErrors_; }

uint8_t *UartProtocol::framePayloadScratch() { return raw_ + 5; }

uint8_t UartProtocol::crc8(const uint8_t *data, uint8_t length) {
  uint8_t crc = 0;
  while (length-- != 0) {
    crc ^= *data++;
    for (uint8_t bit = 0; bit < 8; ++bit) {
      crc = (crc & 0x80) ? static_cast<uint8_t>((crc << 1) ^ 0x07)
                         : static_cast<uint8_t>(crc << 1);
    }
  }
  return crc;
}

bool UartProtocol::writeCobs(const uint8_t *input, uint8_t length) {
  static_assert(MaximumRaw < 254,
                "Streaming COBS assumes every frame fits in one code block");
  uint8_t readIndex = 0;
  do {
    const uint8_t blockStart = readIndex;
    while (readIndex < length && input[readIndex] != 0) {
      ++readIndex;
    }
    const uint8_t blockLength = static_cast<uint8_t>(readIndex - blockStart);
    if (serial_.write(static_cast<uint8_t>(blockLength + 1)) != 1 ||
        (blockLength != 0 &&
         serial_.write(input + blockStart, blockLength) != blockLength)) {
      return false;
    }
    if (readIndex < length) {
      ++readIndex;
    } else {
      break;
    }
  } while (readIndex <= length);
  return true;
}

uint8_t UartProtocol::cobsDecode(const uint8_t *input, uint8_t length,
                                 uint8_t *output, uint8_t capacity) {
  uint8_t readIndex = 0;
  uint8_t writeIndex = 0;
  while (readIndex < length) {
    const uint8_t code = input[readIndex++];
    if (code == 0) {
      return 0;
    }
    const uint8_t count = static_cast<uint8_t>(code - 1);
    if (count > static_cast<uint8_t>(length - readIndex) ||
        count > static_cast<uint8_t>(capacity - writeIndex)) {
      return 0;
    }
    for (uint8_t index = 0; index < count; ++index) {
      output[writeIndex++] = input[readIndex++];
    }
    if (code != 0xFF && readIndex < length) {
      if (writeIndex >= capacity) {
        return 0;
      }
      output[writeIndex++] = 0;
    }
  }
  return writeIndex;
}

void UartProtocol::processEncodedFrame() {
  // COBS decode is safe in-place: after each code byte is consumed, the write
  // cursor can only trail the unread input cursor. Keeping the decoded request
  // in RX storage leaves raw_ available for nested ACK/error/response writes.
  const uint8_t rawLength =
      cobsDecode(receive_, receiveLength_, receive_, sizeof(receive_));
  // The revision byte is advisory: magic, bounded shape, CRC, and each known
  // opcode's semantic validation decide whether a frame is understandable.
  // This lets reduced/newer feature sets interoperate without build-specific
  // branches; unknown operations still receive Unsupported from the handler.
  if (rawLength < RawOverhead || receive_[0] != Magic ||
      receive_[4] > MaximumPayload ||
      rawLength != static_cast<uint8_t>(receive_[4] + RawOverhead)) {
    if (framingErrors_ != UINT16_MAX) {
      ++framingErrors_;
    }
    return;
  }
  if (receive_[rawLength - 1] !=
      crc8(receive_, static_cast<uint8_t>(rawLength - 1))) {
    if (crcErrors_ != UINT16_MAX) {
      ++crcErrors_;
    }
    return;
  }

  if (handler_ == nullptr) {
    return;
  }
  const Frame frame = {receive_[2], receive_[3], receive_[4], receive_ + 5};
  handler_(frame, context_);
}

} // namespace ControllerProtocol
