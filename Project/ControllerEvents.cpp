#include "ControllerEvents.h"

#include <string.h>

ControllerEvents::ControllerEvents(
    ControllerProtocol::UartProtocol &protocol)
    : protocol_(protocol) {}

bool ControllerEvents::send(const uint8_t *payload, uint8_t length) {
  return protocol_.send(ControllerProtocol::Event, 0, payload, length);
}

void ControllerEvents::key(uint8_t bit, uint8_t gesture,
                           InputEventSource source, uint8_t sourceId) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::Key),
      bit,
      gesture,
      static_cast<uint8_t>(source),
      sourceId,
  };
  send(payload, sizeof(payload));
}

bool ControllerEvents::action(InputEventSource source, uint8_t opcode,
                              const uint8_t *payload,
                              uint8_t availablePayload,
                              uint32_t capturedAtUs) {
  if (!MacroAction::recordable(opcode, availablePayload)) {
    return false;
  }
  const uint8_t length = MacroAction::payloadLength(opcode);
  uint8_t evidence[4 + MacroAction::MaximumPayload] = {
      static_cast<uint8_t>(ControllerEventType::Action),
      static_cast<uint8_t>(source), opcode, length};
  if (length != 0) {
    memcpy(evidence + 4, payload, length);
  }
  return protocol_.sendEventAt(evidence, static_cast<uint8_t>(4 + length),
                               capturedAtUs);
}

void ControllerEvents::door(bool open) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::Door),
      static_cast<uint8_t>(open),
  };
  send(payload, sizeof(payload));
}

void ControllerEvents::bluetooth(uint8_t state) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::Bluetooth),
      state,
  };
  send(payload, sizeof(payload));
}

void ControllerEvents::pwmChannel(uint8_t channel) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::PwmChannel),
      channel,
  };
  send(payload, sizeof(payload));
}

void ControllerEvents::rfLearned(uint8_t id) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::RfLearned),
      id,
  };
  send(payload, sizeof(payload));
}

void ControllerEvents::rfLearning(uint8_t state, uint8_t count, uint8_t mode,
                                  uint8_t totalSeconds,
                                  uint8_t remainingSeconds) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::RfLearning),
      state,
      count,
      mode,
      totalSeconds,
      remainingSeconds,
  };
  send(payload, sizeof(payload));
}

void ControllerEvents::rfReceived(uint32_t code, uint8_t bits,
                                  uint8_t protocol, uint16_t pulseMicros,
                                  uint8_t learnedId) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::RfReceived),
      static_cast<uint8_t>(code),
      static_cast<uint8_t>(code >> 8),
      static_cast<uint8_t>(code >> 16),
      static_cast<uint8_t>(code >> 24),
      bits,
      protocol,
      static_cast<uint8_t>(pulseMicros),
      static_cast<uint8_t>(pulseMicros >> 8),
      learnedId,
  };
  send(payload, sizeof(payload));
}

void ControllerEvents::relay(uint8_t activeMask) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::Relay), activeMask};
  send(payload, sizeof(payload));
}

void ControllerEvents::macro(uint8_t state, uint8_t id) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::Macro),
      state,
      id,
  };
  send(payload, sizeof(payload));
}

void ControllerEvents::reset(uint8_t cause, uint32_t count) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::Fault),
      cause,
      static_cast<uint8_t>(count),
      static_cast<uint8_t>(count >> 8),
      static_cast<uint8_t>(count >> 16),
      static_cast<uint8_t>(count >> 24),
  };
  send(payload, sizeof(payload));
}

void ControllerEvents::alert(ControllerAlertKind kind, bool active) {
  const uint8_t payload[] = {
      static_cast<uint8_t>(ControllerEventType::Alert),
      static_cast<uint8_t>(kind),
      static_cast<uint8_t>(active),
  };
  send(payload, sizeof(payload));
}
