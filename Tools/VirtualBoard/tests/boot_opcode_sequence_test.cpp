#include <cstdint>
#include <stdexcept>
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

void require(bool condition, const char *message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

void capture(const ControllerProtocol::Frame &frame, void *context) {
  auto &result = *static_cast<Capture *>(context);
  result.frames.push_back({frame.opcode, frame.sequence,
                           {frame.payload, frame.payload + frame.payloadLength}});
}

void writeScript(const std::vector<std::uint8_t> &data,
                 bool validChecksum = true, bool committed = true) {
  constexpr std::size_t dataCapacity =
      EepromLayout::BootOpcodeBytes - 4U;
  require(data.size() <= dataCapacity, "test boot script exceeds EEPROM slot");
  EEPROM.fill(0xFF);
  for (int index = 0; index < EepromLayout::AudioCueBytes; ++index) {
    EEPROM.update(EepromLayout::AudioCueAddress + index, 0x5A);
  }
  const int address = EepromLayout::BootOpcodeAddress;
  EEPROM.update(address, 0xB0);
  EEPROM.update(address + 1, static_cast<std::uint8_t>(data.size()));
  for (std::size_t index = 0; index < data.size(); ++index) {
    EEPROM.update(address + 3 + static_cast<int>(index), data[index]);
  }
  std::vector<std::uint8_t> checksumInput{0xB0,
                                          static_cast<std::uint8_t>(data.size())};
  checksumInput.insert(checksumInput.end(), data.begin(), data.end());
  std::uint8_t checksum = ControllerProtocol::UartProtocol::crc8(
      checksumInput.data(), static_cast<std::uint8_t>(checksumInput.size()));
  EEPROM.update(address + 2,
                validChecksum ? checksum : static_cast<std::uint8_t>(checksum ^ 0xFFU));
  // Commit last: firmware refuses a complete-looking record unless this exact
  // marker survives, which models a power loss during host EEPROM update.
  if (committed) {
    EEPROM.update(address + EepromLayout::BootOpcodeBytes - 1, 0xA7);
  }
}

void testBlankStorageIsQuiet() {
  EEPROM.fill(0xFF);
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture result;
  require(BootOpcodeSequence::dispatch(protocol, capture, &result) == 0U,
          "blank storage dispatched a boot frame");
  require(result.frames.empty(), "blank storage captured a boot frame");
}

void testValidMixedSafeGroupsDispatchFifoWithoutWrites() {
  // STATUS_RGB once, followed by one ordinary Buzzer pause. Both opcodes are
  // presentation-only and pass through the normal firmware dispatcher.
  writeScript({ControllerProtocol::StatusRgb, 1, 2, 3, 4,
               ControllerProtocol::Buzzer, 0, 0, 30, 0});
  EEPROM.clearUpdates();
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture result;
  const std::uint8_t count =
      BootOpcodeSequence::dispatch(protocol, capture, &result);
  require(count == 2U, "valid compact script dispatch count drifted");
  require(result.frames.size() == 2U,
          "valid compact script captured the wrong frame count");
  require(result.frames[0].opcode == ControllerProtocol::StatusRgb,
          "first compact script opcode was not STATUS_RGB");
  require(result.frames[1].opcode == ControllerProtocol::Buzzer,
          "second compact script opcode was not BUZZER");
  require(result.frames[1].payload[2] == 30U,
          "compact buzzer duration payload drifted");
  for (const CapturedFrame &frame : result.frames) {
    require(frame.sequence == BootOpcodeSequence::ExecutionSequence,
            "boot frame did not use the private execution sequence");
  }
  for (int index = 0; index < EepromLayout::AudioCueBytes; ++index) {
    require(EEPROM.read(EepromLayout::AudioCueAddress + index) == 0x5A,
            "boot script write overlapped autonomous audio cues");
  }
  require(EEPROM.updates().empty(), "boot dispatch wrote EEPROM");
}

void testInvalidChecksumUnsafeOrTornRecordIsQuiet() {
  writeScript({ControllerProtocol::Buzzer, 0, 0, 20, 0}, false);
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture checksumResult;
  require(BootOpcodeSequence::dispatch(protocol, capture, &checksumResult) == 0U,
          "bad checksum dispatched a boot frame");
  require(checksumResult.frames.empty(),
          "bad checksum captured a boot frame");

  // RelaySet is intentionally excluded even with an otherwise correct CRC.
  writeScript({ControllerProtocol::RelaySet, 0, 1});
  Capture unsafeResult;
  require(BootOpcodeSequence::dispatch(protocol, capture, &unsafeResult) == 0U,
          "unsafe opcode dispatched a boot frame");
  require(unsafeResult.frames.empty(), "unsafe opcode captured a boot frame");

  writeScript({ControllerProtocol::Buzzer, 0, 0, 20, 0}, true, false);
  Capture tornResult;
  require(BootOpcodeSequence::dispatch(protocol, capture, &tornResult) == 0U,
          "torn record dispatched a boot frame");
  require(tornResult.frames.empty(), "torn record captured a boot frame");
}

void testExplicitEmptyCommittedScriptDisablesBootActions() {
  writeScript({});
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture result;
  require(BootOpcodeSequence::dispatch(protocol, capture, &result) == 0U,
          "empty committed script dispatched a boot frame");
  require(result.frames.empty(), "empty committed script captured a frame");
}

void testMalformedTailNeverDispatchesValidPrefix() {
  // A valid Buzzer entry followed by one orphan byte must be rejected before
  // the first frame is emitted, rather than partially playing the script.
  writeScript({ControllerProtocol::Buzzer, 0, 0, 20, 0, 0xFF});
  HardwareSerial serial;
  ControllerProtocol::UartProtocol protocol(serial);
  Capture result;
  require(BootOpcodeSequence::dispatch(protocol, capture, &result) == 0U,
          "malformed tail dispatched a valid prefix");
  require(result.frames.empty(), "malformed tail captured a boot frame");
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
