#pragma once

#include <stdint.h>

// Fixed-storage macro scheduler shared by AVR-facing adapters. It owns only
// queue/timing state; its caller still sends every released command
// through the ordinary dispatcher and therefore keeps peripheral safety local.
namespace ControllerCore {

class MacroRing {
public:
  static constexpr uint8_t QueueSize = 128;
  static constexpr uint16_t WorkspaceBytes = 300;
  static constexpr uint8_t Capacity = QueueSize - 1;
  static constexpr uint8_t QueueMask = QueueSize - 1;
  static constexpr uint8_t RecordHeaderBytes = 6;
  static constexpr uint8_t KeepOutputsOnCancel = 1U << 0;

  static constexpr uint8_t SnapshotBytes = 5;
  // State is the compact lifecycle value sent to the host.
  enum StateCode : uint8_t {
    Idle = 0,
    Buffering = 1,
    Playing = 2,
    Cancelled = 3,
    Completed = 4,
    Failed = 5,
    Recording = 6,
    Recorded = 7,
    ReplayingRecording = 8,
  };

#pragma pack(push, 1)
  struct Report {
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

  // Event envelope is already UART-ready; no allocator or serializer needed.
  struct StatusEvent {
    uint8_t type;
    Report report;
  };
#pragma pack(pop)

  static_assert(sizeof(Report) == 17, "macro report must remain byte stable");
  static_assert(sizeof(StatusEvent) == 18, "macro event must remain byte stable");

  struct Command {
    uint8_t opcode;
    uint8_t payloadLength;
  };

  enum DequeueResult : uint8_t {
    NotDue = 0,
    Ready = 1,
    Malformed = 2,
  };

  // The ring is deliberately trivial at static-storage duration. The adapter
  // supplies its event tag once during setup, avoiding a second AVR global
  // constructor while keeping the same zeroed idle state.
  void initialize(uint8_t eventType);

  void begin(uint8_t id, uint8_t options, uint16_t totalSteps);
  bool append(uint16_t streamOffset, uint16_t completeStepIndex,
              const uint8_t *bytes, uint8_t byteCount, uint32_t nowUs);
  bool canStart() const;
  bool start(uint32_t nowUs);
  DequeueResult dequeueDue(uint32_t nowUs, Command &command,
                           uint8_t *payload, uint8_t payloadCapacity);
  bool completeStep(bool succeeded);
  bool cancel(bool keepOutputs);
  bool defaultKeepOutputsOnCancel() const;
  bool takeSafeStopRequest();
  bool active() const;
  void beginRecording(uint8_t id, uint8_t mask, uint32_t nowUs);
  bool recordRelay(uint8_t mask, uint32_t nowUs);
  bool stopRecording();
  bool startRecorded(uint32_t nowUs, uint8_t relayOpcode);
  // Raw capture-clock offsets; the host subtracts the first retained stamp
  // after download, exactly as local replay does at scheduling time.
  uint8_t readRecording(uint16_t offset, uint8_t *bytes, uint8_t capacity) const;
  bool hasRecording() const;
  bool clearRecording();
  uint8_t *claimSharedWorkspace();

  // Refreshes fill at serialization time, preserving the original report
  // semantics without mutating a second adapter-owned copy.
  const StatusEvent &status();
  const StatusEvent &status() const;

private:
  uint8_t peek(uint8_t offset) const;
  uint32_t peekU32(uint8_t offset) const;
  bool recordReady() const;
  void fail();

  // This order is intentionally the previous AVR queue layout minus only the
  // UART reference. MacroQueue adds that reference before this object, keeping
  // its static SRAM footprint bounded on ATmega328P.
  StatusEvent status_;
  // Macro and strip streaming are mutually exclusive. The strip borrows this
  // workspace only after the user explicitly clears any retained recording.
  uint8_t queue_[WorkspaceBytes];
  uint8_t head_;
  uint8_t used_;
  uint8_t options_;
  bool safeStopRequested_;
  // Recording shares queue_ with streamed playback; only one cursor byte
  // are added, never a second allocation or recording-sized RAM buffer.
  uint8_t replayOffset_;
};

#if defined(__AVR__)
static_assert(sizeof(MacroRing) == 323,
              "portable macro ring must preserve the AVR queue footprint");
#endif

} // namespace ControllerCore
