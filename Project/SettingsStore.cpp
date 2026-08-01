#include "SettingsStore.h"

#include <EEPROM.h>
#include <string.h>

#include "UartProtocol.h"

namespace {

struct __attribute__((packed)) StoredSettings {
  ControllerSettings settings;
  uint8_t checksum;
};

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
      (visibleMask & ~PersistentMenuAllPagesMask) != 0 ||
      (packedOrder[7] & 0xF0U) != 0xF0U) {
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

  dirty_ = true;
  changedAt_ = now - SaveDelayMs;
  return false;
}

ControllerSettings &SettingsStore::values() { return settings_; }

const ControllerSettings &SettingsStore::values() const { return settings_; }

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
  record.checksum =
      checksum(&record, static_cast<uint8_t>(sizeof(record) - 1));

  const uint8_t *bytes = reinterpret_cast<const uint8_t *>(&record);
  for (uint8_t index = 0; index < sizeof(record); ++index) {
    EEPROM.update(EepromAddress + index, bytes[index]);
  }
  dirty_ = false;
  return true;
}

bool SettingsStore::dirty() const { return dirty_; }

void SettingsStore::setDefaults() {
  // MotionDoorPolicy::Always is the zero encoding in the cleared policy bits.
  settings_.flags = SettingsFlags::SwapTemperatureSensors;
#if PCCONTROLLER_SAFE_EEPROM_MIGRATION
  settings_.illuminationMode = 0; // Off during one-shot EEPROM migration.
#else
  settings_.illuminationMode = 1; // Auto
#endif
  settings_.illuminationOnBrightness = 128;
  settings_.illuminationOffBrightness = 0;
  settings_.displayBrightness = 5;
  settings_.statusBrightness = 128;
#if PCCONTROLLER_SAFE_EEPROM_MIGRATION
  settings_.pwmBootMode = 0; // Off during one-shot EEPROM migration.
#else
  settings_.pwmBootMode = 2; // Auto test
#endif
  settings_.streamPeriodMs = 500;
  memset(settings_.userPwm, 0, sizeof(settings_.userPwm));
  settings_.defaultMenuPage = 0;
  settings_.menuFlags = 0; // Keep Status as the deterministic factory page.
#if PCCONTROLLER_MENU_VISIBILITY
  settings_.visibleMenuMask = PersistentMenuAllPagesMask;
#endif
#if PCCONTROLLER_MENU_ORDERING
  for (uint8_t pair = 0; pair < 8; ++pair) {
    const uint8_t first = static_cast<uint8_t>(pair << 1);
    settings_.menuOrder[pair] =
        static_cast<uint8_t>(first | ((first + 1U) << 4));
  }
#endif
}

bool SettingsStore::loadCurrent() {
  StoredSettings record;
  EEPROM.get(EepromAddress, record);
  if (record.checksum !=
          checksum(&record, static_cast<uint8_t>(sizeof(record) - 1))) {
    return false;
  }
  if (record.settings.illuminationMode > 2 ||
      record.settings.displayBrightness > 7 ||
      record.settings.pwmBootMode > 2 ||
      record.settings.defaultMenuPage >= PersistentMenuPageCount ||
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
  return true;
}
