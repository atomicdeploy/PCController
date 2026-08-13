#pragma once

#include <Arduino.h>

#include "ProtocolCodec.h"
#include "ProtocolContract.h"

namespace ControllerProtocol {

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
  explicit UartProtocol(HardwareSerial &serial);

  void begin(uint32_t baud, FrameHandler handler, void *context = nullptr);
  void service();

  bool send(uint8_t opcode, uint8_t sequence, const uint8_t *payload = nullptr,
            uint8_t payloadLength = 0);
  bool sendAck(uint8_t sequence, uint8_t requestOpcode);
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
  static constexpr uint8_t RawOverhead = WireContract::RawFrameOverhead;
  static constexpr uint8_t MaximumRaw = WireContract::MaximumRawFrame;
  static constexpr uint8_t MaximumEncoded =
      WireContract::MaximumEncodedFrame;

  bool writeCobs(const uint8_t *input, uint8_t length);
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
  uint16_t framingErrors_ = 0;
  uint16_t crcErrors_ = 0;
  uint16_t responseErrors_ = 0;
};

} // namespace ControllerProtocol
