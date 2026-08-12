#include "MacroQueue.h"

#include <stddef.h>
#include <string.h>

#include "ControllerEvents.h"

namespace {
uint16_t read16(const uint8_t *value) {
  return static_cast<uint16_t>(value[0]) |
         static_cast<uint16_t>(value[1]) << 8;
}

} // namespace

MacroQueue::MacroQueue(ControllerProtocol::UartProtocol &protocol)
    : protocol_(protocol) {
  wire_.type = static_cast<uint8_t>(ControllerEventType::Macro);
  wire_.report.schema = Schema;
}

uint8_t MacroQueue::peek(uint8_t offset) const {
  return queue_[static_cast<uint8_t>(head_ + offset) & QueueMask];
}

uint32_t MacroQueue::peekU32(uint8_t offset) const {
  return static_cast<uint32_t>(peek(offset)) |
         static_cast<uint32_t>(peek(offset + 1)) << 8 |
         static_cast<uint32_t>(peek(offset + 2)) << 16 |
         static_cast<uint32_t>(peek(offset + 3)) << 24;
}

bool MacroQueue::recordReady() const {
  return used_ >= 6 && used_ >= static_cast<uint8_t>(6 + peek(5));
}

bool MacroQueue::appendRecord(uint32_t dueUs, uint8_t opcode,
                              const uint8_t *payload, uint8_t length) {
  const uint8_t recordLength = static_cast<uint8_t>(6 + length);
  if (length > ControllerProtocol::MaximumPayload ||
      recordLength > static_cast<uint8_t>(127 - used_)) {
    return false;
  }
  const uint8_t record[] = {
      static_cast<uint8_t>(dueUs), static_cast<uint8_t>(dueUs >> 8),
      static_cast<uint8_t>(dueUs >> 16), static_cast<uint8_t>(dueUs >> 24),
      opcode, length};
  for (uint8_t index = 0; index < sizeof(record); ++index) {
    queue_[static_cast<uint8_t>(head_ + used_) & QueueMask] = record[index];
    ++used_;
  }
  for (uint8_t index = 0; index < length; ++index) {
    queue_[static_cast<uint8_t>(head_ + used_) & QueueMask] = payload[index];
    ++used_;
  }
  return true;
}

bool MacroQueue::captureable(uint8_t opcode) const {
  // Capture only side-effecting ordinary opcodes.  Keeping configuration,
  // discovery, RF learning and macro control out of a replay prevents a
  // recorded session from changing the board's future configuration.
  switch (opcode) {
    case ControllerProtocol::Buzzer:
    case ControllerProtocol::PwmSet:
    case ControllerProtocol::PwmAllOff:
    case ControllerProtocol::StatusRgb:
    case ControllerProtocol::StatusEffect:
    case ControllerProtocol::AddressableLed:
    case ControllerProtocol::MenuAction:
    case ControllerProtocol::RemoteKeyGesture:
    case ControllerProtocol::DisplayText:
    case ControllerProtocol::RelaySet:
    case ControllerProtocol::RelaySide:
    case ControllerProtocol::RelayAllOff:
      return true;
    default:
      return false;
  }
}

void MacroQueue::sendStatus(uint8_t opcode, uint8_t sequence) {
  Report &report = wire_.report;
  report.fill = used_;
  // Events and query replies deliberately share one self-describing envelope.
  protocol_.send(opcode, sequence,
                 reinterpret_cast<const uint8_t *>(&wire_), sizeof(wire_));
}

void MacroQueue::fail() {
  wire_.report.state = Failed;
  head_ = used_ = 0;
  safeStopRequested_ = true;
  sendStatus(ControllerProtocol::Event, 0);
}

bool MacroQueue::capture(uint8_t opcode, const uint8_t *payload,
                         uint8_t length) {
  Report &report = wire_.report;
  if (report.state != Recording || !captureable(opcode)) {
    return false;
  }
  const uint32_t dueUs = micros() - startedAtUs_;
  if (!appendRecord(dueUs, opcode, payload, length)) {
    // Capture exhaustion is reported, but must never change live outputs.
    report.state = Failed;
    ++report.dispatchErrors;
    sendStatus(ControllerProtocol::Event, 0);
    return false;
  }
  ++report.acceptedSteps;
  report.totalSteps = report.acceptedSteps;
  report.acceptedBytes = static_cast<uint16_t>(report.acceptedBytes + 6 + length);
  return true;
}

