#pragma once

#include <Arduino.h>

#include "UartProtocol.h"

// Buffers ordinary protocol commands against USB/network jitter and releases
// them from the AVR microsecond clock without duplicating peripheral guards.
class MacroQueue {
public:
  // Schema 3 keeps the schema-2 raw record format.  Only the control/status
  // envelope grew, so a host never has to translate ordinary opcodes.
  static constexpr uint8_t Schema = 3;
  static constexpr uint8_t ExecutionSequence = 0xFE;
  static constexpr uint8_t KeepOutputsOnCancel = 1U << 0;
  static constexpr uint8_t CaptureInputs = 1U << 1;

  // State is the compact lifecycle value reported to the host.
  enum State : uint8_t {
    Idle = 0,
    Buffering = 1,
    Playing = 2,
    Completed = 3,
    Cancelled = 4,
    Failed = 5,
    Recording = 6,
    Captured = 7,
    Exported = 8,
  };

  explicit MacroQueue(ControllerProtocol::UartProtocol &protocol);

  // Accepts Start/Step/Cancel protocol records into the byte-ring buffer.
  bool handle(const ControllerProtocol::Frame &frame);
  // Releases one due opcode using the MCU microsecond clock for precise deltas.
  bool dequeueDue(ControllerProtocol::Frame &frame);
  // Records dispatch fidelity and advances or terminates playback.
  void completeStep(bool succeeded);
  // Captures an already-accepted ordinary opcode with an MCU-clock delta.
  // The command dispatcher and local input paths call this only after their
  // existing validation/safety path has accepted the action.
  bool capture(uint8_t opcode, const uint8_t *payload, uint8_t length);
  void cancel(bool keepOutputs = false);
  bool takeSafeStopRequest();
  bool active() const;

private:
  static constexpr uint8_t QueueSize = 128;
  static constexpr uint8_t QueueMask = QueueSize - 1;

  // Report is the synchronous macro status response payload.
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

  // EventReport records MCU timing fidelity for one dispatched step.
  struct __attribute__((packed)) EventReport {
    uint8_t type;
    Report report;
  } wire_;

  uint8_t peek(uint8_t offset) const;
  uint32_t peekU32(uint8_t offset) const;
  bool recordReady() const;
  bool appendRecord(uint32_t dueUs, uint8_t opcode, const uint8_t *payload,
                    uint8_t length);
  bool captureable(uint8_t opcode) const;
  void sendStatus(uint8_t opcode, uint8_t sequence);
  void sendFetch(uint8_t sequence, uint16_t offset, uint8_t requested);
  void fail();

  ControllerProtocol::UartProtocol &protocol_;
  uint8_t queue_[QueueSize]; // Circular variable-record byte storage.
  uint32_t startedAtUs_ = 0;
  uint8_t head_ = 0;
  uint8_t used_ = 0;
  uint8_t options_ = 0;
  bool safeStopRequested_ = false;
};
