#include <EEPROM.h>

#include "Project/EepromLayout.h"
#include "Project/ResetTelemetry.h"
#include "Project/UartProtocol.h"

#include <cstdint>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <string>

namespace {

constexpr std::uint8_t kMarker = 0xA7;

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

int slotAddress(std::uint8_t slot) {
  return EepromLayout::ResetJournalAddress +
         static_cast<int>(slot) * EepromLayout::ResetRecordBytes;
}

std::uint8_t checksum(std::uint32_t count) {
  const std::uint8_t bytes[] = {
      static_cast<std::uint8_t>(count),
      static_cast<std::uint8_t>(count >> 8U),
      static_cast<std::uint8_t>(count >> 16U),
      static_cast<std::uint8_t>(count >> 24U),
  };
  return ControllerProtocol::UartProtocol::crc8(bytes, sizeof(bytes));
}

void writeRecord(std::uint8_t slot, std::uint32_t count,
                 std::uint8_t marker = kMarker, bool corruptChecksum = false) {
  const int address = slotAddress(slot);
  EEPROM.update(address, static_cast<std::uint8_t>(count));
  EEPROM.update(address + 1, static_cast<std::uint8_t>(count >> 8U));
  EEPROM.update(address + 2, static_cast<std::uint8_t>(count >> 16U));
  EEPROM.update(address + 3, static_cast<std::uint8_t>(count >> 24U));
  EEPROM.update(address + 4,
                static_cast<std::uint8_t>(checksum(count) ^
                                          (corruptChecksum ? 0x5AU : 0U)));
  EEPROM.update(address + 5, marker);
}

std::uint32_t readCount(std::uint8_t slot) {
  const int address = slotAddress(slot);
  return static_cast<std::uint32_t>(EEPROM.read(address)) |
         (static_cast<std::uint32_t>(EEPROM.read(address + 1)) << 8U) |
         (static_cast<std::uint32_t>(EEPROM.read(address + 2)) << 16U) |
         (static_cast<std::uint32_t>(EEPROM.read(address + 3)) << 24U);
}

bool recordValid(std::uint8_t slot) {
  const int address = slotAddress(slot);
  const std::uint32_t count = readCount(slot);
  return EEPROM.read(address + 5) == kMarker && count != 0 &&
         EEPROM.read(address + 4) == checksum(count);
}

void requirePublishedLast(std::uint8_t slot) {
  const auto &updates = EEPROM.updates();
  require(!updates.empty(), "reset journal produced no EEPROM updates");
  const int markerAddress = slotAddress(slot) + 5;
  require(updates.front().address == markerAddress &&
              updates.front().value == 0,
          "reset record was not invalidated before its payload write");
  require(updates.back().address == markerAddress &&
              updates.back().value == kMarker,
          "reset record marker was not published last");
}

void testEmptyAndPublishOrder() {
  EEPROM.fill(0xFF);
  ResetTelemetry telemetry;
  telemetry.begin();
  require(telemetry.count() == 1 && recordValid(0) && readCount(0) == 1,
          "empty reset journal did not start at count 1");
  requirePublishedLast(0);
}

void testCorruptAndTornSlotsAreIgnored() {
  EEPROM.fill(0xFF);
  writeRecord(5, 41);
  writeRecord(6, 42, kMarker, true);
  writeRecord(7, 43, 0);
  EEPROM.clearUpdates();

  ResetTelemetry telemetry;
  telemetry.begin();
  require(telemetry.count() == 42 && recordValid(6) && readCount(6) == 42,
          "corrupt/torn future slots displaced the newest valid record");
  require(EEPROM.read(slotAddress(7) + 5) == 0,
          "journal recovery modified an unrelated torn slot");
  requirePublishedLast(6);
}

void testPhysicalJournalSlotRollover() {
  EEPROM.fill(0xFF);
  for (std::uint8_t slot = 0; slot < EepromLayout::ResetJournalSlots;
       ++slot) {
    writeRecord(slot, static_cast<std::uint32_t>(100U + slot));
  }
  EEPROM.clearUpdates();

  ResetTelemetry telemetry;
  telemetry.begin();
  require(telemetry.count() == 164 && recordValid(0) && readCount(0) == 164,
          "full reset journal did not wrap from its last slot to slot 0");
  requirePublishedLast(0);
}

void testCounterRollover() {
  EEPROM.fill(0xFF);
  writeRecord(62, std::numeric_limits<std::uint32_t>::max() - 1U);

  ResetTelemetry maximum;
  maximum.begin();
  require(maximum.count() == std::numeric_limits<std::uint32_t>::max() &&
              recordValid(63),
          "reset count did not advance to the valid UINT32_MAX record");

  ResetTelemetry wrapped;
  wrapped.begin();
  require(wrapped.count() == 1 && recordValid(0) && readCount(0) == 1,
          "reset count did not wrap from UINT32_MAX to 1");

  ResetTelemetry continued;
  continued.begin();
  require(continued.count() == 2 && recordValid(1) && readCount(1) == 2,
          "serial-number comparison did not continue after counter wrap");
}

} // namespace

int main() {
  try {
    testEmptyAndPublishOrder();
    testCorruptAndTornSlotsAreIgnored();
    testPhysicalJournalSlotRollover();
    testCounterRollover();
    std::cout << "reset_telemetry_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "reset_telemetry_tests: " << error.what() << '\n';
    return 1;
  }
}
