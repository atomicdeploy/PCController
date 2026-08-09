#pragma once

#include <Arduino.h>
#include <stddef.h>

#include "../ProjectConfig.h"
#include "EepromLayout.h"
#include "MotionDoorPolicy.h"

// Persistent built-in menu geometry; stable page IDs are packed two per byte.
constexpr uint8_t PersistentMenuPageCount = 14;
constexpr uint8_t PersistentMenuOrderWireBytes =
    (PersistentMenuPageCount + 1U) / 2U;
constexpr uint16_t PersistentMenuAllPagesMask =
    static_cast<uint16_t>((1UL << PersistentMenuPageCount) - 1UL);

#if PCCONTROLLER_MENU_ORDERING
// Validates the packed stable-ID permutation and returns its first visible
// page. 0xFF means the mask/order pair is invalid.
uint8_t firstVisiblePersistentMenuPage(uint16_t visibleMask,
                                       const uint8_t *packedOrder);
#endif

namespace SettingsFlags {
constexpr uint8_t Silent = 1U << 0;
// Host-managed programming latch: survives every reset in the current EEPROM
// record without changing the user's persistent Silent preference.
constexpr uint8_t ProgrammingMode = 1U << 1;
constexpr uint8_t SwapTemperatureSensors = 1U << 2;
constexpr uint8_t MotionDoorPolicyMask = 3U << 3;
constexpr uint8_t MotionDoorPolicyShift = 3;
// Disable bits keep the zero-valued factory representation audible while
// still allowing each cue family to be independently muted.
constexpr uint8_t DoorAudioDisabled = 1U << 5;
constexpr uint8_t RelayAudioDisabled = 1U << 6;
} // namespace SettingsFlags

namespace MenuSettingsFlags {
constexpr uint8_t SaveLastPage = 1U << 0;
constexpr uint8_t StatusColorMask = 0x0EU;
constexpr uint8_t VoltageDecimalsMask = 0x30U;
constexpr uint8_t CurrentDecimalsMask = 0xC0U;
constexpr uint8_t StatusColorShift = 1;
constexpr uint8_t VoltageDecimalsShift = 4;
constexpr uint8_t CurrentDecimalsShift = 6;

// The compact mapping is 0->2, 1->0, 2->1, 3->2 decimal places. Mapping the
// zero-filled representation to two decimals gives useful factory output.
inline uint8_t decodeDecimals(uint8_t value) {
  return value == 0 ? 2 : static_cast<uint8_t>(value - 1);
}

inline uint8_t encodeDecimals(uint8_t value) {
  return static_cast<uint8_t>((value > 2 ? 2 : value) + 1);
}
} // namespace MenuSettingsFlags

namespace DisplayOptions {
constexpr uint8_t ClosedBrightnessMask = 0x07U;
constexpr uint8_t MotionExitSecondsShift = 3;
constexpr uint8_t DefaultMotionExitSeconds = 2;
} // namespace DisplayOptions

namespace OutputPersistence {
constexpr uint8_t RememberMotion = 1U << 0;
constexpr uint8_t RememberUserRelays = 1U << 1;
constexpr uint8_t RememberUserPwm = 1U << 2;
constexpr uint8_t RetainDirectionOnStop = 1U << 3;
constexpr uint8_t AllowedMask = RememberMotion | RememberUserRelays |
                                RememberUserPwm | RetainDirectionOnStop;
constexpr uint8_t Defaults = RememberUserRelays | RememberUserPwm;
} // namespace OutputPersistence

// ControllerSettings is the packed MCU-owned EEPROM and native settings record.
struct ControllerSettings {
  // Core flags and illumination/display boot values use their native 8-bit units.
  uint8_t flags;
  uint8_t illuminationMode;
  uint8_t illuminationOnBrightness;
  uint8_t illuminationOffBrightness;
  uint8_t displayBrightness;
  uint8_t statusBrightness;
  // Independent restore/stop policies replace the removed PWM demo mode.
  uint8_t outputPersistence;
  // Periodic native telemetry interval in milliseconds; zero disables streaming.
  uint16_t streamPeriodMs;
  // Persistent channels 0..7 use 0..255 and expand to 12-bit PWM at runtime.
  uint8_t userPwm[8];
  // Stable boot page ID plus packed save/color/decimal presentation options.
  uint8_t defaultMenuPage;
  uint8_t menuFlags;
#if PCCONTROLLER_MENU_LAYOUT_STORAGE
  // Stable page IDs remain unchanged; the mask only controls local browsing.
  uint16_t visibleMenuMask;
  // Two stable page IDs per byte, low nibble first; rank is the wire order.
  uint8_t menuOrder[PersistentMenuOrderWireBytes];
#endif
  // Low three bits are the closed-door TM1637 target; high five bits persist
  // the motion-page two-key exit hold in seconds (zero encodes the 2 s default).
  uint8_t displayOptions;
  // Last applied R1..R8 mask; restore flags decide which domains may use it.
  uint8_t relayRestoreMask;
  // Exact 1..255 ms disable-to-direction interval; factory value is 1 ms.
  uint8_t motionBreakMs;

