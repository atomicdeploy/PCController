#include "MacroQueue.h"

#include <stddef.h>
#include <string.h>

#include "ControllerEvents.h"

namespace {
uint16_t read16(const uint8_t *value) {
  return static_cast<uint16_t>(value[0]) |
         static_cast<uint16_t>(value[1]) << 8;
}

uint32_t read32(const uint8_t *value) {
  return static_cast<uint32_t>(value[0]) |
         static_cast<uint32_t>(value[1]) << 8 |
         static_cast<uint32_t>(value[2]) << 16 |
         static_cast<uint32_t>(value[3]) << 24;
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
  return used_ >= MacroAction::RecordHeaderBytes &&
         used_ >= static_cast<uint8_t>(MacroAction::RecordHeaderBytes +
                                       peek(5));
}

bool MacroQueue::bufferedRecordsValid(uint16_t completeSteps) const {
  uint16_t offset = 0;
  uint16_t countedSteps = wire_.report.executedSteps;
  while (static_cast<uint16_t>(used_) - offset >=
         MacroAction::RecordHeaderBytes) {
    const uint8_t opcode = peek(static_cast<uint8_t>(offset + 4U));
    const uint8_t payloadLength = peek(static_cast<uint8_t>(offset + 5U));
    if (!MacroAction::validPlaybackPayload(opcode, payloadLength) ||
        peekU32(static_cast<uint8_t>(offset)) > 0x7FFFFFFFUL) {
      return false;
    }
    const uint16_t recordLength =
        MacroAction::RecordHeaderBytes + payloadLength;
    if (static_cast<uint16_t>(used_) - offset < recordLength) {
      break;
    }
    ++countedSteps;
    offset += recordLength;
  }
  return countedSteps == completeSteps;
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
  capturedData_ = true;
}

void MacroQueue::restoreCaptureSnapshot() {
  head_ = capturedHead_;
  used_ = capturedUsed_;
  retainedSteps_ = capturedSteps_;
}

void MacroQueue::restoreRetainedCaptureState() {
  restoreCaptureSnapshot();
  wire_.report.startedAtUs = captureStartedAtUs_;
  wire_.report.state =
      (options_ & CaptureExportAcknowledged) != 0 ? Exported : Captured;
  capturePlayback_ = false;
}

void MacroQueue::sendCaptureChunk(uint8_t sequence, uint16_t offset) {
  // [schema, command=3, id, total bytes LE16, offset LE16, chunk length,
  // raw ring bytes...].  Keeping chunks at forty bytes stays below the native
  // 48-byte payload without allocating another AVR buffer.
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
  wire_.report.fill = used_;
  protocol_.send(opcode, sequence,
                 reinterpret_cast<const uint8_t *>(&wire_), sizeof(wire_));
}

void MacroQueue::fail() {
  wire_.report.state = Failed;
  safeStopRequested_ = true;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  if (capturePlayback_ && capturedData_) {
    // Report the failed local replay, then restore the retained capture as a
    // first-class recoverable state.  A reconnect must never mistake a replay
    // failure for loss of the board-owned recording.
    sendStatus(ControllerProtocol::Event, 0);
    restoreRetainedCaptureState();
    sendStatus(ControllerProtocol::Event, 0);
    return;
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
    if (length != 5 || payload[0] != Schema) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    if (active()) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::Busy);
      return true;
    }
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
    // A host stream must not silently erase a capture that has not been
    // explicitly cleared, including one already acknowledged as exported.
    if (capturedData_) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::Busy);
      return true;
    }
    if ((payload[2] & CaptureInputs) != 0) {
      if (payload[2] != CaptureInputs || read16(payload + 3) != 0 ||
          !beginCapture(payload[1], micros())) {
        protocol_.sendError(frame.sequence, frame.opcode,
                            ControllerProtocol::BadPayload);
        return true;
      }
      protocol_.sendAck(frame.sequence, frame.opcode);
      return true;
    }
#endif
    if (read16(payload + 3) == 0 ||
        (payload[2] & static_cast<uint8_t>(~KeepOutputsOnCancel)) != 0) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
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
  // FINISH_CAPTURE [5]. CLEAR_CAPTURE keeps the identity-guarded six-byte
  // selector-5 form, so stop and destructive clear cannot be confused.
  if (length == 1 && payload[0] == 5) {
    if (!finishCapture()) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    protocol_.sendAck(frame.sequence, frame.opcode);
    return true;
  }
  // FETCH [3, id, offset LE16].  The identity byte prevents a delayed page
  // request from reading a replacement capture that reused the same ring.
  if (length == 4 && payload[0] == 3) {
    const uint16_t offset = read16(payload + 2);
    if (!captured() || payload[1] != report.id || offset > used_) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    sendCaptureChunk(frame.sequence, offset);
    return true;
  }
  // ACK_EXPORT [4, id, capture-start micros LE32].
  if (length == 6 && payload[0] == 4) {
    if (!captured() || payload[1] != report.id ||
        read32(payload + 2) != captureStartedAtUs_) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    options_ |= CaptureExportAcknowledged;
    report.state = Exported;
    protocol_.sendAck(frame.sequence, frame.opcode);
    sendStatus(ControllerProtocol::Event, 0);
    return true;
  }
  // CLEAR_CAPTURE [5, id, capture-start micros LE32].
  if (length == 6 && payload[0] == 5) {
    if (!captured() || payload[1] != report.id ||
        read32(payload + 2) != captureStartedAtUs_) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    head_ = used_ = capturedHead_ = capturedUsed_ = 0;
    retainedSteps_ = capturedSteps_ = 0;
    captureStartedAtUs_ = 0;
    capturedData_ = capturePlayback_ = false;
    options_ = 0;
    safeStopRequested_ = true;
    report.state = Idle;
    protocol_.sendAck(frame.sequence, frame.opcode);
    sendStatus(ControllerProtocol::Event, 0);
    return true;
  }
