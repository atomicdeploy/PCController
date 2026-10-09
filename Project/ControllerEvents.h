#pragma once

#include <Arduino.h>

#include "MacroAction.h"
#include "UartProtocol.h"

using ControllerEventType = ControllerProtocol::EventType;
using InputEventSource = ControllerProtocol::InputEventSource;

// ControllerAlertKind classifies board-generated warning notifications.
enum class ControllerAlertKind : uint8_t {
  Fault = 1,
  Hot = 2,
};

// Owns the compact asynchronous event wire format. Keeping event encoding in
// one domain prevents menu, RF, sensor, and macro code from drifting apart.
class ControllerEvents {
public:
  explicit ControllerEvents(ControllerProtocol::UartProtocol &protocol);

  // Emits stable key ID, gesture, source channel, and optional RF source ID.
  void key(uint8_t bit, uint8_t gesture,
           InputEventSource source = InputEventSource::Physical,
           uint8_t sourceId = 0xFF);
  // Emits one successful canonical action. Larger/control-plane operations are
  // excluded by MacroAction rather than being truncated ambiguously.
  bool action(InputEventSource source, uint8_t opcode, const uint8_t *payload,
              uint8_t availablePayload, uint32_t capturedAtUs);
  // State-change methods emit only normalized event payloads, never debug text.
  void door(bool open);
  void bluetooth(uint8_t state);
  void pwmChannel(uint8_t channel);
  void rfLearned(uint8_t id);
  // state: 0=ended, 1=canceled, 2=full, 3=started, 4=timer progress.
  void rfLearning(uint8_t state, uint8_t count, uint8_t mode,
                  uint8_t totalSeconds, uint8_t remainingSeconds);
  void rfReceived(uint32_t code, uint8_t bits, uint8_t protocol,
                  uint16_t pulseMicros, uint8_t learnedId);
  void relay(uint8_t activeMask);
  void macro(uint8_t state, uint8_t id);
  void reset(uint8_t cause, uint32_t count);
  // Emits an immediate typed transition; measurements remain in STATUS.
  void alert(ControllerAlertKind kind, bool active);

private:
  // Prepends the event type and sends it as an unsolicited native Event frame.
  bool send(const uint8_t *payload, uint8_t length);

  ControllerProtocol::UartProtocol &protocol_;
};
