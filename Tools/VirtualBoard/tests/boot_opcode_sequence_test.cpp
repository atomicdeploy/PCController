#include <cassert>
#include <cstdint>
#include <vector>

#include <EEPROM.h>

#include "Project/BootOpcodeSequence.h"
#include "Project/EepromLayout.h"
#include "Project/UartProtocol.h"

namespace {

struct CapturedFrame {
  std::uint8_t opcode;
  std::uint8_t sequence;
  std::vector<std::uint8_t> payload;
};

struct Capture {
  std::vector<CapturedFrame> frames;
};

void capture(const ControllerProtocol::Frame &frame, void *context) {
  auto &result = *static_cast<Capture *>(context);
  result.frames.push_back({frame.opcode, frame.sequence,
                           {frame.payload, frame.payload + frame.payloadLength}});
}

void writeScript(const std::vector<std::uint8_t> &data,
                 bool validChecksum = true, bool committed = true) {
  assert(data.size() <= 26U);
  EEPROM.fill(0xFF);
  const int address = EepromLayout::BootOpcodeAddress;
  EEPROM.update(address, 0x42);
  EEPROM.update(address + 1, 0x4F);
  EEPROM.update(address + 2, 1);
  EEPROM.update(address + 3, static_cast<std::uint8_t>(data.size()));
  for (std::size_t index = 0; index < data.size(); ++index) {
    EEPROM.update(address + 5 + static_cast<int>(index), data[index]);
  }
  std::vector<std::uint8_t> checksumInput{0x42, 0x4F, 1,
                                          static_cast<std::uint8_t>(data.size())};
  checksumInput.insert(checksumInput.end(), data.begin(), data.end());
  std::uint8_t checksum = ControllerProtocol::UartProtocol::crc8(
      checksumInput.data(), static_cast<std::uint8_t>(checksumInput.size()));
  EEPROM.update(address + 4,
                validChecksum ? checksum : static_cast<std::uint8_t>(checksum ^ 0xFFU));
  // Commit last: firmware refuses a complete-looking record unless this exact
  // marker survives, which models a power loss during host EEPROM update.
  if (committed) {
    EEPROM.update(address + 31, 0xA7);
  }
}

void testBlankStorageIsQuiet() {
  EEPROM.fill(0xFF);
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture result;
  assert(BootOpcodeSequence::dispatch(protocol, capture, &result) == 0U);
  assert(result.frames.empty());
}

void testValidMixedSafeGroupsDispatchFifoWithoutWrites() {
  // STATUS_RGB once, followed by two ordinary Buzzer pauses. Both opcodes are
  // presentation-only and pass through the normal firmware dispatcher.
  writeScript({ControllerProtocol::StatusRgb, 1, 1, 2, 3, 4,
               ControllerProtocol::Buzzer, 2, 0, 0, 20, 0, 0, 0, 30, 0});
  EEPROM.clearUpdates();
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture result;
  const std::uint8_t count =
      BootOpcodeSequence::dispatch(protocol, capture, &result);
  assert(count == 3U);
  assert(result.frames.size() == 3U);
  assert(result.frames[0].opcode == ControllerProtocol::StatusRgb);
  assert(result.frames[1].opcode == ControllerProtocol::Buzzer);
  assert(result.frames[2].opcode == ControllerProtocol::Buzzer);
  assert(result.frames[2].payload[2] == 30U);
  for (const CapturedFrame &frame : result.frames) {
    assert(frame.sequence == BootOpcodeSequence::ExecutionSequence);
  }
  assert(EEPROM.updates().empty());
}

void testInvalidChecksumUnsafeOrTornRecordIsQuiet() {
  writeScript({ControllerProtocol::Buzzer, 1, 0, 0, 20, 0}, false);
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture checksumResult;
  assert(BootOpcodeSequence::dispatch(protocol, capture, &checksumResult) == 0U);
  assert(checksumResult.frames.empty());

  // RelaySet is intentionally excluded even with an otherwise correct CRC.
  writeScript({ControllerProtocol::RelaySet, 1, 0, 1});
  Capture unsafeResult;
  assert(BootOpcodeSequence::dispatch(protocol, capture, &unsafeResult) == 0U);
  assert(unsafeResult.frames.empty());

  writeScript({ControllerProtocol::Buzzer, 1, 0, 0, 20, 0}, true, false);
  Capture tornResult;
  assert(BootOpcodeSequence::dispatch(protocol, capture, &tornResult) == 0U);
  assert(tornResult.frames.empty());
}

void testExplicitEmptyCommittedScriptDisablesBootActions() {
  writeScript({});
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture result;
  assert(BootOpcodeSequence::dispatch(protocol, capture, &result) == 0U);
  assert(result.frames.empty());
}

void testMalformedTailNeverDispatchesValidPrefix() {
  // A valid Buzzer group followed by one orphan byte must be rejected before
  // the first frame is emitted, rather than partially playing the script.
  writeScript({ControllerProtocol::Buzzer, 1, 0, 0, 20, 0, 0xFF});
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture result;
  assert(BootOpcodeSequence::dispatch(protocol, capture, &result) == 0U);
  assert(result.frames.empty());
}

} // namespace

int main() {
  testBlankStorageIsQuiet();
  testValidMixedSafeGroupsDispatchFifoWithoutWrites();
  testInvalidChecksumUnsafeOrTornRecordIsQuiet();
  testExplicitEmptyCommittedScriptDisablesBootActions();
  testMalformedTailNeverDispatchesValidPrefix();
  return 0;
}
