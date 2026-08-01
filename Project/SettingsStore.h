#pragma once

#include <Arduino.h>
#include <stddef.h>

#include "../ProjectConfig.h"
#include "EepromLayout.h"

constexpr uint8_t PersistentMenuPageCount = 15;
constexpr uint16_t PersistentMenuAllPagesMask =
    static_cast<uint16_t>((1UL << PersistentMenuPageCount) - 1UL);

#if PCCONTROLLER_MENU_ORDERING
// Validates the packed stable-ID permutation and returns its first visible
// page. 0xFF means the mask/order pair is invalid.
uint8_t firstVisiblePersistentMenuPage(uint16_t visibleMask,
                                       const uint8_t *packedOrder);
#endif

enum class MotionDoorPolicy : uint8_t {
  Always = 0,
  ClosedOnly,
  OpenOnly,
  Never,
};

namespace SettingsFlags {
constexpr uint8_t Silent = 1U << 0;
constexpr uint8_t Reserved1 = 1U << 1;
constexpr uint8_t SwapTemperatureSensors = 1U << 2;
constexpr uint8_t MotionDoorPolicyMask = 3U << 3;
constexpr uint8_t MotionDoorPolicyShift = 3;
// Disabled bits are intentionally active-high: legacy EEPROM records have
// them clear, so upgrading preserves the requested default of audible cues.
constexpr uint8_t DoorAudioDisabled = 1U << 5;
constexpr uint8_t RelayAudioDisabled = 1U << 6;
// Clear selects the compact 1 ms default; set selects a conservative 100 ms.
// Direction settling and cross-side interlock timings remain independent.
constexpr uint8_t ExtendedMotionBreak = 1U << 7;
} // namespace SettingsFlags

namespace MenuSettingsFlags {
constexpr uint8_t SaveLastPage = 1U << 0;
constexpr uint8_t StatusColorMask = 0x0EU;
constexpr uint8_t VoltageDecimalsMask = 0x30U;
constexpr uint8_t CurrentDecimalsMask = 0xC0U;
constexpr uint8_t StatusColorShift = 1;
constexpr uint8_t VoltageDecimalsShift = 4;
constexpr uint8_t CurrentDecimalsShift = 6;

// Encoding 0 is reserved for legacy EEPROM records and means two decimals.
inline uint8_t decodeDecimals(uint8_t value) {
  return value == 0 ? 2 : static_cast<uint8_t>(value - 1);
}

inline uint8_t encodeDecimals(uint8_t value) {
  return static_cast<uint8_t>((value > 2 ? 2 : value) + 1);
}
} // namespace MenuSettingsFlags

struct ControllerSettings {
  // Core flags and illumination/display boot values use their native 8-bit units.
  uint8_t flags;
  uint8_t illuminationMode;
  uint8_t illuminationOnBrightness;
  uint8_t illuminationOffBrightness;
  uint8_t displayBrightness;
  uint8_t statusBrightness;
  uint8_t pwmBootMode;
  // Periodic native telemetry interval in milliseconds; zero disables streaming.
  uint16_t streamPeriodMs;
  // Persistent channels 0..7 use 0..255 and expand to 12-bit PWM at runtime.
  uint8_t userPwm[8];
  // Stable boot page ID plus packed save/color/decimal presentation options.
  uint8_t defaultMenuPage;
  uint8_t menuFlags;
#if PCCONTROLLER_MENU_VISIBILITY
  // Stable page IDs remain unchanged; the mask only controls local browsing.
  uint16_t visibleMenuMask;
#endif
#if PCCONTROLLER_MENU_ORDERING
  // Two stable page IDs per byte, low nibble first; rank is the wire order.
  uint8_t menuOrder[8];
#endif

