#pragma once

#include <avr/io.h>

// Canonical MCU-owned layout. Invalid records are replaced with defaults;
// firmware never carries a chain of development-layout migration handlers.
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
constexpr int AutomationBankAAddress = ResetJournalEnd;
constexpr uint8_t AutomationHeaderBytes = 10;
constexpr uint8_t AutomationCapacity = 12;
constexpr uint8_t AutomationRecordBytes = 12;
constexpr int AutomationBankBytes =
    AutomationHeaderBytes + AutomationCapacity * AutomationRecordBytes;
constexpr int AutomationBankBAddress =
    AutomationBankAAddress + AutomationBankBytes;
constexpr int AutomationEnd = AutomationBankBAddress + AutomationBankBytes;

static_assert(RemoteEnd <= ResetJournalAddress,
              "RF records overlap reset journal");
static_assert(ResetJournalEnd <= E2END + 1,
              "EEPROM layout exceeds ATmega328P EEPROM");
static_assert(AutomationBankAAddress >= ResetJournalEnd,
              "automation store overlaps reset journal");
static_assert(AutomationEnd <= E2END + 1,
              "automation store exceeds ATmega328P EEPROM");
} // namespace EepromLayout
