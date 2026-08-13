#include "BootOpcodeSequence.h"

#include <EEPROM.h>

#include "EepromLayout.h"

namespace {

constexpr uint8_t BootMagic0 = 0x42; // B
constexpr uint8_t BootMagic1 = 0x4F; // O
constexpr uint8_t BootSchema = 1;
constexpr uint8_t BootCommitted = 0xA7;
uint8_t BootExecutionToken;

} // namespace

uint8_t BootOpcodeSequence::dispatch(ControllerProtocol::UartProtocol &protocol,
                                     Dispatch callback, void *context) {
  if (callback == nullptr) {
    return 0;
  }

  // Reuse protocol-owned scratch rather than reserve boot-only SRAM. The boot
  // sequence runs synchronously before the cooperative RX service loop.
  uint8_t *const record = protocol.framePayloadScratch();
  const int address = EepromLayout::BootOpcodeAddress;
  for (uint8_t index = 0; index < EepromLayout::BootOpcodeBytes; ++index) {
    record[index] = EEPROM.read(address + index);
  }

  const uint8_t used = record[3];
  const uint8_t storedCrc = record[4];
  if (record[0] != BootMagic0 || record[1] != BootMagic1 ||
      record[2] != BootSchema || record[CommitOffset] != BootCommitted ||
      used > DataBytes) {
    return 0;
  }

  // CRC covers the immutable header prefix plus only declared data. The
  // commit byte lives at byte 31 and is written last by provisioning/update
  // code; any torn update therefore fails closed without a migration path.
  const uint8_t crcInputBytes = 4;
  for (uint8_t index = 0; index < used; ++index) {
    record[crcInputBytes + index] = record[DataOffset + index];
  }
  if (ControllerProtocol::UartProtocol::crc8(record,
                                              crcInputBytes + used) !=
      storedCrc) {
    return 0;
  }

  // Validate every group before dispatching the first one. Thus a malformed
  // tail cannot turn a partially written record into a partial boot action.
  const uint8_t dataEnd = static_cast<uint8_t>(crcInputBytes + used);
  uint8_t cursor = crcInputBytes;
  uint8_t entries = 0;
  while (cursor < dataEnd) {
    // A group is [safe opcode][repeat] followed by repeat fixed-size payloads.
    // Payload length is inferred by the strict whitelist, not EEPROM metadata.
    if (cursor + 2U > dataEnd) {
      return 0;
    }
    const uint8_t opcode = record[cursor++];
    const uint8_t repeat = record[cursor++];
    const uint8_t length = payloadLength(opcode);
    const uint16_t bytes = static_cast<uint16_t>(length) * repeat;
    if (length == 0 || repeat == 0 || entries + repeat > MaximumDispatches ||
        bytes > static_cast<uint16_t>(dataEnd - cursor)) {
      return 0;
    }
    cursor = static_cast<uint8_t>(cursor + bytes);
    entries = static_cast<uint8_t>(entries + repeat);
  }
  if (cursor != dataEnd) {
    return 0;
  }

  cursor = crcInputBytes;
  uint8_t dispatched = 0;
  while (cursor < dataEnd) {
    const uint8_t opcode = record[cursor++];
    const uint8_t repeat = record[cursor++];
    const uint8_t length = payloadLength(opcode);
    for (uint8_t entry = 0; entry < repeat; ++entry) {
      const ControllerProtocol::Frame frame = {
          opcode, ExecutionSequence, length, record + cursor};
      callback(frame, context);
      cursor = static_cast<uint8_t>(cursor + length);
      ++dispatched;
    }
  }
  return dispatched;
}

void *BootOpcodeSequence::executionContext() { return &BootExecutionToken; }

bool BootOpcodeSequence::isExecutionContext(const void *context) {
  return context == &BootExecutionToken;
}

uint8_t BootOpcodeSequence::payloadLength(uint8_t opcode) {
  // The EEPROM script is a presentation-only boot hook. It cannot start
  // motion/relays/PWM/RF/I2C/macros, reset/program, or mutate stored settings.
  if (opcode == ControllerProtocol::Buzzer ||
      opcode == ControllerProtocol::StatusRgb) {
    return 4;
  }
  return opcode == ControllerProtocol::StatusEffect ? 12 : 0;
}
