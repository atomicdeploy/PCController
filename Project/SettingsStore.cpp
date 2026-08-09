#include "SettingsStore.h"

#include <EEPROM.h>
#include <string.h>

#include "UartProtocol.h"
#include "EepromAccess.h"

namespace {

struct __attribute__((packed)) StoredSettings {
  ControllerSettings settings;
  // Low nibble is name length 0..8; high nibble is modulo-16 bank generation.
  uint8_t boardNameMeta;
  char name[SettingsStore::MaximumBoardNameLength];
  uint8_t checksum;
};

static_assert(sizeof(StoredSettings) ==
                  sizeof(ControllerSettings) +
                      1U + SettingsStore::MaximumBoardNameLength + 1U,
              "settings/name EEPROM record packing changed");
static_assert(sizeof(StoredSettings) == SettingsRecordLayout::RecordBytes,
              "settings/name EEPROM schema byte count changed");

#if PCCONTROLLER_MENU_LAYOUT_STORAGE
static_assert(SettingsStore::EepromAddress + sizeof(StoredSettings) <=
                  EepromLayout::RemoteHeaderAddress,
              "feature-profile settings overlap learned RF header");
#else
static_assert(SettingsStore::EepromAddress + sizeof(StoredSettings) <=
                  EepromLayout::TemperatureRoleAddress,
              "production settings overlap temperature-role identity");
static_assert(sizeof(StoredSettings) == EepromLayout::SettingsBankBytes,
              "dual-bank settings must fill one 32-byte EEPROM bank");
#endif

uint8_t checksum(const void *value, uint8_t length) {
  return ControllerProtocol::UartProtocol::crc8(
      static_cast<const uint8_t *>(value), length);
}

} // namespace

SettingsStore settingsStore;

#if PCCONTROLLER_MENU_ORDERING
uint8_t firstVisiblePersistentMenuPage(uint16_t visibleMask,
                                       const uint8_t *packedOrder) {
  if (visibleMask == 0 ||
      (visibleMask & ~PersistentMenuAllPagesMask) != 0) {
    return 0xFF;
  }
  uint16_t seen = 0;
  uint8_t firstVisible = 0xFF;
  for (uint8_t rank = 0; rank < PersistentMenuPageCount; ++rank) {
    const uint8_t packed = packedOrder[rank >> 1];
    const uint8_t page = (rank & 1U) == 0
                             ? static_cast<uint8_t>(packed & 0x0FU)
                             : static_cast<uint8_t>(packed >> 4);
    const uint16_t pageBit = static_cast<uint16_t>(1U << page);
    if (page >= PersistentMenuPageCount || (seen & pageBit) != 0) {
      return 0xFF;
    }
    seen |= pageBit;
    if (firstVisible == 0xFF && (visibleMask & pageBit) != 0) {
      firstVisible = page;
    }
  }
  return seen == PersistentMenuAllPagesMask ? firstVisible : 0xFF;
}
#endif

bool SettingsStore::begin(uint32_t now) {
  dirty_ = false;
  persisted_ = false;
  writePending_ = false;
  saveImmediately_ = false;
  writeIndex_ = 0;
  generation_ = 0;
  activeBank_ = 0xFF;
  writeBank_ = 1;
  setDefaults();
  if (loadCurrent()) {
    persisted_ = true;
#if PCCONTROLLER_MENU_VISIBILITY
    if (!settings_.menuPageVisible(settings_.defaultMenuPage)) {
      for (uint8_t rank = 0; rank < PersistentMenuPageCount; ++rank) {
#if PCCONTROLLER_MENU_ORDERING
        const uint8_t page = settings_.menuPageAtRank(rank);
#else
        const uint8_t page = rank;
#endif
        if (settings_.menuPageVisible(page)) {
          settings_.defaultMenuPage = page;
          markDirty(now);
          break;
        }
      }
    }
#endif
    return true;
  }

  // A blank/corrupt board runs on the volatile safe fallback until the Go
  // host writes its canonical factory settings. Do not persist the AVR
  // fallback and make it indistinguishable from an initialized board.
  dirty_ = false;
  persisted_ = false;
  changedAt_ = now;
  return false;
}