  bool silent() const { return (flags & SettingsFlags::Silent) != 0; }
  void setSilent(bool value) {
    if (value) {
      flags |= SettingsFlags::Silent;
    } else {
      flags &= static_cast<uint8_t>(~SettingsFlags::Silent);
    }
  }
  bool programmingMode() const {
    return (flags & SettingsFlags::ProgrammingMode) != 0;
  }
  uint8_t displayClosedBrightness() const {
    return displayOptions & DisplayOptions::ClosedBrightnessMask;
  }
  void setDisplayClosedBrightness(uint8_t value) {
    displayOptions = static_cast<uint8_t>(
        (displayOptions & ~DisplayOptions::ClosedBrightnessMask) |
        (value & DisplayOptions::ClosedBrightnessMask));
  }
  uint8_t motionExitHoldSeconds() const {
    const uint8_t encoded = displayOptions >>
                            DisplayOptions::MotionExitSecondsShift;
    return encoded == 0 ? DisplayOptions::DefaultMotionExitSeconds : encoded;
  }
  bool rememberMotion() const {
    return (outputPersistence & OutputPersistence::RememberMotion) != 0;
  }
  bool rememberUserRelays() const {
    return (outputPersistence & OutputPersistence::RememberUserRelays) != 0;
  }
  bool rememberUserPwm() const {
    return (outputPersistence & OutputPersistence::RememberUserPwm) != 0;
  }
  bool retainDirectionOnStop() const {
    return (outputPersistence & OutputPersistence::RetainDirectionOnStop) != 0;
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
  // Rolls the two-bit policy without disturbing adjacent audio/settings flags.
  void adjustMotionDoorPolicy(bool increase) {
    flags = static_cast<uint8_t>(
        (flags & ~SettingsFlags::MotionDoorPolicyMask) |
        ((flags +
          (increase ? (1U << SettingsFlags::MotionDoorPolicyShift)
                    : (3U << SettingsFlags::MotionDoorPolicyShift))) &
         SettingsFlags::MotionDoorPolicyMask));
  }
  bool doorAudioEnabled() const {
    return (flags & SettingsFlags::DoorAudioDisabled) == 0;
  }
  bool relayAudioEnabled() const {
    return (flags & SettingsFlags::RelayAudioDisabled) == 0;
  }
  uint8_t motionBreakBeforeDirectionMs() const {
    return motionBreakMs;
  }
  void setMotionBreakBeforeDirectionMs(uint8_t value) {
    motionBreakMs = value;
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

// Explicit build contract for the checksum-backed EEPROM settings/name record.
// Schema IDs describe feature layouts, not a compatibility/migration chain:
// alpha builds intentionally reject a record whose exact shape/CRC differs.
namespace SettingsRecordLayout {
constexpr uint8_t CoreSchema = 1;
constexpr uint8_t MenuLayoutSchema = 2;
#if PCCONTROLLER_MENU_LAYOUT_STORAGE
constexpr uint8_t Schema = MenuLayoutSchema;
constexpr uint8_t ControllerBytes = 31;
#else
constexpr uint8_t Schema = CoreSchema;
constexpr uint8_t ControllerBytes = 22;
#endif
constexpr uint8_t BoardNameLengthBytes = 1;
constexpr uint8_t BoardNameBytes = 8;
constexpr uint8_t ChecksumBytes = 1;
constexpr uint8_t RecordBytes = ControllerBytes + BoardNameLengthBytes +
                                BoardNameBytes + ChecksumBytes;
} // namespace SettingsRecordLayout

// The seven leading byte-sized settings form the canonical UART settings
// prefix. Keep the shared semantic prefix explicit while allowing later
// payloads to append independently discoverable fields.
constexpr uint8_t ControllerSettingsPrefixSize = 7;
#if defined(__AVR__)
// AVR byte alignment defines the EEPROM/wire record. Desktop test compilers
// may align uint16_t differently, so production-layout assertions belong to
// the target that serializes this structure.
static_assert(offsetof(ControllerSettings, streamPeriodMs) ==
                  ControllerSettingsPrefixSize,
              "Controller settings prefix layout changed");
#if PCCONTROLLER_MENU_LAYOUT_STORAGE
static_assert(sizeof(ControllerSettings) == SettingsRecordLayout::ControllerBytes,
              "Host-owned packed menu storage changed AVR EEPROM/RAM layout");
#else
static_assert(sizeof(ControllerSettings) == SettingsRecordLayout::ControllerBytes,
              "Controller settings AVR layout changed");
#endif
#endif

// Owns the MCU EEPROM settings record, defaults, checksum, and delayed writes.
class SettingsStore {
public:
  static constexpr int EepromAddress = EepromLayout::SettingsAddress;
  static constexpr uint16_t SaveDelayMs = 1500;
  static constexpr uint8_t MaximumBoardNameLength = 8;

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
  bool persisted() const;

  // Reads the compact length/name record into the SETTINGS response tail.
  bool boardName(uint8_t *record) const;
  bool setBoardName(const uint8_t *name, uint8_t length);

private:
  void setDefaults();
  bool loadCurrent();

  ControllerSettings settings_{}; // Live MCU settings; never host-config storage.
  uint8_t boardNameLength_ = 0;
  uint8_t boardName_[MaximumBoardNameLength]{};
  uint32_t changedAt_ = 0;
  bool dirty_ = false;
  bool persisted_ = false;
};

// settingsStore is the single MCU EEPROM settings owner.
extern SettingsStore settingsStore;
