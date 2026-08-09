#pragma once

#include <stdint.h>

namespace ControllerProtocol {

constexpr uint8_t MaximumPayload = 48;

// Opcode is the platform-neutral native request, response, and event registry.
// AVR transport and VirtualBoard/shared policy code include this one contract.
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

enum Error : uint8_t {
  NoError = 0,
  BadEnvelope = 1,
  Unsupported = 2,
  BadPayload = 3,
  HardwareUnavailable = 4,
  Busy = 5,
  Unsafe = 6,
};

// EventType is the byte at the beginning of an asynchronous Event payload.
// It lives beside Opcode so the AVR, VirtualBoard, and host generators consume
// one platform-neutral registry instead of maintaining parallel definitions.
enum class EventType : uint8_t {
  Key = 1,
  Door = 2,
  Bluetooth = 3,
  PwmChannel = 4,
  RfLearned = 5,
  Macro = 6,
  Fault = 7,
  RfReceived = 8,
  RfLearning = 9,
  Relay = 10,
  Alert = 11,
  AppNavigation = 12,
  Action = 13,
};

// InputEventSource identifies the origin of accepted action evidence.
enum class InputEventSource : uint8_t {
  Physical = 0,
  Radio = 1,
  Host = 2,
};

enum FeatureProfile : uint8_t {
  FullPeripheralProfile = 0,
  MotionMacroProfile = 1,
  KeyDiagnosticProfile = 2,
  CustomFeatureProfile = 3,
};

enum BuildFeatureFlag : uint8_t {
  LocalMacroCapture = 1U << 0,
  UnifiedPageIdentifiesKeys = 1U << 1,
  ForceSilent = 1U << 2,
  BlankEepromSilent = 1U << 3,
  McuLcdRenderer = 1U << 4,
  LocalPcaPages = 1U << 5,
  StatusLedEngine = 1U << 6,
  IlluminationAutomation = 1U << 7,
};

} // namespace ControllerProtocol
