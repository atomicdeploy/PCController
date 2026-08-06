#pragma once

#include <Arduino.h>

#include "UartProtocol.h"

// ControllerEventType identifies one asynchronous native event payload shape.
enum class ControllerEventType : uint8_t {
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
  // Host-routed page navigation: [type, target, ASCII page...]. Target is
  // 0=all, 1=WebUI, 2=TUI. Firmware may emit it without knowing host UI APIs.
  AppNavigation = 12,
};

// ControllerAlertKind classifies board-generated warning notifications.
enum class ControllerAlertKind : uint8_t {
  Fault = 1,
  Hot = 2,
};

// InputEventSource records whether a gesture originated physically, by RF, or by host.
enum class InputEventSource : uint8_t {
  Physical = 0,
  Radio = 1,
  Host = 2,
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
  void send(const uint8_t *payload, uint8_t length);

  ControllerProtocol::UartProtocol &protocol_;
};
