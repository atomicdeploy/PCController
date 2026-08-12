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

// PAGE_KEYS remains a stable protocol/EEPROM ID, but the AVR only presents a
// single input page.  This prevents a key-ID page from trapping an operator or
// spending bytes in the normal motion build.
constexpr uint8_t canonicalFrontPanelPage(uint8_t page) {
  return page == PAGE_KEYS ? static_cast<uint8_t>(PAGE_MOTION) : page;
}

constexpr bool frontPanelPageCompiled(uint8_t page) {
  if (page >= PAGE_COUNT || page == PAGE_KEYS) {
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
#if !PCCONTROLLER_ENABLE_PCA9685 || !PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES
  if (page == PAGE_ILLUMINATION || page == PAGE_PWM || page == PAGE_USER_PWM) {
    return false;
  }
#elif !PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION
  if (page == PAGE_ILLUMINATION) {
    return false;
  }
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

constexpr LeafDecreaseAction leafDecreaseAction(ProgramMode mode) {
  return mode == MODE_RELAY ? LeafDecreaseAction::AllRelaysOff
                            : LeafDecreaseAction::ParentCategory;
}

// UnifiedInputIntent is the complete four-key policy for the single input
// page.  In normal builds K3 handles capture/replay and K4 enters motion; in
// diagnostics both leaf keys identify inputs and K1/K2 always exit.
enum class UnifiedInputIntent : uint8_t {
  PreviousPage,
  NextPage,
  Identify,
  Macro,
  Motion,
};

constexpr UnifiedInputIntent unifiedInputIntent(MenuAction action,
                                                 bool identifyKeys) {
  if (action == MENU_PREVIOUS) return UnifiedInputIntent::PreviousPage;
  if (action == MENU_NEXT) return UnifiedInputIntent::NextPage;
  if (identifyKeys) return UnifiedInputIntent::Identify;
  return action == MENU_DECREASE ? UnifiedInputIntent::Macro
                                 : UnifiedInputIntent::Motion;
}

enum class UnifiedMacroGesture : uint8_t {
  None,
  ImmediateCapture,
  Replay,
  ReplaceCapture,
  SuppressClassification,
};

constexpr UnifiedMacroGesture unifiedMacroGesture(KeyEvent event,
                                                   bool hasCapture,
                                                   bool suppressClassification) {
  if (event == KeyEvent::Down && !hasCapture) {
    return UnifiedMacroGesture::ImmediateCapture;
  }
  const bool classified = event == KeyEvent::Click ||
                          event == KeyEvent::DoubleClick ||
                          event == KeyEvent::HoldStart;
  if (classified && suppressClassification) {
    return UnifiedMacroGesture::SuppressClassification;
  }
  if (!hasCapture) return UnifiedMacroGesture::None;
  if (event == KeyEvent::Click || event == KeyEvent::DoubleClick) {
    return UnifiedMacroGesture::Replay;
  }
  return event == KeyEvent::HoldStart ? UnifiedMacroGesture::ReplaceCapture
                                      : UnifiedMacroGesture::None;
}

struct MotionKeyBinding {
  uint8_t side;
  bool reverse;
};

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