#endif
  // STATUS [2].
  if (length == 1 && payload[0] == 2) {
    sendStatus(ControllerProtocol::MacroStatusResponse, frame.sequence);
    return true;
  }
  // RUN [1].
  if (length == 1 && payload[0] == 1) {
    if (report.state != Buffering || !recordReady() ||
        (report.acceptedSteps < report.totalSteps && used_ < 64)) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    startedAtUs_ = micros();
    report.startedAtUs = startedAtUs_;
    report.state = Playing;
    protocol_.sendAck(frame.sequence, frame.opcode);
    sendStatus(ControllerProtocol::Event, 0);
    return true;
  }

  // APPEND [0][stream offset LE16][complete step index LE16][bytes...].
  if (length < 6 || payload[0] != 0 || !hostDependent() ||
      read16(payload + 1) != report.acceptedBytes ||
      read16(payload + 3) < report.acceptedSteps ||
      read16(payload + 3) > report.totalSteps ||
      static_cast<uint8_t>(length - 5) >
          static_cast<uint8_t>(QueueCapacity - used_)) {
    protocol_.sendError(frame.sequence, frame.opcode,
                        ControllerProtocol::BadPayload);
    return true;
  }
  const bool wasStarved = !recordReady();
  for (uint8_t index = 5; index < length; ++index) {
    appendByte(payload[index]);
  }
  if (!bufferedRecordsValid(read16(payload + 3))) {
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
    if (!MacroAction::validPlaybackPayload(peek(4), payloadLength)) {
      ++report.dispatchErrors;
      fail();
      return false;
    }
    if (static_cast<int32_t>(micros() - (startedAtUs_ + peekU32(0))) < 0) {
      return false;
    }
    frame.opcode = peek(4);
    frame.sequence = ExecutionSequence;
    frame.payloadLength = payloadLength;
    uint8_t *out = protocol_.framePayloadScratch();
    frame.payload = out;
    for (uint8_t index = 0; index < payloadLength; ++index) {
      out[index] = peek(static_cast<uint8_t>(6 + index));
    }
    const uint8_t recordLength =
        static_cast<uint8_t>(MacroAction::RecordHeaderBytes + payloadLength);
    head_ = static_cast<uint8_t>(head_ + recordLength) & QueueMask;
    used_ = static_cast<uint8_t>(used_ - recordLength);
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
    if (capturePlayback_ && retainedSteps_ != 0) {
      --retainedSteps_;
    }
#endif
    ++report.executedSteps;
    if (!MacroAction::macroQueueableOpcode(frame.opcode)) {
      completeStep(false);
      continue;
    }
    return true;
  }
  return false;
}

void MacroQueue::completeStep(bool succeeded) {
  Report &report = wire_.report;
  if (!succeeded && report.dispatchErrors != UINT8_MAX) {
    ++report.dispatchErrors;
  }
  if (report.executedSteps != report.totalSteps) {
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
    if (capturePlayback_) {
      // Local replay has no host playback goroutine consuming sequence-0xFE
      // evidence. Publish bounded progress so every connected monitor sees the
      // exact executed-step count without polling.
      sendStatus(ControllerProtocol::Event, 0);
    }
#endif
    return;
  }
  report.state = used_ == 0 && report.dispatchErrors == 0 ? Completed : Failed;
  safeStopRequested_ = report.state == Failed;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  if (capturePlayback_ && capturedData_) {
    // Preserve both lifecycle evidence and reconnect recovery: observers see
    // the terminal replay state followed by the retained capture state.
    sendStatus(ControllerProtocol::Event, 0);
    restoreRetainedCaptureState();
    sendStatus(ControllerProtocol::Event, 0);
    return;
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

void MacroQueue::cancel(bool keepOutputs) {
  if (!active()) {
    return;
  }
  const bool ownedOutputs = wire_.report.state == Playing;
  wire_.report.state = Cancelled;
  safeStopRequested_ = ownedOutputs && !keepOutputs;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
  if (capturePlayback_ && capturedData_) {
    sendStatus(ControllerProtocol::Event, 0);
    restoreRetainedCaptureState();
    sendStatus(ControllerProtocol::Event, 0);
    return;
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
  return wire_.report.state == Buffering ||
         (wire_.report.state == Playing
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE
          && !capturePlayback_
#endif
         );
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
  captureStartedAtUs_ = atUs;
  startedAtUs_ = atUs;
  head_ = used_ = 0;
  retainedSteps_ = 0;
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
  const uint32_t due = atUs - startedAtUs_;
  Report &report = wire_.report;
  if (due > 0x7FFFFFFFUL ||
      static_cast<uint16_t>(used_) + recordLength > QueueCapacity) {
    if (report.droppedSteps != UINT16_MAX) {
      ++report.droppedSteps;
    }
    report.state = Captured;
    report.totalSteps = retainedSteps_;
    preserveCaptureSnapshot();
    sendStatus(ControllerProtocol::Event, 0);
    return false;
  }
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
  report.acceptedBytes = static_cast<uint16_t>(report.acceptedBytes +
                                                recordLength);
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
  capturePlayback_ = true;
  safeStopRequested_ = false;
  sendStatus(ControllerProtocol::Event, 0);
  return true;
}

bool MacroQueue::recording() const { return wire_.report.state == Recording; }

bool MacroQueue::captured() const {
  return capturedData_ && wire_.report.state != Recording &&
         wire_.report.state != Buffering && wire_.report.state != Playing;
}

uint16_t MacroQueue::retainedSteps() const { return retainedSteps_; }
#endif