  bool silent() const { return (flags & SettingsFlags::Silent) != 0; }
  void setSilent(bool value) {
    if (value) {
      flags |= SettingsFlags::Silent;
    } else {
      flags &= static_cast<uint8_t>(~SettingsFlags::Silent);
    }
  }
  bool swapTemperatureSensors() const {
    return (flags & SettingsFlags::SwapTemperatureSensors) != 0;
  }
  void setSwapTemperatureSensors(bool value) {
    if (value) {
      flags |= SettingsFlags::SwapTemperatureSensors;
    } else {
      flags &=
          static_cast<uint8_t>(~SettingsFlags::SwapTemperatureSensors);
    }
  }
  MotionDoorPolicy motionDoorPolicy() const {
    return static_cast<MotionDoorPolicy>(
        (flags & SettingsFlags::MotionDoorPolicyMask) >>
        SettingsFlags::MotionDoorPolicyShift);
  }
  void setMotionDoorPolicy(MotionDoorPolicy value) {
    flags = static_cast<uint8_t>(
        (flags & ~SettingsFlags::MotionDoorPolicyMask) |
        (static_cast<uint8_t>(value) <<
         SettingsFlags::MotionDoorPolicyShift));
  }
  bool doorAudioEnabled() const {
    return (flags & SettingsFlags::DoorAudioDisabled) == 0;
  }
  bool relayAudioEnabled() const {
    return (flags & SettingsFlags::RelayAudioDisabled) == 0;
  }
  uint8_t motionBreakBeforeDirectionMs() const {
    return (flags & SettingsFlags::ExtendedMotionBreak) != 0 ? 100 : 1;
  }
  void setExtendedMotionBreak(bool value) {
    if (value) {
      flags |= SettingsFlags::ExtendedMotionBreak;
    } else {
      flags &= static_cast<uint8_t>(~SettingsFlags::ExtendedMotionBreak);
    }
  }
  bool saveLastMenuPage() const {
    return (menuFlags & MenuSettingsFlags::SaveLastPage) != 0;
  }
  void setSaveLastMenuPage(bool value) {
    if (value) {
      menuFlags |= MenuSettingsFlags::SaveLastPage;
    } else {
      menuFlags &=
          static_cast<uint8_t>(~MenuSettingsFlags::SaveLastPage);
    }
  }
  uint8_t statusColor() const {
    return static_cast<uint8_t>(
        (menuFlags & MenuSettingsFlags::StatusColorMask) >>
        MenuSettingsFlags::StatusColorShift);
  }
  void setStatusColor(uint8_t value) {
    menuFlags = static_cast<uint8_t>(
        (menuFlags & ~MenuSettingsFlags::StatusColorMask) |
        ((value & 0x07U) << MenuSettingsFlags::StatusColorShift));
  }
  uint8_t voltageDecimals() const {
    return MenuSettingsFlags::decodeDecimals(static_cast<uint8_t>(
        (menuFlags & MenuSettingsFlags::VoltageDecimalsMask) >>
        MenuSettingsFlags::VoltageDecimalsShift));
  }
  void setVoltageDecimals(uint8_t value) {
    menuFlags = static_cast<uint8_t>(
        (menuFlags & ~MenuSettingsFlags::VoltageDecimalsMask) |
        (MenuSettingsFlags::encodeDecimals(value)
         << MenuSettingsFlags::VoltageDecimalsShift));
  }
  uint8_t currentDecimals() const {
    return MenuSettingsFlags::decodeDecimals(static_cast<uint8_t>(
        (menuFlags & MenuSettingsFlags::CurrentDecimalsMask) >>
        MenuSettingsFlags::CurrentDecimalsShift));
  }
  void setCurrentDecimals(uint8_t value) {
    menuFlags = static_cast<uint8_t>(
        (menuFlags & ~MenuSettingsFlags::CurrentDecimalsMask) |
        (MenuSettingsFlags::encodeDecimals(value)
         << MenuSettingsFlags::CurrentDecimalsShift));
  }
#if PCCONTROLLER_MENU_VISIBILITY
  bool menuPageVisible(uint8_t page) const {
    return page < PersistentMenuPageCount &&
           (visibleMenuMask & static_cast<uint16_t>(1U << page)) != 0;
  }
#endif
#if PCCONTROLLER_MENU_ORDERING
  uint8_t menuPageAtRank(uint8_t rank) const {
    const uint8_t packed = menuOrder[rank >> 1];
    return (rank & 1U) == 0 ? static_cast<uint8_t>(packed & 0x0FU)
                            : static_cast<uint8_t>(packed >> 4);
  }
#endif
};

// The seven leading byte-sized settings are also the native UART settings
// prefix. Keep this explicit so EEPROM and wire compatibility cannot drift.
constexpr uint8_t ControllerSettingsPrefixSize = 7;
static_assert(offsetof(ControllerSettings, streamPeriodMs) ==
                  ControllerSettingsPrefixSize,
              "Controller settings prefix layout changed");
#if PCCONTROLLER_MENU_ORDERING
static_assert(sizeof(ControllerSettings) == 29,
              "Packed menu order changed AVR EEPROM/RAM layout");
#elif PCCONTROLLER_MENU_VISIBILITY
static_assert(sizeof(ControllerSettings) == 21,
              "Visible menu mask changed AVR EEPROM/RAM layout");
#else
static_assert(sizeof(ControllerSettings) == 19,
              "Controller settings AVR layout changed");
#endif

// Owns the MCU EEPROM settings record, defaults, checksum, and delayed writes.
class SettingsStore {
public:
  static constexpr int EepromAddress = EepromLayout::SettingsAddress;
  static constexpr uint16_t SaveDelayMs = 1500;

  // Loads a valid EEPROM record or installs development defaults in RAM.
  bool begin(uint32_t now = millis());
  // Returns the single MCU-owned live settings record.
  ControllerSettings &values();
  const ControllerSettings &values() const;

  // Coalesces edits before a delayed EEPROM write to reduce wear.
  void markDirty(uint32_t now = millis());
  bool service(uint32_t now = millis(), bool allowWrite = true);
  // Writes the checksum-backed record immediately with EEPROM.update wear reduction.
  bool saveNow();
  bool dirty() const;

private:
  void setDefaults();
  bool loadCurrent();

  ControllerSettings settings_{}; // Live MCU settings; never host-config storage.
  uint32_t changedAt_ = 0;
  bool dirty_ = false;
};

extern SettingsStore settingsStore;
