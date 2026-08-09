#pragma once

#include <Arduino.h>

#include "../ProjectConfig.h"
#include "MacroAction.h"
#include "UartProtocol.h"

#if defined(_MSC_VER)
#define PCCONTROLLER_PACK_PUSH __pragma(pack(push, 1))
#define PCCONTROLLER_PACK_POP __pragma(pack(pop))
#define PCCONTROLLER_PACKED
#else
#define PCCONTROLLER_PACK_PUSH
#define PCCONTROLLER_PACK_POP
#define PCCONTROLLER_PACKED __attribute__((packed))
#endif

// Buffers normal protocol commands against transport jitter and also retains a
// bounded board-origin capture.  Both paths use exactly the same circular
// action record so timing and peripheral validation cannot drift.
class MacroQueue {
public:
  static constexpr uint8_t Schema = 3;
  static constexpr uint8_t ExecutionSequence = 0xFE;
  static constexpr uint8_t KeepOutputsOnCancel = 1U << 0;
  static constexpr uint8_t CaptureExportAcknowledged = 1U << 7;
  static constexpr uint8_t QueueSize = 128;
  static constexpr uint8_t QueueCapacity = QueueSize - 1U;

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

  bool handle(const ControllerProtocol::Frame &frame);
  bool dequeueDue(ControllerProtocol::Frame &frame);
  void completeStep(bool succeeded);
  void cancel(bool keepOutputs = false);
  bool takeSafeStopRequest();
  bool active() const;
  bool hostDependent() const;

#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  bool beginCapture(uint8_t id, uint32_t atUs = micros());
  bool captureAction(uint8_t opcode, const uint8_t *payload,
                     uint8_t availablePayload, uint32_t atUs = micros());
  bool finishCapture();
  bool playCapture(uint32_t atUs = micros());
  bool recording() const;
  bool captured() const;
  uint16_t retainedSteps() const;
#endif

private:
  static constexpr uint8_t QueueMask = QueueSize - 1U;

  PCCONTROLLER_PACK_PUSH
  struct PCCONTROLLER_PACKED Report {
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
    uint16_t droppedSteps;
  };
  struct PCCONTROLLER_PACKED EventReport {
    uint8_t type;
    Report report;
  } wire_{};
  PCCONTROLLER_PACK_POP

  static_assert(sizeof(Report) == 20,
                "macro report wire shape changed unexpectedly");
  static_assert(sizeof(EventReport) == 21,
                "macro event wire shape changed unexpectedly");

  uint8_t peek(uint8_t offset) const;
  uint32_t peekU32(uint8_t offset) const;
  bool recordReady() const;
  bool bufferedRecordsValid(uint16_t completeSteps) const;
  void appendByte(uint8_t value);
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  void sendCaptureChunk(uint8_t sequence, uint16_t offset);
  void preserveCaptureSnapshot();
  void restoreCaptureSnapshot();
#endif
  void sendStatus(uint8_t opcode, uint8_t sequence);
  void fail();

  ControllerProtocol::UartProtocol &protocol_;
  uint8_t queue_[QueueSize];
  uint32_t startedAtUs_ = 0;
  uint8_t head_ = 0;
  uint8_t used_ = 0;
  uint8_t options_ = 0;
  bool safeStopRequested_ = false;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  uint16_t retainedSteps_ = 0;
  uint8_t capturedHead_ = 0;
  uint8_t capturedUsed_ = 0;
  uint16_t capturedSteps_ = 0;
  bool capturePlayback_ = false;
  bool capturedData_ = false;
#endif
};

static_assert((MacroQueue::QueueSize & (MacroQueue::QueueSize - 1U)) == 0,
              "macro byte ring size must remain a power of two");
static_assert(MacroQueue::QueueSize >=
                  2U * (MacroAction::RecordHeaderBytes +
                        MacroAction::MaximumPayload),
              "macro byte ring must retain at least two maximum actions");
static_assert(MacroQueue::QueueCapacity < 0x80U,
              "macro used/head arithmetic requires a seven-bit capacity");

#undef PCCONTROLLER_PACKED
#undef PCCONTROLLER_PACK_POP
#undef PCCONTROLLER_PACK_PUSH
