#pragma once

#include <Arduino.h>

#include "UartProtocol.h"

// Buffers ordinary protocol commands against USB/network jitter and releases
// them from the AVR microsecond clock without duplicating peripheral guards.
class MacroQueue {
public:
  static constexpr uint8_t Schema = 2;
  static constexpr uint8_t ExecutionSequence = 0xFE;
  static constexpr uint8_t KeepOutputsOnCancel = 1U << 0;

  enum State : uint8_t {
    Idle = 0,
    Buffering = 1,
    Playing = 2,
    Cancelled = 3,
    Completed = 4,
    Failed = 5,
  };

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
  static constexpr uint8_t QueueSize = 128;
  static constexpr uint8_t QueueMask = QueueSize - 1;

  struct __attribute__((packed)) Report {
    uint8_t schema;
    uint8_t state;
    uint8_t id;
    uint16_t acceptedSteps;
    uint16_t executedSteps;
    uint16_t acceptedBytes;
    uint8_t fill;
    uint8_t underruns;
    uint8_t dispatchErrors;
    uint32_t startedAtUs;
    uint16_t totalSteps;
  };

  struct __attribute__((packed)) EventReport {
    uint8_t type;
    Report report;
  } wire_;

  uint8_t peek(uint8_t offset) const;
  uint32_t peekU32(uint8_t offset) const;
  bool recordReady() const;
  void sendStatus(uint8_t opcode, uint8_t sequence);
  void fail();

  ControllerProtocol::UartProtocol &protocol_;
  uint8_t queue_[QueueSize]; // Circular variable-record byte storage.
  uint32_t startedAtUs_ = 0;
  uint8_t head_ = 0;
  uint8_t used_ = 0;
  uint8_t options_ = 0;
  bool safeStopRequested_ = false;
};
