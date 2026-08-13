#pragma once

// Target-neutral native wire contract.
//
// This file intentionally has no Arduino, STL, allocation, clock, or
// transport dependency. It is the single fixed-width vocabulary shared by
// the AVR UART adapter now and native targets as they are migrated. Keep
// wire values stable: support is advertised through capabilities, not by
// renumbering an operation for a smaller firmware build.

#include <stdint.h>

namespace ControllerProtocol {
namespace WireContract {

// Native envelope marker, advisory revision, and bounded payload/frame sizes.
constexpr uint8_t Magic = 0xA5;
constexpr uint8_t EnvelopeRevision = 1;
constexpr uint8_t MaximumPayload = 48;
constexpr uint8_t RawFrameOverhead = 6;
constexpr uint8_t MaximumRawFrame = MaximumPayload + RawFrameOverhead;
constexpr uint8_t MaximumEncodedFrame = MaximumRawFrame + 2;

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
  // Host extensions are declared here even when a reduced AVR build does not
  // implement or advertise them. Their wire values must not drift.
  HostMenuDirectory = 0x42,
  HostMenuContent = 0x43,
  HostMenuStateGet = 0x44,
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
  HostMenuContentRequest = 0x9A,
  HostMenuStateResponse = 0x9B,
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

static_assert(MaximumRawFrame < 254,
              "native COBS contract requires one bounded code block");
static_assert(HostMenuDirectory == 0x42 && StatusLedChanged == 0x9E,
              "native opcode values are part of the wire contract");

} // namespace WireContract

// Preserve the established ControllerProtocol::* source API while moving the
// values themselves into the target-neutral contract namespace. A using
// directive is intentional here: it does not duplicate storage or values on
// AVR, and existing firmware includes continue to compile unchanged.
using namespace WireContract;

} // namespace ControllerProtocol
