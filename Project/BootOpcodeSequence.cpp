#include "BootOpcodeSequence.h"

#include <EEPROM.h>

#include "EepromLayout.h"

namespace {

constexpr uint8_t BootMagic = 0xB0;
constexpr uint8_t BootCommitted = 0xA7;

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

  const uint8_t used = record[1];
  const uint8_t storedCrc = record[2];
  if (record[0] != BootMagic || record[CommitOffset] != BootCommitted ||
      used > DataBytes) {
    return 0;
  }

  // CRC covers the immutable header prefix plus only declared data. The
  // commit byte lives at the final slot byte and is written last by update
  // code; any torn update therefore fails closed without a migration path.
  const uint8_t crcInputBytes = 2;
  for (uint8_t index = 0; index < used; ++index) {
    record[crcInputBytes + index] = record[DataOffset + index];
  }
  if (ControllerProtocol::UartProtocol::crc8(record,
                                              crcInputBytes + used) !=
      storedCrc) {
    return 0;
  }

  // Validate every entry before dispatching the first one. Thus a malformed
  // tail cannot turn a partially written record into a partial boot action.
  const uint8_t dataEnd = static_cast<uint8_t>(crcInputBytes + used);
  uint8_t cursor = crcInputBytes;
  while (cursor < dataEnd) {
    // Each entry is [safe opcode][fixed payload]. Payload length comes from
    // the strict whitelist, never EEPROM metadata.
    const uint8_t opcode = record[cursor++];
    const uint8_t length = payloadLength(opcode);
    if (length == 0 || length > static_cast<uint8_t>(dataEnd - cursor)) {
      return 0;
    }
    cursor = static_cast<uint8_t>(cursor + length);
  }
  if (cursor != dataEnd) {
    return 0;
  }

  cursor = crcInputBytes;
  uint8_t dispatched = 0;
  while (cursor < dataEnd) {
    const uint8_t opcode = record[cursor++];
    const uint8_t length = payloadLength(opcode);
    const ControllerProtocol::Frame frame = {
        opcode, ExecutionSequence, length, record + cursor};
    callback(frame, context);
    cursor = static_cast<uint8_t>(cursor + length);
    ++dispatched;
  }
  return dispatched;
}

uint8_t BootOpcodeSequence::payloadLength(uint8_t opcode) {
  // The EEPROM script is a presentation-only boot hook. It cannot start
  // motion/relays/PWM/RF/I2C/macros, reset/program, or mutate stored settings.
  return opcode == ControllerProtocol::Buzzer ||
                 opcode == ControllerProtocol::StatusRgb
             ? 4
             : 0;
}
