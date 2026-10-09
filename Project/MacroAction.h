#pragma once

#include <stdint.h>

#include "ProtocolContract.h"

// MacroAction is the single firmware-side registry for actions that may be
// evidenced, captured, streamed, and replayed through the ordinary opcode
// dispatcher. Control-plane operations (settings, reset, learning, macro
// lifecycle, raw I2C) are deliberately excluded.
namespace MacroAction {

constexpr uint8_t MaximumPayload = 8;
constexpr uint8_t RecordHeaderBytes = 6; // due-us LE32, opcode, payload length

// Returns the canonical captured prefix length, or 0xFF when an opcode must
// never enter a macro. Accepted protocol extensions beyond this prefix remain
// forward-readable but are not copied into the small MCU ring.
constexpr uint8_t payloadLength(uint8_t opcode) {
  using namespace ControllerProtocol;
  switch (opcode) {
#define PCCONTROLLER_MACRO_ACTION(name, captureLength)                       \
    case name:                                                               \
      return captureLength;
#include "MacroActions.inc.h"
#undef PCCONTROLLER_MACRO_ACTION
    default:
      return 0xFF;
  }
}

constexpr bool playbackAllowed(uint8_t opcode) {
  using namespace ControllerProtocol;
  switch (opcode) {
#define PCCONTROLLER_MACRO_ACTION(name, captureLength) case name:
#include "MacroActions.inc.h"
#undef PCCONTROLLER_MACRO_ACTION
      return true;
    default:
      return false;
  }
}

// Fixed-shape actions must use their exact canonical payload on raw playback.
// Variable-shape rows remain bounded by the native frame and are validated by
// the ordinary opcode dispatcher immediately before touching hardware.
constexpr bool validPlaybackPayload(uint8_t opcode, uint8_t payloadBytes) {
  if (!playbackAllowed(opcode) ||
      payloadBytes > ControllerProtocol::MaximumPayload) {
    return false;
  }
  const uint8_t fixed = payloadLength(opcode);
  return fixed == 0xFF || payloadBytes == fixed;
}

constexpr bool recordable(uint8_t opcode, uint8_t availablePayload) {
  const uint8_t required = payloadLength(opcode);
  return required != 0xFF && availablePayload >= required &&
         required <= MaximumPayload;
}

constexpr bool macroQueueableOpcode(uint8_t opcode) {
  return playbackAllowed(opcode);
}

static_assert(MaximumPayload + RecordHeaderBytes < 128,
              "one canonical action no longer fits the AVR macro ring");

} // namespace MacroAction
