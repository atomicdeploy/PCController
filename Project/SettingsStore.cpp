#include "SettingsStore.h"

#include <EEPROM.h>
#include <string.h>

#include "UartProtocol.h"

namespace {

struct __attribute__((packed)) StoredSettings {
  ControllerSettings settings;
  uint8_t boardNameLength;
  char name[SettingsStore::MaximumBoardNameLength];
  uint8_t checksum;
};

#if !defined(PCCONTROLLER_SETTINGS_TEST_LAYOUT)
static_assert(sizeof(StoredSettings) == EepromLayout::SettingsRecordBytes,
              "settings/name EEPROM record layout changed");
#else
// Native contract tests deliberately enable the optional layout fields. Their
// desktop ABI padding is not an AVR EEPROM-layout assertion.
#endif

static_assert(SettingsStore::EepromAddress + sizeof(StoredSettings) <=
                  EepromLayout::RemoteHeaderAddress,
              "Settings overlap learned RF header");

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
  setDefaults();
  if (loadCurrent()) {
    persisted_ = true;
    bool rewriteLayout = normalizeMenuLayout();
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
          rewriteLayout = true;
          break;
        }
      }
    }
#endif
    if (rewriteLayout) {
      markDirty(now);
    }
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

bool SettingsStore::normalizeMenuLayout() {
#if PCCONTROLLER_MENU_LAYOUT_STORAGE
  bool changed = false;
  if (settings_.defaultMenuPage == PAGE_MOTION) {
    settings_.defaultMenuPage = PAGE_KEYS;
    changed = true;
  }

  const uint16_t aliasBit = static_cast<uint16_t>(1U << PAGE_MOTION);
  const uint16_t keyBit = static_cast<uint16_t>(1U << PAGE_KEYS);
  if ((settings_.visibleMenuMask & aliasBit) == 0) {
    return changed;
  }

  const bool keyWasVisible = (settings_.visibleMenuMask & keyBit) != 0;
  settings_.visibleMenuMask = static_cast<uint16_t>(
      (settings_.visibleMenuMask & static_cast<uint16_t>(~aliasBit)) | keyBit);
#if PCCONTROLLER_MENU_ORDERING
  // A layout that showed only MOVE meant to place the motion surface at that
  // rank. Swap the two unique IDs so RF and every other stored rank stay put;
  // the old MOVE slot remains hidden compatibility data.
  if (!keyWasVisible) {
    for (uint8_t rank = 0; rank < PersistentMenuPageCount; ++rank) {
      uint8_t &packed = settings_.menuOrder[rank >> 1];
      const uint8_t shift = (rank & 1U) == 0 ? 0 : 4;
      const uint8_t page = static_cast<uint8_t>((packed >> shift) & 0x0FU);
      if (page != PAGE_KEYS && page != PAGE_MOTION) {
        continue;
      }
      const uint8_t replacement =
          page == PAGE_KEYS ? static_cast<uint8_t>(PAGE_MOTION)
                           : static_cast<uint8_t>(PAGE_KEYS);
      packed = static_cast<uint8_t>(
          (packed & static_cast<uint8_t>(~(0x0FU << shift))) |
          static_cast<uint8_t>(replacement << shift));
    }
  }
#endif
  return true;
#else
  return false;
#endif
}

void SettingsStore::markDirty(uint32_t now) {
  dirty_ = true;
  changedAt_ = now;
}

bool SettingsStore::service(uint32_t now, bool allowWrite) {
  if (!allowWrite || !dirty_ ||
      static_cast<uint32_t>(now - changedAt_) < SaveDelayMs) {
    return false;
  }
  return saveNow();
}

bool SettingsStore::saveNow() {
  StoredSettings record{};
  record.settings = settings_;
  record.boardNameLength = boardNameLength_;
  memcpy(record.name, boardName_, boardNameLength_);
  record.checksum =
      checksum(&record, static_cast<uint8_t>(sizeof(record) - 1));

  const uint8_t *bytes = reinterpret_cast<const uint8_t *>(&record);
  for (uint8_t index = 0; index < sizeof(record); ++index) {
    EEPROM.update(EepromAddress + index, bytes[index]);
  }
  dirty_ = false;
  persisted_ = true;
  return true;
}

bool SettingsStore::dirty() const { return dirty_; }

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
  // Host-owned factory provisioning supplies the legacy packed menu bytes.
  // They intentionally stay zero in the volatile blank-EEPROM fallback.
  settings_.motionBreakMs = 1;
}

bool SettingsStore::loadCurrent() {
  StoredSettings record;
  EEPROM.get(EepromAddress, record);
  if (record.checksum !=
          checksum(&record, static_cast<uint8_t>(sizeof(record) - 1))) {
    return false;
  }
  if (record.boardNameLength > MaximumBoardNameLength ||
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
  settings_ = record.settings;
  boardNameLength_ = record.boardNameLength;
  memcpy(boardName_, record.name, boardNameLength_);
  return true;
}
