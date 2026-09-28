#include "MacroRing.h"

#include <stddef.h>
#include <string.h>

namespace ControllerCore {

void MacroRing::initialize(uint8_t eventType) {
  // Static storage is already zero-filled; only the wire invariants require
  // explicit initialization. Keeping this small matters on the AVR's global
  // constructor path and does not change a freshly constructed ring's state.
  status_.type = eventType;
}

uint8_t MacroRing::peek(uint8_t offset) const {
  return queue_[static_cast<uint8_t>(head_ + offset) & QueueMask];
}

uint32_t MacroRing::peekU32(uint8_t offset) const {
  return static_cast<uint32_t>(peek(offset)) |
         static_cast<uint32_t>(peek(static_cast<uint8_t>(offset + 1))) << 8 |
         static_cast<uint32_t>(peek(static_cast<uint8_t>(offset + 2))) << 16 |
         static_cast<uint32_t>(peek(static_cast<uint8_t>(offset + 3))) << 24;
}

bool MacroRing::recordReady() const {
  return used_ >= RecordHeaderBytes &&
         peek(5) <= static_cast<uint8_t>(used_ - RecordHeaderBytes);
}

void MacroRing::begin(uint8_t id, uint8_t options, uint16_t totalSteps) {
  Report &report = status_.report;
  memset(&report.acceptedSteps, 0,
         sizeof(report) - offsetof(Report, acceptedSteps));
  report.state = Buffering;
  report.id = id;
  report.totalSteps = totalSteps;
  options_ = options;
  head_ = 0;
  used_ = 0;
  safeStopRequested_ = false;
  replayOffset_ = 0;
}

bool MacroRing::append(uint16_t streamOffset, uint16_t completeStepIndex,
                       const uint8_t *bytes, uint8_t byteCount,
                       uint32_t nowUs) {
  Report &report = status_.report;
  if ((report.state != Buffering && report.state != Playing) ||
      (byteCount != 0 && bytes == nullptr) ||
      streamOffset != report.acceptedBytes ||
      completeStepIndex < report.acceptedSteps ||
      completeStepIndex > report.totalSteps ||
      byteCount > static_cast<uint8_t>(Capacity - used_)) {
    return false;
  }
  const bool wasStarved = !recordReady();
  for (uint8_t index = 0; index < byteCount; ++index) {
    queue_[static_cast<uint8_t>(head_ + used_) & QueueMask] = bytes[index];
    ++used_;
  }
  report.acceptedBytes =
      static_cast<uint16_t>(report.acceptedBytes + byteCount);
  report.acceptedSteps = completeStepIndex;
  if (wasStarved && report.state == Playing && recordReady() &&
      static_cast<int32_t>(nowUs - (report.startedAtUs + peekU32(0))) >= 0) {
    ++report.underruns;
  }
  return true;
}

bool MacroRing::canStart() const {
  const Report &report = status_.report;
  return report.state == Buffering &&
         (report.totalSteps == 0 ||
          (recordReady() &&
           (report.acceptedSteps >= report.totalSteps || used_ >= 64)));
}

bool MacroRing::start(uint32_t nowUs) {
  if (!canStart()) {
    return false;
  }
  Report &report = status_.report;
  report.startedAtUs = nowUs;
  report.state = Playing;
  return true;
}

MacroRing::DequeueResult MacroRing::dequeueDue(uint32_t nowUs,
                                                Command &command,
                                                uint8_t *payload,
                                                uint8_t payloadCapacity) {
  Report &report = status_.report;
  if (report.state == ReplayingRecording) {
    if (payloadCapacity < 1 || payload == nullptr) {
      fail();
      return Malformed;
    }
    const uint32_t due = peekU32(replayOffset_) - peekU32(0);
    if (static_cast<int32_t>(nowUs - report.startedAtUs - due) < 0) {
      return NotDue;
    }
    command.opcode = options_;
    command.payloadLength = 1;
    payload[0] = peek(static_cast<uint8_t>(replayOffset_ + 4));
    replayOffset_ = static_cast<uint8_t>(replayOffset_ + SnapshotBytes);
    ++report.executedSteps;
    return Ready;
  }
  while (report.state == Playing && report.executedSteps < report.totalSteps) {
    if (used_ < RecordHeaderBytes) {
      return NotDue;
    }
    const uint8_t payloadLength = peek(5);
    if (payloadLength > payloadCapacity ||
        (payloadLength != 0 && payload == nullptr)) {
      ++report.dispatchErrors;
      fail();
      return Malformed;
    }
    if (!recordReady()) {
      return NotDue;
    }
    if (static_cast<int32_t>(nowUs - (report.startedAtUs + peekU32(0))) < 0) {
      return NotDue;
    }
    command.opcode = peek(4);
    command.payloadLength = payloadLength;
    for (uint8_t index = 0; index < payloadLength; ++index) {
      payload[index] = peek(static_cast<uint8_t>(RecordHeaderBytes + index));
    }
    const uint8_t recordLength =
        static_cast<uint8_t>(RecordHeaderBytes + payloadLength);
    head_ = static_cast<uint8_t>(head_ + recordLength) & QueueMask;
    used_ = static_cast<uint8_t>(used_ - recordLength);
    ++report.executedSteps;
    return Ready;
  }
  return NotDue;
}

bool MacroRing::completeStep(bool succeeded) {
  Report &report = status_.report;
  if (!succeeded) {
    ++report.dispatchErrors;
  }
  if (report.executedSteps != report.totalSteps) {
    return false;
  }
  if (report.state == ReplayingRecording) {
    report.state = Recorded;
    return true;
  }
  report.state = used_ == 0 ? Completed : Failed;
  head_ = 0;
  used_ = 0;
  safeStopRequested_ = report.state == Failed;
  return true;
}

bool MacroRing::cancel(bool keepOutputs) {
  if (!active()) {
    return false;
  }
  if (status_.report.state == Recording ||
      status_.report.state == ReplayingRecording) {
    status_.report.state = Recorded;
    safeStopRequested_ = !keepOutputs;
    return true;
  }
  status_.report.state = Cancelled;
  head_ = 0;
  used_ = 0;
  safeStopRequested_ = !keepOutputs;
  return true;
}

bool MacroRing::defaultKeepOutputsOnCancel() const {
  if (status_.report.state == Recording || status_.report.state == ReplayingRecording) {
    return false;
  }
  return (options_ & KeepOutputsOnCancel) != 0;
}

bool MacroRing::takeSafeStopRequest() {
  const bool requested = safeStopRequested_;
  safeStopRequested_ = false;
  return requested;
}

bool MacroRing::active() const {
  return status_.report.state == Buffering || status_.report.state == Playing ||
         status_.report.state == Recording || status_.report.state == ReplayingRecording;
}

void MacroRing::beginRecording(uint8_t id, uint8_t mask, uint32_t nowUs) {
  begin(id, 0, 0);
  status_.report.state = Recording;
  status_.report.startedAtUs = nowUs;
  recordRelay(mask, nowUs);
}

bool MacroRing::recordRelay(uint8_t mask, uint32_t nowUs) {
  Report &report = status_.report;
  if (report.state != Recording ||
      (used_ != 0 && peek(static_cast<uint8_t>(used_ - 1)) == mask)) {
    return false;
  }
  const uint32_t elapsed = nowUs - report.startedAtUs;
  // Signed deadline comparison permits sessions shorter than 2^31 us.
  // Stop explicitly at the limit instead of silently misordering timestamps.
  if (elapsed > 0x7FFFFFFFUL) {
    report.state = Recorded;
    return false;
  }
  if (used_ + SnapshotBytes > Capacity) {
    head_ = static_cast<uint8_t>(head_ + SnapshotBytes) & QueueMask;
    used_ = static_cast<uint8_t>(used_ - SnapshotBytes);
    if (report.underruns != 255) ++report.underruns;
  }
  uint32_t remaining = elapsed;
  for (uint8_t index = 0; index < SnapshotBytes; ++index) {
    queue_[static_cast<uint8_t>(head_ + used_) & QueueMask] =
        index == 4 ? mask : static_cast<uint8_t>(remaining);
    remaining >>= 8;
    ++used_;
  }
  report.totalSteps = used_ / SnapshotBytes;
  report.acceptedSteps = report.totalSteps;
  report.acceptedBytes = used_;
  return true;
}

bool MacroRing::stopRecording() {
  if (hasRecording()) return true;
  if (status_.report.state != Recording) return false;
  status_.report.state = Recorded;
  return true;
}

bool MacroRing::hasRecording() const {
  return status_.report.state == Recorded && used_ != 0;
}

bool MacroRing::clearRecording() {
  if (active()) return false;
  begin(0, 0, 0);
  status_.report.state = Idle;
  return true;
}

uint8_t *MacroRing::claimSharedWorkspace() {
  if (active() || hasRecording()) return nullptr;
  // The replay cursor is unused outside macro ownership. Its sentinel clears
  // macro bytes once when the strip takes over, preserving later chunk writes.
  if (replayOffset_ != 0xFF) {
    memset(queue_, 0, sizeof(queue_));
    replayOffset_ = 0xFF;
  }
  return queue_;
}

bool MacroRing::startRecorded(uint32_t nowUs, uint8_t relayOpcode) {
  if (!hasRecording()) return false;
  Report &report = status_.report;
  report.state = ReplayingRecording;
  report.startedAtUs = nowUs;
  report.executedSteps = 0;
  report.dispatchErrors = 0;
  replayOffset_ = 0;
  options_ = relayOpcode;
  return true;
}

uint8_t MacroRing::readRecording(uint16_t offset, uint8_t *bytes,
                                uint8_t capacity) const {
  if (!hasRecording() || offset > used_ ||
      static_cast<uint8_t>(offset) % SnapshotBytes != 0) return 0;
  uint8_t count = static_cast<uint8_t>(used_ - offset);
  if (count > capacity) count = capacity - capacity % SnapshotBytes;
  for (uint8_t index = 0; index < count; ++index) {
    bytes[index] = peek(static_cast<uint8_t>(offset + index));
  }
  return count;
}

const MacroRing::StatusEvent &MacroRing::status() {
  status_.report.fill = used_;
  return status_;
}

const MacroRing::StatusEvent &MacroRing::status() const { return status_; }

void MacroRing::fail() {
  status_.report.state = Failed;
  head_ = 0;
  used_ = 0;
  safeStopRequested_ = true;
}

} // namespace ControllerCore
