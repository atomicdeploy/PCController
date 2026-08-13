#pragma once

#include <avr/io.h>

// Canonical MCU-owned layout. Invalid records are replaced with defaults;
// firmware never carries a chain of development-layout migration handlers.
namespace EepromLayout {
// A fixed 32-byte boot-script slot is deliberately isolated from the
// settings/RF/reset/profile regions. It is [magic,schema,used,crc] at 0..4,
// 26 data bytes at 5..30, then a commit marker at 31. The optional feature
// validates it before dispatching and never writes it during ordinary boot.
constexpr int BootOpcodeAddress = 0;
constexpr uint8_t BootOpcodeBytes = 32;
constexpr int SettingsAddress = 32;
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
static_assert(BootOpcodeAddress + BootOpcodeBytes <= SettingsAddress,
              "boot opcodes overlap settings");
static_assert(ResetJournalEnd <= E2END + 1,
              "EEPROM layout exceeds ATmega328P EEPROM");
static_assert(StatusProfileEnd <= E2END + 1,
              "status profiles exceed ATmega328P EEPROM");
} // namespace EepromLayout
