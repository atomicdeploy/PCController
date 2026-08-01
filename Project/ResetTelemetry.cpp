#include "ResetTelemetry.h"

#include <EEPROM.h>
#include <avr/wdt.h>

#include "EepromLayout.h"
#include "UartProtocol.h"

namespace {
constexpr int ResetJournalEepromAddress = EepromLayout::ResetJournalAddress;
constexpr uint8_t ResetJournalSlots = EepromLayout::ResetJournalSlots;
constexpr uint8_t ResetRecordMarker = 0xA7;

struct __attribute__((packed)) ResetRecord {
  uint32_t count;
  uint8_t checksum;
  uint8_t marker;
};

static_assert(sizeof(ResetRecord) == EepromLayout::ResetRecordBytes,
              "Reset record layout changed");
static_assert(ResetJournalEepromAddress >= EepromLayout::RemoteEnd,
              "Reset journal overlaps learned RF records");

static_assert(
    ResetJournalEepromAddress +
            static_cast<int>(ResetJournalSlots) * sizeof(ResetRecord) <=
        E2END + 1,
    "Reset journal exceeds ATmega328P EEPROM");

uint8_t capturedResetCause __attribute__((section(".noinit")));

void captureResetCause()
    __attribute__((naked, used, section(".init3")));

void captureResetCause() {
  capturedResetCause = MCUSR;
  MCUSR = 0;
  wdt_disable();
}

uint8_t resetRecordChecksum(uint32_t count) {
  // 0x1F advances the protocol CRC-8 from its zero seed to the journal's
  // historical 0x5D seed, preserving every record written by older firmware
  // while sharing the already-linked CRC implementation.
  struct __attribute__((packed)) ChecksumInput {
    uint8_t seedPrefix;
    uint32_t count;
  };
  const ChecksumInput input = {0x1F, count};
  return ControllerProtocol::UartProtocol::crc8(
      reinterpret_cast<const uint8_t *>(&input), sizeof(input));
}

bool validResetRecord(const ResetRecord &record) {
  return record.marker == ResetRecordMarker && record.count != 0 &&
         record.count != UINT32_MAX &&
         record.checksum == resetRecordChecksum(record.count);
}

bool countIsNewer(uint32_t candidate, uint32_t reference) {
  return static_cast<int32_t>(candidate - reference) > 0;
}
} // namespace

ResetTelemetry resetTelemetry;

void ResetTelemetry::begin() {
  cause_ = capturedResetCause;

  bool found = false;
  uint8_t newestSlot = 0;
  ResetRecord record;
  for (uint8_t slot = 0; slot < ResetJournalSlots; ++slot) {
    EEPROM.get(
        ResetJournalEepromAddress +
            static_cast<int>(slot) * sizeof(ResetRecord),
        record);
    if (validResetRecord(record) &&
        (!found || countIsNewer(record.count, count_))) {
      found = true;
      newestSlot = slot;
      count_ = record.count;
    }
  }

  count_ = count_ == UINT32_MAX ? 1 : count_ + 1;

  const uint8_t nextSlot =
      found ? static_cast<uint8_t>((newestSlot + 1) % ResetJournalSlots) : 0;
  const int address =
      ResetJournalEepromAddress +
      static_cast<int>(nextSlot) * sizeof(ResetRecord);
  const uint8_t checksum = resetRecordChecksum(count_);

  // Invalidate first and publish the marker last. A power loss can therefore
  // leave only an ignored partial record, never a plausible newer count.
  EEPROM.update(address + offsetof(ResetRecord, marker), 0);
  const uint8_t *bytes =
      reinterpret_cast<const uint8_t *>(&count_);
  for (uint8_t index = 0; index < sizeof(count_); ++index) {
    EEPROM.update(address + index, bytes[index]);
  }
  EEPROM.update(address + offsetof(ResetRecord, checksum), checksum);
  EEPROM.update(address + offsetof(ResetRecord, marker), ResetRecordMarker);
}

uint8_t ResetTelemetry::cause() const { return cause_; }

uint32_t ResetTelemetry::count() const { return count_; }
