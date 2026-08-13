#pragma once

#include <stdint.h>

#include "../LocalLib/Keys.h"
#include "../ProjectConfig.h"

// Physical, RF, and host keys share these stable four action IDs.
enum MenuAction : uint8_t {
  MENU_PREVIOUS = 0,
  MENU_NEXT = 1,
  MENU_DECREASE = 2,
  MENU_INCREASE = 3
};

// Stable EEPROM/protocol page IDs; presentation order is stored separately.
enum MenuPage : uint8_t {
  PAGE_DOOR = 0,
  PAGE_VOLTAGE,
  PAGE_CURRENT,
  PAGE_TLED,
  PAGE_TBT,
  PAGE_ILLUMINATION,
  PAGE_SOUND,
  PAGE_PWM,
  PAGE_RELAY,
  PAGE_KEYS,
  PAGE_USER_PWM,
  PAGE_USER_RELAYS,
  PAGE_MOTION,
  PAGE_RF,
  PAGE_COUNT
};

// Alpha aliases collapse retired duplicate pages into one relay controller,
// one PWM controller, and one Key/motion controller. Only the canonical page
// is navigable; host tooling may still normalize an old numeric page ID.
constexpr uint8_t canonicalFrontPanelPage(uint8_t page) {
  return page == PAGE_USER_RELAYS
             ? static_cast<uint8_t>(PAGE_RELAY)
             : (page == PAGE_USER_PWM
                    ? static_cast<uint8_t>(PAGE_PWM)
                    : (page == PAGE_MOTION
                           ? static_cast<uint8_t>(PAGE_KEYS)
                           : page));
}

constexpr bool frontPanelPageCompiled(uint8_t page) {
  if (page >= PAGE_COUNT || page == PAGE_USER_PWM ||
      page == PAGE_USER_RELAYS || page == PAGE_MOTION) {
    return false;
  }
#if !PCCONTROLLER_ENABLE_INA219
  if (page == PAGE_VOLTAGE || page == PAGE_CURRENT) {
    return false;
  }
#endif
#if !PCCONTROLLER_ENABLE_DS18B20
  if (page == PAGE_TLED || page == PAGE_TBT) {
    return false;
  }
#endif
#if !PCCONTROLLER_ENABLE_PCA9685
  if (page == PAGE_ILLUMINATION || page == PAGE_PWM) {
    return false;
  }
#else
#if !PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES
  if (page == PAGE_PWM) {
    return false;
  }
#endif
#if !PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION
  if (page == PAGE_ILLUMINATION) {
    return false;
  }
#endif
#endif
#if !PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI
  if (page == PAGE_RF) {
    return false;
  }
#endif
  return true;
}

// Top-level pages and modal editors consumed by ModeManager.
enum ProgramMode : uint8_t {
  MODE_BOOT = 0,
  MODE_DOOR,
  MODE_VOLTAGE,
  MODE_CURRENT,
  MODE_TLED,
  MODE_TBT,
  MODE_ILLUMINATION,
  MODE_SOUND,
  MODE_PWM,
  MODE_RELAY,
  MODE_KEYS,
  MODE_USER_PWM,
  MODE_USER_RELAYS,
  MODE_MOTION,
  MODE_RF,
  MODE_ILLUMINATION_MODE_EDIT,
  MODE_ILLUMINATION_ON_EDIT,
  MODE_ILLUMINATION_OFF_EDIT,
  MODE_SOUND_EDIT,
  MODE_PWM_CHANNEL_EDIT,
  MODE_PWM_VALUE_EDIT,
  MODE_RELAY_CHANNEL_EDIT,
  MODE_RELAY_VALUE_EDIT,
  MODE_USER_PWM_CHANNEL_EDIT,
  MODE_USER_PWM_VALUE_EDIT,
  MODE_USER_RELAY_CHANNEL_EDIT,
  MODE_USER_RELAY_BEHAVIOR_EDIT,
  MODE_USER_RELAY_CONTROL,
  MODE_MOTION_CONTROL,
  MODE_SAVE_PROMPT,
  MODE_FLASH_MESSAGE,
  MODE_RF_LEARNING,
  MODE_FAULT,
  MODE_UNDEFINED = 0xFF
};

enum class LeafDecreaseAction : uint8_t {
  ParentCategory,
  AllRelaysOff,
};

// Rolls any zero-based modal selector in both directions. Relay number,
// illumination mode, and optional PWM channel editors share this endpoint
// contract so no profile can trap K3/K4 at its first or last value.
constexpr uint8_t rollMenuIndex(uint8_t value, uint8_t count, bool increase) {
  if (count == 0) return 0;
  value = value < count ? value : static_cast<uint8_t>(count - 1U);
  return increase
             ? static_cast<uint8_t>(value + 1U == count ? 0 : value + 1U)
             : static_cast<uint8_t>(value == 0 ? count - 1U : value - 1U);
}

constexpr LeafDecreaseAction leafDecreaseAction(ProgramMode mode) {
  return mode == MODE_RELAY ? LeafDecreaseAction::AllRelaysOff
                            : LeafDecreaseAction::ParentCategory;
}

// UnifiedInputIntent is the complete four-key policy for the single input
// page. Normal builds map all four keys to A/B Up/Down; diagnostics retain
// direct navigation plus key identification.
enum class UnifiedInputIntent : uint8_t {
  PreviousPage,
  NextPage,
  Identify,
  Macro,
  Motion,
};

constexpr UnifiedInputIntent unifiedInputIntent(MenuAction action,
                                                 bool identifyKeys) {
  if (!identifyKeys) return UnifiedInputIntent::Motion;
  if (action == MENU_PREVIOUS) return UnifiedInputIntent::PreviousPage;
  if (action == MENU_NEXT) return UnifiedInputIntent::NextPage;
  return UnifiedInputIntent::Identify;
}

struct MotionKeyBinding {
  uint8_t side;
  bool reverse;
};

// MotionPresentation is derived from the electrically applied relay mask, so
// the TM1637 reports the same side/direction for physical, RF, host, and macro
// commands. When both sides run, alternateSide selects the one to display.
struct MotionPresentation {
  uint8_t side;
  bool reverse;
  bool active;
};

constexpr MotionPresentation motionPresentation(uint8_t relayMask,
                                                  bool alternateSide) {
  const bool sideA = (relayMask & (1U << 1)) != 0;
  const bool sideB = (relayMask & (1U << 3)) != 0;
  if (!sideA && !sideB) return {0, false, false};
  const uint8_t side = sideA && sideB
                           ? static_cast<uint8_t>(alternateSide)
                           : static_cast<uint8_t>(sideB);
  return {side,
          (relayMask & (1U << static_cast<uint8_t>(side * 2U))) != 0,
          true};
}

constexpr MotionKeyBinding motionKeyBinding(MenuAction action) {
  return {static_cast<uint8_t>(action >= MENU_DECREASE),
          action == MENU_NEXT || action == MENU_INCREASE};
}

// Opposing directions on one side form the no-twitch exit chord.  It uses the
// current active snapshot, not a historical commanded mask.
constexpr bool motionKeyCompletesExitChord(uint8_t activeSnapshot,
                                            uint8_t action) {
  return action <= MENU_INCREASE &&
         (activeSnapshot & static_cast<uint8_t>(
                               1U << (static_cast<uint8_t>(action) ^ 1U))) != 0;
}
