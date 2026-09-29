#include "MacroQueue.h"

#include "ControllerEvents.h"

namespace {

uint16_t read16(const uint8_t *value) {
  return static_cast<uint16_t>(value[0]) |
         static_cast<uint16_t>(value[1]) << 8;
}

} // namespace

MacroQueue::MacroQueue(ControllerProtocol::UartProtocol &protocol)
    : protocol_(protocol) {
  ring_.initialize(static_cast<uint8_t>(ControllerEventType::Macro));
}

void MacroQueue::sendStatus(uint8_t opcode, uint8_t sequence) {
  // Events and query replies deliberately share one self-describing envelope.
  const ControllerCore::MacroRing::StatusEvent &status = ring_.status();
  protocol_.send(opcode, sequence,
                 reinterpret_cast<const uint8_t *>(&status), sizeof(status));
}

bool MacroQueue::handle(const ControllerProtocol::Frame &frame, uint8_t relayMask) {
  const uint8_t *payload = frame.payload;
  const uint8_t length = frame.payloadLength;
  bool notify = true;
  if (frame.opcode == ControllerProtocol::MacroStart) {
    // BEGIN [id, cancel flags, total steps LE16]. Per-step timed
    // 0xFE ACK/error frames let the host calculate exact error without
    // duplicating tolerance/statistics code in AVR flash.
    // Duration remains host-owned because the hosted front-panel macro page
    // already has the complete library and this saves scarce AVR flash.
    if (length != 4) {
      goto badPayload;
    }
    if (active()) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::Busy);
      return true;
    }
    ring_.begin(payload[0], payload[1], read16(payload + 2));
    goto acknowledged;
  }
  if (frame.opcode == ControllerProtocol::MacroCancel) {
    if (length > 1 || (length != 0 && payload[0] > 1)) goto badPayload;
    cancel(length != 0 ? payload[0] != 0 : ring_.defaultKeepOutputsOnCancel());
    notify = false;
    goto acknowledged;
  }
  if (frame.opcode != ControllerProtocol::MacroStep) {
    return false;
  }
  if (length == 0) goto badPayload;
  switch (payload[0]) {
    case 0: // APPEND [0,streamOffsetLE16,completeStepIndexLE16,bytes...].
      if (length < 6 || !ring_.append(read16(payload + 1), read16(payload + 3),
          payload + 5, static_cast<uint8_t>(length - 5), micros())) goto badPayload;
      notify = false;
      break;
    case 1:
      if (length != 1 || !ring_.start(micros())) goto badPayload;
      break;
    case 2:
      if (length != 1) goto badPayload;
      sendStatus(ControllerProtocol::MacroStatusResponse, frame.sequence);
      return true;
    case 3: // RECORD BEGIN [3,id], including the current physical baseline.
      if (length != 2 || active()) goto badPayload;
      ring_.beginRecording(payload[1], relayMask, micros());
      break;
    case 4: // RECORD STOP retains the circular RAM contents.
      if (length != 1 || !ring_.stopRecording()) goto badPayload;
      break;
    case 5: { // READ [5,byteOffsetLE16]; response [event,0x80,offsetLE16,snapshots].
      if (length != 3 || !ring_.hasRecording()) goto badPayload;
      const uint16_t offset = read16(payload + 1);
      if (offset > ring_.status().report.acceptedBytes ||
          static_cast<uint8_t>(offset) % ControllerCore::MacroRing::SnapshotBytes != 0) goto badPayload;
      uint8_t *response = protocol_.framePayloadScratch();
      response[0] = static_cast<uint8_t>(ControllerEventType::Macro);
      response[1] = 0x80;
      response[2] = static_cast<uint8_t>(offset);
      response[3] = static_cast<uint8_t>(offset >> 8);
      const uint8_t count = ring_.readRecording(offset, response + 4,
                                                ControllerProtocol::MaximumPayload - 4);
      protocol_.send(ControllerProtocol::MacroStatusResponse, frame.sequence,
                     response, static_cast<uint8_t>(count + 4));
      return true;
    }
    case 6: // Replay retained snapshots through ordinary safe RelaySet dispatch.
      if (length != 1 || !ring_.startRecorded(micros(), ControllerProtocol::RelaySet)) goto badPayload;
      break;
    case 7: // Explicitly release retained RAM before strip streaming may borrow it.
      if (length != 1 || !ring_.clearRecording()) goto badPayload;
      break;
    default: goto badPayload;
  }
acknowledged:
  protocol_.sendAck(frame.sequence, frame.opcode);
  if (notify) sendStatus(ControllerProtocol::Event, 0);
  return true;
badPayload:
  protocol_.sendError(frame.sequence, frame.opcode, ControllerProtocol::BadPayload);
  return true;
}

bool MacroQueue::dequeueDue(ControllerProtocol::Frame &frame) {
  for (;;) {
    ControllerCore::MacroRing::Command command;
    const ControllerCore::MacroRing::DequeueResult result = ring_.dequeueDue(
        micros(), command, protocol_.framePayloadScratch(),
        ControllerProtocol::MaximumPayload);
    if (result == ControllerCore::MacroRing::Malformed) {
      sendStatus(ControllerProtocol::Event, 0);
      return false;
    }
    if (result != ControllerCore::MacroRing::Ready) {
      return false;
    }
    frame.opcode = command.opcode;
    frame.sequence = ExecutionSequence;
    frame.payloadLength = command.payloadLength;
    frame.payload = protocol_.framePayloadScratch();
    if (frame.opcode >= ControllerProtocol::MacroStart &&
        frame.opcode <= ControllerProtocol::MacroStep) {
      // Replayed macro-control records are rejected through the same result
      // accounting path as a dispatcher failure; never recursively schedule.
      completeStep(false);
      continue;
    }
    return true;
  }
}

void MacroQueue::completeStep(bool succeeded) {
  if (ring_.completeStep(succeeded)) {
    sendStatus(ControllerProtocol::Event, 0);
  }
}

void MacroQueue::cancel(bool keepOutputs) {
  if (ring_.cancel(keepOutputs)) {
    sendStatus(ControllerProtocol::Event, 0);
  }
}

bool MacroQueue::takeSafeStopRequest() { return ring_.takeSafeStopRequest(); }

bool MacroQueue::active() const { return ring_.active(); }

void MacroQueue::recordRelay(uint8_t mask, uint32_t nowUs) {
  ring_.recordRelay(mask, nowUs);
}
