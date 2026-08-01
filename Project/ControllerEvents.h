#pragma once

#include <Arduino.h>

#include "UartProtocol.h"

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
};

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

  void key(uint8_t bit, uint8_t gesture,
           InputEventSource source = InputEventSource::Physical,
           uint8_t sourceId = 0xFF);
  void door(bool open);
  void bluetooth(uint8_t state);
  void pwmChannel(uint8_t channel);
  void rfLearned(uint8_t id);
  // state: 0=ended, 1=canceled, 2=full, 3=started.
  void rfLearning(uint8_t state, uint8_t count);
  void rfReceived(uint32_t code, uint8_t bits, uint8_t protocol,
                  uint16_t pulseMicros, uint8_t learnedId);
  void relay(uint8_t activeMask);
  void macro(uint8_t state, uint8_t id);
  void reset(uint8_t cause, uint32_t count);

private:
  void send(const uint8_t *payload, uint8_t length);

  ControllerProtocol::UartProtocol &protocol_;
};