ControllerSettings &SettingsStore::values() { return settings_; }

const ControllerSettings &SettingsStore::values() const { return settings_; }

void SettingsStore::markDirty(uint32_t now) {
  dirty_ = true;
  // RAM now differs from durable storage even when an older snapshot is still
  // being written. Hosts must not see a stale persisted=true while polling.
  persisted_ = false;
  changedAt_ = now;
}

bool generationNewer(uint8_t candidate, uint8_t current) {
  const uint8_t delta = static_cast<uint8_t>((candidate - current) & 0x0FU);
  return delta != 0 && delta < 8;
}

bool validRecord(const StoredSettings &record) {
  const uint8_t boardNameLength = record.boardNameMeta & 0x0FU;
  if (record.checksum !=
          checksum(&record, static_cast<uint8_t>(sizeof(record) - 1)) ||
      boardNameLength > SettingsStore::MaximumBoardNameLength ||
      record.settings.illuminationMode > 2 ||
      record.settings.displayBrightness > 7 ||
      (record.settings.outputPersistence & ~OutputPersistence::AllowedMask) != 0 ||
      record.settings.defaultMenuPage >= PersistentMenuPageCount ||
      record.settings.motionBreakMs == 0 ||
      (record.settings.streamPeriodMs != 0 &&
       record.settings.streamPeriodMs < 100)) {
    return false;
  }
#if PCCONTROLLER_MENU_VISIBILITY
  if (record.settings.visibleMenuMask == 0 ||
      (record.settings.visibleMenuMask & ~PersistentMenuAllPagesMask) != 0 ||
      record.settings.defaultMenuPage >= PersistentMenuPageCount) {
    return false;
  }
#endif
#if PCCONTROLLER_MENU_ORDERING
  if (firstVisiblePersistentMenuPage(record.settings.visibleMenuMask,
                                     record.settings.menuOrder) == 0xFF) {
    return false;
  }
#endif
  return true;
}

bool SettingsStore::service(uint32_t now, bool allowWrite) {
  if (!allowWrite) {
    return false;
  }
  if (!writePending_) {
    if (!dirty_ ||
        (!saveImmediately_ &&
         static_cast<uint32_t>(now - changedAt_) < SaveDelayMs)) {
      return false;
    }
    preparePendingRecord();
  }
  return servicePendingWrite();
}

bool SettingsStore::saveNow() {
  // If a snapshot is already in flight, keep it internally consistent and
  // request a second immediate snapshot rather than mixing two records.
  dirty_ = true;
  persisted_ = false;
  saveImmediately_ = true;
  return true;
}

bool SettingsStore::dirty() const { return dirty_ || writePending_; }

bool SettingsStore::persisted() const { return persisted_; }

bool SettingsStore::setBoardName(const uint8_t *name, uint8_t length) {
  // The Go API owns printable-ASCII/whitespace policy. The MCU still enforces
  // the hard storage bound; the full settings/name record is CRC-protected.
  if (length > MaximumBoardNameLength) {
    return false;
  }
  boardNameLength_ = length;
  memcpy(boardName_, name, length);
  return true;
}

bool SettingsStore::boardName(uint8_t *recordBytes) const {
  recordBytes[0] = boardNameLength_;
  memcpy(recordBytes + 1, boardName_, boardNameLength_);
  return persisted_;
}

void SettingsStore::setDefaults() {
  // The singleton's static storage supplies every zero-valued safe default at
  // boot. Only non-zero fallback fields need AVR instructions here; the Go
  // tooling owns and provisions the complete canonical factory record.
  settings_.illuminationOnBrightness = 128;
  settings_.displayBrightness = 5;
  settings_.statusBrightness = 128;
  settings_.streamPeriodMs = 500;
#if PCCONTROLLER_BLANK_EEPROM_SILENT
  settings_.flags |= SettingsFlags::Silent;
#endif
  // Host-owned factory provisioning supplies the legacy packed menu bytes.
  // They intentionally stay zero in the volatile blank-EEPROM fallback.
  settings_.motionBreakMs = 1;
}

