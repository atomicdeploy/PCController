#pragma once

#include <Arduino.h>

namespace ControllerProtocol {

// Native frame marker and bounded payload capacity for the AVR transport.
constexpr uint8_t Magic = 0xA5;
// Sent as an advisory envelope revision. Receivers validate the canonical
// magic/length/CRC shape and opcode payload semantics instead of rejecting an
// otherwise understandable peer solely because this byte differs.
constexpr uint8_t EnvelopeRevision = 1;
constexpr uint8_t MaximumPayload = 48;

// Opcode is the stable native request, response, and event operation registry.
enum Opcode : uint8_t {
  Hello = 0x01,
  GetStatus = 0x02,
  SetStreamPeriod = 0x03,
  GetSettings = 0x04,
  SetSettings = 0x05,
  TemperatureList = 0x06,

  Buzzer = 0x10,
  PwmSet = 0x11,
  PwmAllOff = 0x12,
  StatusRgb = 0x14,
  PwmGet = 0x15,
  AddressableLed = 0x16,
  StatusEffect = 0x17,
  StatusProfileGet = 0x18,
  StatusProfileSet = 0x19,

  RadioTransmit = 0x20,
  RadioLearnStart = 0x21,
  RadioLearnCancel = 0x22,
  RadioLearnClear = 0x23,
  RadioLearnList = 0x24,
  RadioLearnRemove = 0x25,

  MenuAction = 0x30,
  RelaySet = 0x31,
  RelaySide = 0x32,
  RelayAllOff = 0x33,
  RelayTest = 0x34,
  Reset = 0x35,
  I2cTransfer = 0x36,
  MenuSetPage = 0x37,
  DisplayText = 0x38,
  MacroStart = 0x39,
  MacroCancel = 0x3A,
  MacroStep = 0x3B,
  FrontPanelGet = 0x3C,
  RemoteKeyGesture = 0x3D,
  MenuList = 0x3E,
  RadioLearnReplace = 0x3F,
  MenuLayoutGet = 0x40,
  MenuLayoutSet = 0x41,
  // Host-owned application state: payload prefix [0=Idle, 1=Running].
  ProgramState = 0x45,

  Ack = 0x80,
  HelloResponse = 0x81,
  ErrorResponse = 0x82,
  StatusResponse = 0x90,
  SettingsResponse = 0x91,
  PwmValuesResponse = 0x92,
  I2cTransferResponse = 0x93,
  RadioLearnListResponse = 0x94,
  TemperatureListResponse = 0x95,
  FrontPanelResponse = 0x96,
  MenuListResponse = 0x97,
  MacroStatusResponse = 0x98,
  MenuLayoutResponse = 0x99,
  SegmentChanged = 0x9C,
  BuzzerChanged = 0x9D,
  StatusLedChanged = 0x9E,
  StatusProfileResponse = 0x9F,
  Event = 0xA0,
};

// Error is the compact protocol failure code returned by ErrorResponse.
enum Error : uint8_t {
  NoError = 0,
  BadEnvelope = 1,
  Unsupported = 2,
  BadPayload = 3,
  HardwareUnavailable = 4,
  Busy = 5,
  Unsafe = 6,
};

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
