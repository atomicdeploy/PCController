#pragma once

#include <avr/io.h>

// Canonical MCU-owned layout. Invalid records are replaced with defaults;
// firmware never carries a chain of development-layout migration handlers.
namespace EepromLayout {
constexpr int SettingsAddress = 32;
constexpr uint8_t SettingsValueBytes = 40;
constexpr uint8_t SettingsRecordBytes = SettingsValueBytes + 1;
constexpr int SettingsEnd = SettingsAddress + SettingsRecordBytes;
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

// Optional host-provisioned, packed 4-character front-panel labels occupy the
// final EEPROM bytes. Their CRC header uses the explicit free gap after
// settings and before the RF header: boot opcodes own 0..31, settings own
// 32..72, and neither the RF nor status-profile regions overlap it.
constexpr int MenuLabelsHeaderAddress = SettingsEnd;
// The commit byte is a format marker as well as the final write. Seeding
// CRC-8/ATM with it binds the checksum to this exact record version without a
// second version field or collision with allocated EEPROM regions.
constexpr uint8_t MenuLabelsFormatMarker = 0xA1;
constexpr int MenuLabelsCrcAddress = MenuLabelsHeaderAddress;
constexpr int MenuLabelsHeaderEnd = MenuLabelsCrcAddress + 1;
constexpr int MenuLabelsAddress = StatusProfileEnd;
constexpr uint8_t MenuLabelCount = 14;
constexpr uint8_t MenuLabelBytes = MenuLabelCount * 4;
// The last byte is a transactional commit marker. Host updates invalidate it
// first, write payload/header, then commit it last; interrupted writes remain
// unavailable instead of exposing a partially written label table.
constexpr int MenuLabelsCommitAddress = MenuLabelsAddress + MenuLabelBytes;
constexpr int MenuLabelsEnd = MenuLabelsCommitAddress + 1;

static_assert(SettingsEnd <= MenuLabelsHeaderAddress,
              "settings overlap menu-label header");
static_assert(MenuLabelsHeaderEnd <= RemoteHeaderAddress,
              "menu-label header overlaps RF records");
static_assert(RemoteEnd <= ResetJournalAddress,
              "RF records overlap reset journal");
static_assert(ResetJournalEnd <= E2END + 1,
              "EEPROM layout exceeds ATmega328P EEPROM");
static_assert(StatusProfileEnd <= E2END + 1,
              "status profiles exceed ATmega328P EEPROM");
static_assert(MenuLabelsAddress == StatusProfileEnd,
              "menu labels must begin after status profiles");
static_assert(MenuLabelsEnd <= E2END + 1,
              "menu labels exceed ATmega328P EEPROM");
} // namespace EepromLayout
