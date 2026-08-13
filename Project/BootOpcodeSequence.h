#pragma once

#include <Arduino.h>

#include "UartProtocol.h"

// BootOpcodeSequence executes a bounded EEPROM record only after the regular
// controller startup has initialized relay/PWM safety, settings, and inputs.
// Entries become ordinary protocol frames; it never owns a raw peripheral.
class BootOpcodeSequence {
public:
  // Internal frames do not represent host traffic and must not answer on UART.
  static constexpr uint8_t ExecutionSequence = 0xFD;

  using Dispatch = void (*)(const ControllerProtocol::Frame &frame,
                            void *context);

  // A valid record dispatches its bounded FIFO entries. Blank, torn, corrupt,
  // unknown, or unsafe storage is intentionally a quiet no-op; factory EEPROM
  // provisioning supplies the welcome melody when this feature is enabled.
  static uint8_t dispatch(ControllerProtocol::UartProtocol &protocol,
                          Dispatch callback, void *context = nullptr);

  // The firmware dispatcher recognizes this private context rather than a
  // wire sequence value, so a host cannot impersonate an internal boot frame.
  static void *executionContext();
  static bool isExecutionContext(const void *context);

private:
  static constexpr uint8_t MetadataBytes = 5;
  static constexpr uint8_t DataOffset = MetadataBytes;
  static constexpr uint8_t DataBytes = 26;
  static constexpr uint8_t CommitOffset = 31;
  static constexpr uint8_t MaximumDispatches = 6;

  static uint8_t payloadLength(uint8_t opcode);
};
