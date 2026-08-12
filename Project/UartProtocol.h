#pragma once

#include <Arduino.h>

#include "ProtocolContract.h"

namespace ControllerProtocol {

// Native frame marker and bounded payload capacity for the AVR transport.
constexpr uint8_t Magic = 0xA5;
// Sent as an advisory envelope revision. Receivers validate the canonical
// magic/length/CRC shape and opcode payload semantics instead of rejecting an
// otherwise understandable peer solely because this byte differs.
constexpr uint8_t EnvelopeRevision = 1;
// Frame is a validated zero-copy view of one decoded native packet.
struct Frame {
  uint8_t opcode;
  uint8_t sequence;
  uint8_t payloadLength;
  // The payload is a short-lived view into protocol-owned scratch storage.
  // Handlers must consume it synchronously and never retain this pointer.
  const uint8_t *payload;
};

#if defined(__AVR__)
static_assert(sizeof(Frame) == 5,
              "UART frame view regressed into AVR stack-owned payload data");
#endif

using FrameHandler = void (*)(const Frame &frame, void *context);

// UartProtocol incrementally decodes COBS frames and owns bounded RX/TX scratch.
class UartProtocol {
public:
  // One service turn consumes no more than one maximum encoded packet's worth
  // of bytes, even when a hostile stream never supplies a delimiter.
  static constexpr uint8_t MaximumServiceBytes = MaximumPayload + 8;

  explicit UartProtocol(HardwareSerial &serial);

  void begin(uint32_t baud, FrameHandler handler, void *context = nullptr);
  void service();

  bool send(uint8_t opcode, uint8_t sequence, const uint8_t *payload = nullptr,
            uint8_t payloadLength = 0);
  // Preserves the action's sampling edge when reporting board-origin macro
  // capture.  Ordinary events keep their existing micros() timestamp.
  bool sendEventAt(const uint8_t *payload, uint8_t payloadLength,
                   uint32_t capturedAtUs);
  bool sendAck(uint8_t sequence, uint8_t requestOpcode);
  bool sendAckAt(uint8_t sequence, uint8_t requestOpcode,
                 uint32_t capturedAtUs);
  bool sendError(uint8_t sequence, uint8_t requestOpcode, Error error);

  uint16_t framingErrors() const;
  uint16_t crcErrors() const;
  uint16_t responseErrors() const;

  // Macro playback stages one synchronous request in the TX scratch. Its
  // handler must consume the view before sending the ACK/response that reuses
  // this storage; unlike RX scratch, a partial serial frame can never occupy it.
  uint8_t *framePayloadScratch();

  static uint8_t crc8(const uint8_t *data, uint8_t length);

private:
  static constexpr uint8_t RawOverhead = 6;
  static constexpr uint8_t MaximumRaw = MaximumPayload + RawOverhead;
  static constexpr uint8_t MaximumEncoded = MaximumServiceBytes;

  bool writeCobs(const uint8_t *input, uint8_t length);
  static uint8_t cobsDecode(const uint8_t *input, uint8_t length,
                            uint8_t *output, uint8_t capacity);
  void processEncodedFrame();

  HardwareSerial &serial_;
  FrameHandler handler_ = nullptr;
  void *context_ = nullptr;
  // TX and RX remain separate so a serial handler may respond without
  // invalidating its zero-copy request view. RX is decoded in-place; TX also
  // stages synchronous MCU-timed macro requests before their response reuses it.
  uint8_t raw_[MaximumRaw];
  uint8_t receive_[MaximumEncoded];
  uint8_t receiveLength_ = 0;
  bool dropping_ = false;
  // A board-origin action samples its timestamp before any later work.  The
  // one-shot override lets the established send() path retain that edge.
  bool timingOverrideActive_ = false;
  uint32_t timingOverrideUs_ = 0;
  uint16_t framingErrors_ = 0;
  uint16_t crcErrors_ = 0;
  uint16_t responseErrors_ = 0;
};

} // namespace ControllerProtocol