bool MacroQueue::handle(const ControllerProtocol::Frame &frame) {
  const uint8_t *payload = frame.payload;
  const uint8_t length = frame.payloadLength;
  Report &report = wire_.report;
  if (frame.opcode == ControllerProtocol::MacroStart) {
    // BEGIN [schema, id, flags, total steps LE16]. Per-step timed
    // 0xFE ACK/error frames let the host calculate exact error without
    // duplicating tolerance/statistics code in AVR flash.
    // Duration remains host-owned because the hosted front-panel macro page
    // already has the complete library and this saves scarce AVR flash.
    if (length < 5 || payload[0] != Schema) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    if (active()) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::Busy);
      return true;
    }
    memset(&report.acceptedSteps, 0,
           sizeof(report) - offsetof(Report, acceptedSteps));
    report.state = (payload[2] & CaptureInputs) != 0 ? Recording : Buffering;
    report.id = payload[1];
    report.totalSteps = (payload[2] & CaptureInputs) != 0 ? 0 : read16(payload + 3);
    options_ = payload[2];
    head_ = used_ = 0;
    startedAtUs_ = micros();
    report.startedAtUs = startedAtUs_;
    safeStopRequested_ = false;
    protocol_.sendAck(frame.sequence, frame.opcode);
    sendStatus(ControllerProtocol::Event, 0);
    return true;
  }
  if (frame.opcode == ControllerProtocol::MacroCancel) {
    if (length != 0 && payload[0] > 1) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    cancel(length != 0 ? payload[0] != 0
                       : (options_ & KeepOutputsOnCancel) != 0);
    protocol_.sendAck(frame.sequence, frame.opcode);
    return true;
  }
  if (frame.opcode != ControllerProtocol::MacroStep) {
    return false;
  }
  if (length != 0 && payload[0] == 2) {
    sendStatus(ControllerProtocol::MacroStatusResponse, frame.sequence);
    return true;
  }
  if (length != 0 && payload[0] == 3) {
    if (length != 1 || active() || report.state == Recording) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::Busy);
      return true;
    }
    memset(&report.acceptedSteps, 0,
           sizeof(report) - offsetof(Report, acceptedSteps));
    report.state = Idle;
    head_ = used_ = 0;
    protocol_.sendAck(frame.sequence, frame.opcode);
    sendStatus(ControllerProtocol::Event, 0);
    return true;
  }
  if (length >= 4 && payload[0] == 4) {
    sendFetch(frame.sequence, read16(payload + 1), payload[3]);
    return true;
  }
  if (length != 0 && payload[0] == 5) {
    if (length != 1 || report.state != Recording) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    report.state = Captured;
    protocol_.sendAck(frame.sequence, frame.opcode);
    sendStatus(ControllerProtocol::Event, 0);
    return true;
  }
  if (length != 0 && payload[0] == 1) {
    if ((report.state != Buffering && report.state != Captured) ||
        (report.totalSteps != 0 &&
         (!recordReady() ||
          (report.acceptedSteps < report.totalSteps && used_ < 64)))) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    startedAtUs_ = micros();
    report.startedAtUs = startedAtUs_;
    // Host validation excludes empty macros, so RUN has one compact path.
    report.state = Playing;
    protocol_.sendAck(frame.sequence, frame.opcode);
    sendStatus(ControllerProtocol::Event, 0);
    return true;
  }

  // APPEND [0][stream offset LE16][complete step index LE16][bytes...].
  if (length < 6 || payload[0] != 0 || report.state != Buffering ||
      read16(payload + 1) != report.acceptedBytes ||
      read16(payload + 3) < report.acceptedSteps ||
      read16(payload + 3) > report.totalSteps ||
      static_cast<uint8_t>(length - 5) > static_cast<uint8_t>(127 - used_)) {
    protocol_.sendError(frame.sequence, frame.opcode,
                        ControllerProtocol::BadPayload);
    return true;
  }
  const bool wasStarved = !recordReady();
  for (uint8_t index = 5; index < length; ++index) {
    queue_[static_cast<uint8_t>(head_ + used_) & QueueMask] = payload[index];
    ++used_;
  }
  report.acceptedBytes =
      static_cast<uint16_t>(report.acceptedBytes + length - 5);
  report.acceptedSteps = read16(payload + 3);
  if (wasStarved && report.state == Playing && recordReady() &&
      static_cast<int32_t>(micros() - (startedAtUs_ + peekU32(0))) >= 0) {
    ++report.underruns;
  }
  protocol_.sendAck(frame.sequence, frame.opcode);
  return true;
}

