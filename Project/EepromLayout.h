#pragma once

#include <avr/io.h>

// Development layout v2. The user explicitly authorized a clean EEPROM
// reinitialization, so no legacy migration code is retained.
namespace EepromLayout {
constexpr int SettingsAddress = 32;
constexpr int RemoteHeaderAddress = 64;
constexpr int RemoteEntriesAddress = RemoteHeaderAddress + 4;
constexpr uint8_t RemoteCapacity = 20;
constexpr uint8_t RemoteRecordBytes = 12;
constexpr int RemoteEnd =
    RemoteEntriesAddress + RemoteCapacity * RemoteRecordBytes;
constexpr int ResetJournalAddress = 320;
constexpr uint8_t ResetJournalSlots = 64;
constexpr uint8_t ResetRecordBytes = 6;
constexpr int ResetJournalEnd =
    ResetJournalAddress + ResetJournalSlots * ResetRecordBytes;

static_assert(RemoteEnd <= ResetJournalAddress,
              "RF records overlap reset journal");
static_assert(ResetJournalEnd <= E2END + 1,
              "EEPROM layout exceeds ATmega328P EEPROM");
} // namespace EepromLayout
