#pragma once

#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace pccontroller::wire {

constexpr std::uint8_t kMagic = 0xA5;
constexpr std::uint8_t kEnvelopeRevision = 1;
constexpr std::size_t kMaximumPayload = 48;
constexpr std::size_t kRawOverhead = 6;
constexpr std::size_t kMaximumRaw = kMaximumPayload + kRawOverhead;
constexpr std::size_t kMaximumEncoded = kMaximumRaw + 2;

enum Opcode : std::uint8_t {
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
  HostMenuDirectory = 0x42,
  HostMenuContent = 0x43,
  HostMenuStateGet = 0x44,
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
  HostMenuContentRequest = 0x9A,
  HostMenuStateResponse = 0x9B,
  SegmentChanged = 0x9C,
  BuzzerChanged = 0x9D,
  StatusLedChanged = 0x9E,
  StatusProfileResponse = 0x9F,
  Event = 0xA0,
};

enum Error : std::uint8_t {
  NoError = 0,
  BadEnvelope = 1,
  Unsupported = 2,
  BadPayload = 3,
  HardwareUnavailable = 4,
  Busy = 5,
  Unsafe = 6,
};

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
