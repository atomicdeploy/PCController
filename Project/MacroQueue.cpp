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

bool MacroQueue::bufferedRecordsValid() const {
  uint16_t offset = 0;
  while (static_cast<uint16_t>(used_) - offset >=
         MacroAction::RecordHeaderBytes) {
    const uint8_t opcode = peek(static_cast<uint8_t>(offset + 4U));
    const uint8_t payloadLength = peek(static_cast<uint8_t>(offset + 5U));
    if (!MacroAction::playbackAllowed(opcode) ||
        payloadLength > ControllerProtocol::MaximumPayload) {
      return false;
    }
    const uint16_t recordLength =
        MacroAction::RecordHeaderBytes + payloadLength;
    if (static_cast<uint16_t>(used_) - offset < recordLength) {
      return true; // a valid header with a split payload tail
    }
    offset += recordLength;
  }
  return true;
}

void MacroQueue::appendByte(uint8_t value) {
  queue_[static_cast<uint8_t>(head_ + used_) & QueueMask] = value;
  ++used_;
}

#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
void MacroQueue::preserveCaptureSnapshot() {
  capturedHead_ = head_;
  capturedUsed_ = used_;
  capturedSteps_ = retainedSteps_;
  capturedData_ = retainedSteps_ != 0;
}

void MacroQueue::restoreCaptureSnapshot() {
  head_ = capturedHead_;
  used_ = capturedUsed_;
  retainedSteps_ = capturedSteps_;
}

void MacroQueue::sendCaptureChunk(uint8_t sequence, uint16_t offset) {
  // [schema, command=3, id, total bytes LE16, offset LE16, chunk length,
  //  raw ring bytes...]. Forty data bytes keep the whole response bounded by
  // ControllerProtocol::MaximumPayload without allocating a second buffer.
  uint8_t *response = protocol_.framePayloadScratch();
  response[0] = Schema;
  response[1] = 3;
  response[2] = wire_.report.id;
  response[3] = used_;
  response[4] = 0;
  response[5] = static_cast<uint8_t>(offset);
  response[6] = static_cast<uint8_t>(offset >> 8);
  const uint8_t remaining = static_cast<uint8_t>(used_ - offset);
  const uint8_t chunk = remaining > 40 ? 40 : remaining;
  response[7] = chunk;
  for (uint8_t index = 0; index < chunk; ++index) {
    response[8 + index] = peek(static_cast<uint8_t>(offset + index));
  }
  protocol_.send(ControllerProtocol::MacroStatusResponse, sequence, response,
                 static_cast<uint8_t>(8 + chunk));
}
#endif

void MacroQueue::sendStatus(uint8_t opcode, uint8_t sequence) {
  Report &report = wire_.report;
  report.fill = used_;
  // Events and query replies deliberately share one self-describing envelope.
  protocol_.send(opcode, sequence,
                 reinterpret_cast<const uint8_t *>(&wire_), sizeof(wire_));
}

void MacroQueue::fail() {
  wire_.report.state = Failed;
  safeStopRequested_ = true;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  if (capturePlayback_ && capturedData_) {
    restoreCaptureSnapshot();
  } else {
    head_ = used_ = 0;
    retainedSteps_ = 0;
    capturedData_ = false;
  }
  capturePlayback_ = false;
#else
  head_ = used_ = 0;
#endif
  sendStatus(ControllerProtocol::Event, 0);
}

