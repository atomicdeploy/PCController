#pragma once

#include <avr/io.h>

// Canonical MCU-owned layout. Invalid records are replaced with defaults;
// firmware never carries a chain of development-layout migration handlers.
namespace EepromLayout {
// Production settings use two CRC banks. Urboot owns no EEPROM metadata; bank
// zero was previously unallocated application space.
constexpr int SettingsStagingAddress = 0;
constexpr uint8_t SettingsBankBytes = 32;
constexpr int SettingsAddress = 32;
constexpr int TemperatureRoleAddress = 64;
constexpr uint8_t TemperatureRoleBytes = 16;
constexpr int RemoteHeaderAddress = 80;
constexpr int RemoteEntriesAddress = RemoteHeaderAddress + 4;
constexpr uint8_t RemoteCapacity = 20;
constexpr uint8_t RemoteRecordBytes = 12;
constexpr int RemoteEnd =
    RemoteEntriesAddress + RemoteCapacity * RemoteRecordBytes;
constexpr int ResetJournalAddress = 336;
constexpr uint8_t ResetJournalSlots = 64;
constexpr uint8_t ResetRecordBytes = 6;
constexpr int ResetJournalEnd =
    ResetJournalAddress + ResetJournalSlots * ResetRecordBytes;
// Nineteen condition slots persist the exact 12-byte STATUS_EFFECT descriptor
// plus a per-record CRC. The Go host provisions rich defaults; invalid or
// unwritten slots use only the firmware's compact safety fallback.
constexpr int StatusProfileAddress = ResetJournalEnd;
constexpr uint8_t StatusProfileCount = 19;
constexpr uint8_t StatusProfilePayloadBytes = 12;
constexpr uint8_t StatusProfileRecordBytes = StatusProfilePayloadBytes + 1;
constexpr int StatusProfileEnd =
    StatusProfileAddress + StatusProfileCount * StatusProfileRecordBytes;

static_assert(RemoteEnd <= ResetJournalAddress,
              "RF records overlap reset journal");
static_assert(ResetJournalEnd <= E2END + 1,
              "EEPROM layout exceeds ATmega328P EEPROM");
static_assert(StatusProfileEnd <= E2END + 1,
              "status profiles exceed ATmega328P EEPROM");
static_assert(SettingsStagingAddress + SettingsBankBytes <= SettingsAddress,
              "settings staging bank overlaps canonical settings");
static_assert(SettingsAddress + SettingsBankBytes <= TemperatureRoleAddress,
              "canonical settings bank overlaps temperature role identity");
static_assert(TemperatureRoleAddress + TemperatureRoleBytes <=
                  RemoteHeaderAddress,
              "temperature role identity overlaps learned RF header");
} // namespace EepromLayout
