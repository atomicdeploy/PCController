#pragma once

#include <Arduino.h>

#include "Core/MacroRing.h"
#include "UartProtocol.h"

// AVR/UART adapter for the portable MacroRing. It keeps macro records on the
// ordinary protocol dispatcher path, so replay cannot bypass peripheral guards.
class MacroQueue {
public:
  static constexpr uint8_t Schema = ControllerCore::MacroRing::Schema;
  static constexpr uint8_t ExecutionSequence = 0xFE;
  static constexpr uint8_t KeepOutputsOnCancel =
      ControllerCore::MacroRing::KeepOutputsOnCancel;

  explicit MacroQueue(ControllerProtocol::UartProtocol &protocol);

  // Accepts Start/Step/Cancel protocol records into the byte-ring buffer.
  bool handle(const ControllerProtocol::Frame &frame);
  // Releases one due opcode using the MCU microsecond clock for precise deltas.
  bool dequeueDue(ControllerProtocol::Frame &frame);
  // Records dispatch fidelity and advances or terminates playback.
  void completeStep(bool succeeded);
  void cancel(bool keepOutputs = false);
  bool takeSafeStopRequest();
  bool active() const;

private:
  void sendStatus(uint8_t opcode, uint8_t sequence);

  ControllerProtocol::UartProtocol &protocol_;
  ControllerCore::MacroRing ring_;
};
