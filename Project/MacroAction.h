#pragma once

#include <stdint.h>

#include "UartProtocol.h"

// Single firmware-side registry for normal actions that can be evidenced,
// captured, streamed, and replayed through the ordinary dispatcher.  Lifecycle
// commands, EEPROM/settings writes, reset, learning, and raw I2C stay out of
// the ring so recording cannot mutate controller state unexpectedly.
namespace MacroAction {

constexpr uint8_t MaximumPayload = 8;
constexpr uint8_t RecordHeaderBytes = 6; // due-us LE32, opcode, payload length
constexpr uint8_t InvalidPayload = 0xFE;
constexpr uint8_t VariablePayload = 0xFF;

constexpr uint8_t payloadLength(uint8_t opcode) {
  using namespace ControllerProtocol;
  switch (opcode) {
#define PCCONTROLLER_MACRO_ACTION(name, captureLength) \
    case name:                                          \
      return captureLength;
#include "MacroActions.inc.h"
#undef PCCONTROLLER_MACRO_ACTION
    default:
      return InvalidPayload;
  }
}

constexpr bool playbackAllowed(uint8_t opcode) {
  return payloadLength(opcode) != InvalidPayload;
}

constexpr bool validPlaybackPayload(uint8_t opcode, uint8_t payloadBytes) {
  if (!playbackAllowed(opcode) ||
      payloadBytes > ControllerProtocol::MaximumPayload) {
    return false;
  }
  const uint8_t fixed = payloadLength(opcode);
  return fixed == VariablePayload || payloadBytes == fixed;
}

constexpr bool recordable(uint8_t opcode, uint8_t availablePayload) {
  const uint8_t required = payloadLength(opcode);
  return availablePayload >= required && required <= MaximumPayload;
}

constexpr bool macroQueueableOpcode(uint8_t opcode) {
  return playbackAllowed(opcode);
}

static_assert(MaximumPayload + RecordHeaderBytes < 128,
              "one canonical action no longer fits the AVR macro ring");

} // namespace MacroAction
