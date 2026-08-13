#include "MacroQueue.h"

#include "ControllerEvents.h"

namespace {

uint16_t read16(const uint8_t *value) {
  return static_cast<uint16_t>(value[0]) |
         static_cast<uint16_t>(value[1]) << 8;
}

} // namespace

MacroQueue::MacroQueue(ControllerProtocol::UartProtocol &protocol)
    : protocol_(protocol),
      ring_(static_cast<uint8_t>(ControllerEventType::Macro)) {}

void MacroQueue::sendStatus(uint8_t opcode, uint8_t sequence) {
  // Events and query replies deliberately share one self-describing envelope.
  const ControllerCore::MacroRing::StatusEvent &status = ring_.status();
  protocol_.send(opcode, sequence,
                 reinterpret_cast<const uint8_t *>(&status), sizeof(status));
}

bool MacroQueue::handle(const ControllerProtocol::Frame &frame) {
  const uint8_t *payload = frame.payload;
  const uint8_t length = frame.payloadLength;
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
    ring_.begin(payload[1], payload[2], read16(payload + 3));
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
    cancel(length != 0 ? payload[0] != 0 : ring_.defaultKeepOutputsOnCancel());
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
  if (length != 0 && payload[0] == 1) {
    if (!ring_.start(micros())) {
      protocol_.sendError(frame.sequence, frame.opcode,
                          ControllerProtocol::BadPayload);
      return true;
    }
    // Host validation excludes empty macros, so RUN has one compact path.
    protocol_.sendAck(frame.sequence, frame.opcode);
    sendStatus(ControllerProtocol::Event, 0);
    return true;
  }

  // APPEND [0][stream offset LE16][complete step index LE16][bytes...].
  if (length < 6 || payload[0] != 0 ||
      !ring_.append(read16(payload + 1), read16(payload + 3), payload + 5,
                    static_cast<uint8_t>(length - 5), micros())) {
    protocol_.sendError(frame.sequence, frame.opcode,
                        ControllerProtocol::BadPayload);
    return true;
  }
  protocol_.sendAck(frame.sequence, frame.opcode);
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