void MacroQueue::sendFetch(uint8_t sequence, uint16_t offset,
                           uint8_t requested) {
  // Response: [event-type, schema, Exported, id, offset LE16, count, bytes].
  // It is intentionally a small page because the UART frame is bounded.
  uint8_t response[ControllerProtocol::MaximumPayload];
  const uint8_t header = 7;
  if (offset > used_) {
    protocol_.sendError(sequence, ControllerProtocol::MacroStep,
                        ControllerProtocol::BadPayload);
    return;
  }
  uint8_t count = static_cast<uint8_t>(used_ - offset);
  const uint8_t limit = static_cast<uint8_t>(ControllerProtocol::MaximumPayload - header);
  if (count > limit) count = limit;
  if (requested != 0 && count > requested) count = requested;
  response[0] = static_cast<uint8_t>(ControllerEventType::Macro);
  response[1] = Schema;
  response[2] = Exported;
  response[3] = wire_.report.id;
  response[4] = static_cast<uint8_t>(offset);
  response[5] = static_cast<uint8_t>(offset >> 8);
  response[6] = count;
  for (uint8_t index = 0; index < count; ++index) {
    response[header + index] = peek(static_cast<uint8_t>(offset + index));
  }
  protocol_.send(ControllerProtocol::MacroStatusResponse, sequence, response,
                 static_cast<uint8_t>(header + count));
}

bool MacroQueue::dequeueDue(ControllerProtocol::Frame &frame) {
  Report &report = wire_.report;
  while (report.state == Playing && report.executedSteps < report.totalSteps) {
    if (!recordReady()) {
      return false;
    }
    const uint8_t payloadLength = peek(5);
    if (payloadLength > ControllerProtocol::MaximumPayload) {
      ++report.dispatchErrors;
      fail();
      return false;
    }
    const uint32_t actual = micros();
    const int32_t error =
        static_cast<int32_t>(actual - (startedAtUs_ + peekU32(0)));
    if (error < 0) {
      return false;
    }
    frame.opcode = peek(4);
    frame.sequence = ExecutionSequence;
    frame.payloadLength = payloadLength;
    uint8_t *payload = protocol_.framePayloadScratch();
    frame.payload = payload;
    for (uint8_t index = 0; index < payloadLength; ++index) {
      payload[index] = peek(static_cast<uint8_t>(6 + index));
    }
    const uint8_t recordLength = static_cast<uint8_t>(6 + payloadLength);
    head_ = static_cast<uint8_t>(head_ + recordLength) & QueueMask;
    used_ = static_cast<uint8_t>(used_ - recordLength);
    ++report.executedSteps;
    if (frame.opcode >= ControllerProtocol::MacroStart &&
        frame.opcode <= ControllerProtocol::MacroStep) {
      completeStep(false);
      continue;
    }
    return true;
  }
  return false;
}

void MacroQueue::completeStep(bool succeeded) {
  Report &report = wire_.report;
  if (!succeeded) {
    ++report.dispatchErrors;
  }
  if (report.executedSteps == report.totalSteps) {
    report.state = used_ == 0 ? Completed : Failed;
    head_ = used_ = 0;
    safeStopRequested_ = report.state == Failed;
    sendStatus(ControllerProtocol::Event, 0);
  }
}

void MacroQueue::cancel(bool keepOutputs) {
  if (!active()) {
    return;
  }
  const bool wasRecording = wire_.report.state == Recording;
  wire_.report.state = Cancelled;
  head_ = used_ = 0;
  safeStopRequested_ = !wasRecording && !keepOutputs;
  sendStatus(ControllerProtocol::Event, 0);
}

bool MacroQueue::takeSafeStopRequest() {
  const bool requested = safeStopRequested_;
  safeStopRequested_ = false;
  return requested;
}

bool MacroQueue::active() const {
  return wire_.report.state == Buffering || wire_.report.state == Playing ||
         wire_.report.state == Recording;
}
