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
// PAGE_MOTION remains a wire-compatible selector for pre-unified clients. It
// is never a separately browsable local page: canonicalMenuPage() resolves it
// to the unified KEY motion surface.
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

constexpr uint8_t canonicalMenuPage(uint8_t page) {
  return page == PAGE_MOTION ? static_cast<uint8_t>(PAGE_KEYS) : page;
}

constexpr bool retiredMenuPageAlias(uint8_t page) {
  return page == PAGE_MOTION;
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
  IdentifyKey3,
  AllRelaysOff,
};

constexpr LeafDecreaseAction leafDecreaseAction(ProgramMode mode) {
  return mode == MODE_KEYS
             ? LeafDecreaseAction::IdentifyKey3
             : (mode == MODE_RELAY ? LeafDecreaseAction::AllRelaysOff
                                   : LeafDecreaseAction::ParentCategory);
}
