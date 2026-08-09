#pragma once

#include <stdint.h>

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

// PAGE_KEYS remains a stable protocol/EEPROM ID for alpha tooling, but the MCU
// presents exactly one input page. Requests for the retired duplicate page are
// canonicalized to PAGE_MOTION and navigation skips it in every build profile.
constexpr uint8_t canonicalFrontPanelPage(uint8_t page) {
  return page == PAGE_KEYS ? static_cast<uint8_t>(PAGE_MOTION) : page;
}

constexpr bool frontPanelPageCompiled(uint8_t page) {
  return page < PAGE_COUNT && page != PAGE_KEYS;
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

// One policy describes whether K3 navigates or invokes a leaf-owned action.
enum class LeafDecreaseAction : uint8_t {
  ParentCategory,
  AllRelaysOff,
};

constexpr LeafDecreaseAction leafDecreaseAction(ProgramMode mode) {
  return mode == MODE_RELAY ? LeafDecreaseAction::AllRelaysOff
                            : LeafDecreaseAction::ParentCategory;
}

// UnifiedInputIntent is the complete four-key policy for the single input page.
// Diagnostics keep two direct exits, so key identification can never trap the
// operator. The normal image assigns K3 to record/stop/play and K4 to motion.
enum class UnifiedInputIntent : uint8_t {
  PreviousPage,
  NextPage,
  Identify,
  Macro,
  Motion,
};

constexpr UnifiedInputIntent unifiedInputIntent(MenuAction action,
                                                 bool identifyKeys) {
  if (action == MENU_PREVIOUS) {
    return UnifiedInputIntent::PreviousPage;
  }
  if (action == MENU_NEXT) {
    return UnifiedInputIntent::NextPage;
  }
  if (identifyKeys) {
    return UnifiedInputIntent::Identify;
  }
  return action == MENU_DECREASE ? UnifiedInputIntent::Macro
                                 : UnifiedInputIntent::Motion;
}

// One immutable mapping is consumed by physical, injected, and native tests.
struct MotionKeyBinding {
  uint8_t side;
  bool reverse;
};

constexpr MotionKeyBinding motionKeyBinding(MenuAction action) {
  return {static_cast<uint8_t>(action >= MENU_DECREASE),
          action == MENU_NEXT || action == MENU_INCREASE};
}