bool MacroQueue::handle(const ControllerProtocol::Frame &frame) {
  const uint8_t *payload = frame.payload;
  const uint8_t length = frame.payloadLength;
  Report &report = wire_.report;
  if (frame.opcode == ControllerProtocol::MacroStart) {
    // BEGIN [schema, id, cancel flags, total steps LE16]. Per-step timed
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
    report.state = Buffering;
    report.id = payload[1];
    report.totalSteps = read16(payload + 3);
    options_ = payload[2];
    head_ = used_ = 0;
    safeStopRequested_ = false;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
    retainedSteps_ = 0;
    capturePlayback_ = false;
    capturedData_ = false;
#endif
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
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  if (length >= 3 && payload[0] == 3) {
    const uint16_t offset = read16(payload + 1);
    if (!captured() || offset > used_) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    sendCaptureChunk(frame.sequence, offset);
    return true;
  }
#endif
  if (length != 0 && payload[0] == 2) {
    sendStatus(ControllerProtocol::MacroStatusResponse, frame.sequence);
    return true;
  }
  if (length != 0 && payload[0] == 1) {
    if (report.state != Buffering ||
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
  if (length < 6 || payload[0] != 0 || !active() ||
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
    appendByte(payload[index]);
  }
  if (!bufferedRecordsValid()) {
    fail();
    protocol_.sendError(frame.sequence, frame.opcode,
                        ControllerProtocol::BadPayload);
    return true;
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

bool MacroQueue::dequeueDue(ControllerProtocol::Frame &frame) {
  Report &report = wire_.report;
  while (report.state == Playing && report.executedSteps < report.totalSteps) {
    if (!recordReady()) {
      return false;
    }
    const uint8_t payloadLength = peek(5);
    if (payloadLength > ControllerProtocol::MaximumPayload ||
        !MacroAction::playbackAllowed(peek(4))) {
      ++report.dispatchErrors;
      fail();
      return false;
    }
    const uint32_t actual = micros();
    uint32_t due = peekU32(0);
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
    if (capturePlayback_) {
      due -= capturePlaybackOffsetUs_;
    }
#endif
    const int32_t error =
        static_cast<int32_t>(actual - (startedAtUs_ + due));
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
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
    if (capturePlayback_ && retainedSteps_ != 0) {
      --retainedSteps_;
    }
#endif
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
    report.state = used_ == 0 && report.dispatchErrors == 0 ? Completed
                                                            : Failed;
    safeStopRequested_ = report.state == Failed;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
    if (capturePlayback_ && capturedData_) {
      restoreCaptureSnapshot();
    } else {
      head_ = used_ = 0;
      retainedSteps_ = 0;
    }
    capturePlayback_ = false;
#else
    head_ = used_ = 0;
#endif
    sendStatus(ControllerProtocol::Event, 0);
  }
}

void MacroQueue::cancel(bool keepOutputs) {
  if (!active()) {
    return;
  }
  const bool ownedOutputs = wire_.report.state == Playing;
  wire_.report.state = Cancelled;
  safeStopRequested_ = ownedOutputs && !keepOutputs;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  if (capturePlayback_ && capturedData_) {
    restoreCaptureSnapshot();
  } else {
    head_ = used_ = 0;
    retainedSteps_ = 0;
    capturedData_ = false;
  }
  capturePlayback_ = false;
#else
  head_ = used_ = 0;
#endif
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

bool MacroQueue::hostDependent() const {
  return wire_.report.state == Buffering || wire_.report.state == Playing;
}

#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
bool MacroQueue::beginCapture(uint8_t id, uint32_t atUs) {
  if (active()) {
    return false;
  }
  Report &report = wire_.report;
  memset(&report.acceptedSteps, 0,
         sizeof(report) - offsetof(Report, acceptedSteps));
  report.state = Recording;
  report.id = id;
  report.startedAtUs = atUs;
  startedAtUs_ = atUs;
  head_ = used_ = 0;
  retainedSteps_ = 0;
  capturePlaybackOffsetUs_ = 0;
  capturePlayback_ = false;
  capturedData_ = false;
  options_ = 0;
  safeStopRequested_ = false;
  sendStatus(ControllerProtocol::Event, 0);
  return true;
}

bool MacroQueue::captureAction(uint8_t opcode, const uint8_t *payload,
                               uint8_t availablePayload, uint32_t atUs) {
  if (!recording() || !MacroAction::recordable(opcode, availablePayload)) {
    return false;
  }
  const uint8_t payloadLength = MacroAction::payloadLength(opcode);
  if (payloadLength != 0 && payload == nullptr) {
    return false;
  }
  const uint8_t recordLength =
      static_cast<uint8_t>(MacroAction::RecordHeaderBytes + payloadLength);
  Report &report = wire_.report;
  if (static_cast<uint16_t>(used_) + recordLength > QueueCapacity) {
    // Never overwrite an unconsumed record. The exact retained prefix remains
    // downloadable/playable; a connected host still receives this action's
    // separate timestamped evidence and can preserve the complete sequence.
    if (report.droppedSteps != UINT16_MAX) {
      ++report.droppedSteps;
    }
    report.state = Captured;
    report.totalSteps = retainedSteps_;
    preserveCaptureSnapshot();
    sendStatus(ControllerProtocol::Event, 0);
    return false;
  }

  const uint32_t due = atUs - startedAtUs_;
  appendByte(static_cast<uint8_t>(due));
  appendByte(static_cast<uint8_t>(due >> 8));
  appendByte(static_cast<uint8_t>(due >> 16));
  appendByte(static_cast<uint8_t>(due >> 24));
  appendByte(opcode);
  appendByte(payloadLength);
  for (uint8_t index = 0; index < payloadLength; ++index) {
    appendByte(payload[index]);
  }
  if (report.acceptedSteps != UINT16_MAX) {
    ++report.acceptedSteps;
  }
  report.acceptedBytes = static_cast<uint16_t>(
      report.acceptedBytes + recordLength);
  ++retainedSteps_;
  report.totalSteps = retainedSteps_;
  return true;
}

bool MacroQueue::finishCapture() {
  if (!recording()) {
    return false;
  }
  wire_.report.state = Captured;
  wire_.report.totalSteps = retainedSteps_;
  preserveCaptureSnapshot();
  sendStatus(ControllerProtocol::Event, 0);
  return true;
}

bool MacroQueue::playCapture(uint32_t atUs) {
  if (!captured() || retainedSteps_ == 0 || !recordReady()) {
    return false;
  }
  Report &report = wire_.report;
  report.state = Playing;
  report.executedSteps = 0;
  report.dispatchErrors = 0;
  report.underruns = 0;
  report.totalSteps = retainedSteps_;
  startedAtUs_ = atUs;
  report.startedAtUs = atUs;
  capturePlaybackOffsetUs_ = peekU32(0);
  capturePlayback_ = true;
  safeStopRequested_ = false;
  sendStatus(ControllerProtocol::Event, 0);
  return true;
}

bool MacroQueue::recording() const {
  return wire_.report.state == Recording;
}

bool MacroQueue::captured() const {
  return capturedData_ && wire_.report.state != Recording &&
         wire_.report.state != Buffering && wire_.report.state != Playing;
}

uint16_t MacroQueue::retainedSteps() const { return retainedSteps_; }
#endif