bool SettingsStore::loadCurrent() {
  StoredSettings canonical;
  EEPROM.get(EepromAddress, canonical);
  const bool canonicalValid = validRecord(canonical);
  const StoredSettings *selected = canonicalValid ? &canonical : nullptr;
  uint8_t selectedBank = canonicalValid ? 1 : 0xFF;
#if !PCCONTROLLER_MENU_LAYOUT_STORAGE
  StoredSettings staging;
  EEPROM.get(EepromLayout::SettingsStagingAddress, staging);
  const bool stagingValid = validRecord(staging);
  if (stagingValid &&
      (!canonicalValid ||
       generationNewer(static_cast<uint8_t>(staging.boardNameMeta >> 4),
                       static_cast<uint8_t>(canonical.boardNameMeta >> 4)))) {
    selected = &staging;
    selectedBank = 0;
  }
#endif
  if (selected == nullptr) {
    return false;
  }
  settings_ = selected->settings;
  boardNameLength_ = selected->boardNameMeta & 0x0FU;
  memcpy(boardName_, selected->name, boardNameLength_);
  generation_ = static_cast<uint8_t>(selected->boardNameMeta >> 4);
  activeBank_ = selectedBank;
  return true;
}

void SettingsStore::preparePendingRecord() {
  static_assert(sizeof(StoredSettings) == sizeof(pendingRecord_),
                "pending settings buffer does not match EEPROM schema");
  StoredSettings record{};
  record.settings = settings_;
#if PCCONTROLLER_MENU_LAYOUT_STORAGE
  // The optional 41-byte feature layout cannot use the 32-byte staging bank.
  writeBank_ = 1;
#else
  writeBank_ = activeBank_ == 0 ? 1 : 0;
  if (activeBank_ == 0xFF) {
    writeBank_ = 1;
  }
#endif
  const uint8_t nextGeneration = activeBank_ == 0xFF
                                     ? 0
                                     : static_cast<uint8_t>(
                                           (generation_ + 1U) & 0x0FU);
  record.boardNameMeta = static_cast<uint8_t>(
      (nextGeneration << 4) | boardNameLength_);
  memcpy(record.name, boardName_, boardNameLength_);
  record.checksum =
      checksum(&record, static_cast<uint8_t>(sizeof(record) - 1));
  memcpy(pendingRecord_, &record, sizeof(record));
  dirty_ = false;
  saveImmediately_ = false;
  writePending_ = true;
  writeIndex_ = 0;
}

bool SettingsStore::servicePendingWrite() {
  if (!controllerEepromReady()) {
    return true;
  }
  constexpr uint8_t checksumIndex = sizeof(StoredSettings) - 1U;
  const int address = writeBank_ == 0
                          ? EepromLayout::SettingsStagingAddress
                          : EepromAddress;
  const int checksumAddress = address + checksumIndex;
  if (writeIndex_ == 0) {
    // Select a marker different from both the old and pending CRC, making the
    // record unambiguously invalid before any payload byte can change.
    uint8_t invalid = static_cast<uint8_t>(pendingRecord_[checksumIndex] + 1U);
    if (invalid == EEPROM.read(checksumAddress)) {
      ++invalid;
    }
    EEPROM.update(checksumAddress, invalid);
  } else if (writeIndex_ <= checksumIndex) {
    const uint8_t dataIndex = static_cast<uint8_t>(writeIndex_ - 1U);
    EEPROM.update(address + dataIndex, pendingRecord_[dataIndex]);
  } else if (writeIndex_ == static_cast<uint8_t>(checksumIndex + 1U)) {
    // Start CRC publication, then wait for EEPE to clear and read it back on a
    // later service turn before claiming durability.
    EEPROM.update(checksumAddress, pendingRecord_[checksumIndex]);
  } else {
    if (EEPROM.read(checksumAddress) != pendingRecord_[checksumIndex]) {
      EEPROM.update(checksumAddress, pendingRecord_[checksumIndex]);
      return true;
    }
    writePending_ = false;
    writeIndex_ = 0;
    generation_ = static_cast<uint8_t>(
        pendingRecord_[offsetof(StoredSettings, boardNameMeta)] >> 4);
    activeBank_ = writeBank_;
    persisted_ = !dirty_;
    return true;
  }
  ++writeIndex_;
  return true;
}
