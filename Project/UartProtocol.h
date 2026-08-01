#pragma once

#include <Arduino.h>

namespace ControllerProtocol {

constexpr uint8_t Magic = 0xA5;
constexpr uint8_t Version = 1;
constexpr uint8_t MaximumPayload = 48;

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
  PwmMode = 0x13,
  StatusRgb = 0x14,
  PwmGet = 0x15,
  AddressableLed = 0x16,

  RadioTransmit = 0x20,
  RadioLearnStart = 0x21,
  RadioLearnCancel = 0x22,
  RadioLearnClear = 0x23,
  RadioLearnList = 0x24,
  RadioLearnRemove = 0x25,
  RadioLearnMap = 0x26,

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
  Event = 0xA0,
};

enum Error : uint8_t {
  NoError = 0,
  BadEnvelope = 1,
  Unsupported = 2,
  BadPayload = 3,
  HardwareUnavailable = 4,
  Busy = 5,
  Unsafe = 6,
};

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
  static constexpr uint8_t RawOverhead = 6;
  static constexpr uint8_t MaximumRaw = MaximumPayload + RawOverhead;
  static constexpr uint8_t MaximumEncoded = MaximumRaw + 2;

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
  uint16_t framingErrors_ = 0;
  uint16_t crcErrors_ = 0;
  uint16_t responseErrors_ = 0;
};

} // namespace ControllerProtocol
