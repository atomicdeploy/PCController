#include "MacroRing.h"

#include <stddef.h>
#include <string.h>

namespace ControllerCore {

void MacroRing::initialize(uint8_t eventType) {
  // Static storage is already zero-filled; only the wire invariants require
  // explicit initialization. Keeping this small matters on the AVR's global
  // constructor path and does not change a freshly constructed ring's state.
  status_.type = eventType;
  status_.report.schema = Schema;
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
}

bool MacroRing::append(uint16_t streamOffset, uint16_t completeStepIndex,
                       const uint8_t *bytes, uint8_t byteCount,
                       uint32_t nowUs) {
  Report &report = status_.report;
  if (!active() || (byteCount != 0 && bytes == nullptr) ||
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
  while (report.state == Playing && report.executedSteps < report.totalSteps) {
    if (!recordReady()) {
      return NotDue;
    }
    const uint8_t payloadLength = peek(5);
    if (payloadLength > payloadCapacity ||
        (payloadLength != 0 && payload == nullptr)) {
      ++report.dispatchErrors;
      fail();
      return Malformed;
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
  status_.report.state = Cancelled;
  head_ = 0;
  used_ = 0;
  safeStopRequested_ = !keepOutputs;
  return true;
}

bool MacroRing::defaultKeepOutputsOnCancel() const {
  return (options_ & KeepOutputsOnCancel) != 0;
}

bool MacroRing::takeSafeStopRequest() {
  const bool requested = safeStopRequested_;
  safeStopRequested_ = false;
  return requested;
}

bool MacroRing::active() const {
  return status_.report.state == Buffering || status_.report.state == Playing;
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
