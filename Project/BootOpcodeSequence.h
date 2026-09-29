#pragma once

#include <Arduino.h>

#include "EepromLayout.h"
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

  // A valid record dispatches bounded buzzer/RGB entries. Blank, torn,
  // corrupt, unknown, or unsafe storage is intentionally a quiet no-op;
  // factory EEPROM provisioning supplies the welcome cue when enabled.
  static uint8_t dispatch(ControllerProtocol::UartProtocol &protocol,
                          Dispatch callback, void *context = nullptr);

  // The firmware dispatcher recognizes this private context rather than a
  // wire sequence value, so a host cannot impersonate an internal boot frame.
  static void *executionContext() { return reinterpret_cast<void *>(1); }
  static bool isExecutionContext(const void *context) {
    return context == reinterpret_cast<const void *>(1);
  }

private:
  static constexpr uint8_t MetadataBytes = 3;
  static constexpr uint8_t DataOffset = MetadataBytes;
  static constexpr uint8_t DataBytes =
      EepromLayout::BootOpcodeBytes - MetadataBytes - 1;
  static constexpr uint8_t CommitOffset =
      EepromLayout::BootOpcodeBytes - 1;

  static uint8_t payloadLength(uint8_t opcode);
};
