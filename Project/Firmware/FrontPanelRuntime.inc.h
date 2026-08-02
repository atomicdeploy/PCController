// Implementation fragment compiled once; owns menus, keys, display, and feedback.
// -----------------------------------------------------------------------------
// Menu, keys, display, and buzzer
// -----------------------------------------------------------------------------

// Shows the active page's packed four-character label for a short dwell.
void showMenuLabel(uint32_t now) {
  display.showText(reinterpret_cast<const __FlashStringHelper *>(
      MenuLabels + static_cast<uint8_t>(menuPage << 2)));
  menuLabelEndsAt = now + 650;
}

// Shows the selected settings field label before rendering its value.
void showSettingsLabel(uint32_t now) {
  display.showText(reinterpret_cast<const __FlashStringHelper *>(
      SettingsLabels + static_cast<uint8_t>(settingsMenuItem << 2)));
  menuLabelEndsAt = now + 650;
}

// Distinguishes ordinary page modes from modal editors and transient states.
bool isMenuMode(ProgramMode mode) {
  return mode >= MODE_DOOR && mode <= MODE_RF;
}

// Converts a stable page ID to its contiguous top-level program mode.
ProgramMode pageToMode(uint8_t page) {
  return static_cast<ProgramMode>(
      static_cast<uint8_t>(MODE_DOOR) + page);
}

// Returns the stable page represented by an ordinary mode.
uint8_t modeToPage(ProgramMode mode) {
  return isMenuMode(mode)
             ? static_cast<uint8_t>(mode) -
                   static_cast<uint8_t>(MODE_DOOR)
             : menuPage;
}

#if PCCONTROLLER_MENU_VISIBILITY
// Reads the stable page ID stored at a presentation rank.
uint8_t configuredMenuPageAt(uint8_t rank) {
#if PCCONTROLLER_MENU_ORDERING
  return settingsStore.values().menuPageAtRank(rank);
#else
  return rank;
#endif
}

// Finds a stable page's current presentation rank.
uint8_t configuredMenuRank(uint8_t page) {
#if PCCONTROLLER_MENU_ORDERING
  for (uint8_t rank = 0; rank < PAGE_COUNT; ++rank) {
    if (configuredMenuPageAt(rank) == page) {
      return rank;
    }
  }
#endif
  return page;
}

#if PCCONTROLLER_MENU_HIERARCHY
constexpr uint8_t MenuCategoryCount = 4;
constexpr uint8_t MenuCategorySelector = 0x80;

// Maps each stable page into Monitoring, Environment, Outputs, or Inputs/RF.
uint8_t menuCategory(uint8_t page) {
  if (page <= PAGE_TBT) {
    return 0; // Monitoring: Status, voltage, current, tLED, tBT.
  }
  if (page <= PAGE_SOUND) {
    return 1; // Environment: illumination and sound.
  }
  return page == PAGE_KEYS || page == PAGE_RF
             ? 3 // Inputs/RF: physical keys and learned RF.
             : 2; // Outputs: PWM, relays, user outputs, motion.
}

// Tests whether navigation is currently at a category parent node.
bool menuCategorySelected() {
  return (menuTreeState & MenuCategorySelector) != 0;
}
#endif

// Finds the next visible page in configured order, optionally within a category.
uint8_t nextConfiguredMenuPage(uint8_t page, bool forward,
                               uint8_t category = 0xFF) {
  uint8_t rank = configuredMenuRank(page);
  for (uint8_t checked = 0; checked < PAGE_COUNT; ++checked) {
    rank = static_cast<uint8_t>(
        rank + (forward ? 1U : PAGE_COUNT - 1U));
    if (rank >= PAGE_COUNT) {
      rank = static_cast<uint8_t>(rank - PAGE_COUNT);
    }
    const uint8_t candidate = configuredMenuPageAt(rank);
    if (!settingsStore.values().menuPageVisible(candidate)) {
      continue;
    }
#if PCCONTROLLER_MENU_HIERARCHY
    if (category < MenuCategoryCount && menuCategory(candidate) != category) {
      continue;
    }
#else
    (void)category;
#endif
    return candidate;
  }
  // A valid layout always revisits the current visible page by the final pass.
  return page;
}

#if PCCONTROLLER_MENU_HIERARCHY
// Returns the first visible child in a category, or 0xFF for an empty category.
uint8_t firstConfiguredMenuPage(uint8_t category) {
  for (uint8_t rank = 0; rank < PAGE_COUNT; ++rank) {
    const uint8_t page = configuredMenuPageAt(rank);
    if (settingsStore.values().menuPageVisible(page) &&
        menuCategory(page) == category) {
      return page;
    }
  }
  return 0xFF;
}

// Renders the representative four-character label for the selected category.
void showMenuCategory(uint32_t now) {
  static const uint8_t labelPages[] PROGMEM = {
      PAGE_DOOR, PAGE_ILLUMINATION, PAGE_PWM, PAGE_KEYS};
  const uint8_t category = menuTreeState & 0x03U;
  const uint8_t labelPage = pgm_read_byte(labelPages + category);
  display.showText(reinterpret_cast<const __FlashStringHelper *>(
      MenuLabels + static_cast<uint8_t>(labelPage << 2)));
  menuLabelEndsAt = now + 650;
}

// Rolls category selection while skipping categories with no visible children.
void moveMenuCategory(bool forward, uint32_t now) {
  uint8_t category = menuTreeState & 0x03U;
  for (uint8_t checked = 0; checked < MenuCategoryCount; ++checked) {
    category = forward
                   ? static_cast<uint8_t>((category + 1U) % MenuCategoryCount)
                   : (category == 0 ? MenuCategoryCount - 1U : category - 1U);
    if (firstConfiguredMenuPage(category) != 0xFF) {
      menuTreeState = static_cast<uint8_t>(MenuCategorySelector | category);
      showMenuCategory(now);
      return;
    }
  }
}
#endif
#endif

// Activates a stable page and optionally persists it as the boot default.
void setMenuPage(uint8_t page) {
  menuPage = page;
#if PCCONTROLLER_MENU_HIERARCHY
  menuTreeState = menuCategory(menuPage);
#endif
  ControllerSettings &settings = settingsStore.values();
  if (settings.saveLastMenuPage() &&
#if PCCONTROLLER_MENU_VISIBILITY
      settings.menuPageVisible(menuPage) &&
#endif
      settings.defaultMenuPage != menuPage) {
    settings.defaultMenuPage = menuPage;
    settingsStore.markDirty(now);
  }
  modeManager.transitionTo(pageToMode(menuPage));
}

// Runs mode entry actions and time-based exits for transient/modal states.
void programService(uint32_t now) {
  ProgramMode previous;
  ProgramMode current;
  if (modeManager.consumeTransition(previous, current)) {
    (void)previous;
    modeEnteredAt = now;

    if (isMenuMode(current)) {
      menuPage = modeToPage(current);
      showMenuLabel(now);
      return;
    }

    if (current == MODE_SOUND_EDIT) {
      showSettingsLabel(now);
      return;
    }

    const uint8_t editLabel =
        static_cast<uint8_t>(current) -
        static_cast<uint8_t>(MODE_ILLUMINATION_MODE_EDIT);
    if (editLabel < EditLabelCount) {
      display.showText(reinterpret_cast<const __FlashStringHelper *>(
          EditLabels + static_cast<uint8_t>(editLabel << 2)));
      menuLabelEndsAt = now + 650;
      return;
    }

    switch (current) {
      case MODE_BOOT:
        display.showText(commonText(settingsStore.values().programmingMode()
                                        ? TextProgram
                                        : TextBoot));
        break;
      case MODE_USER_RELAY_CONTROL:
        display.showText(commonText(TextUserRelays));
        menuLabelEndsAt = now + 450;
        break;
      case MODE_MOTION_CONTROL:
        relays.allOff(now);
        display.showText(commonText(TextGo));
        menuLabelEndsAt = now + 450;
        break;
      case MODE_SAVE_PROMPT:
        display.showText(commonText(TextSave));
        break;
      case MODE_FLASH_MESSAGE:
        break;
      case MODE_RF_LEARNING:
        display.showText(commonText(TextLearn));
        break;
      case MODE_FAULT:
        display.showText(commonText(TextError));
        buzzer.error();
        break;
      default:
        break;
    }
    return;
  }

  switch (modeManager.current()) {
    case MODE_BOOT:
      if (!settingsStore.values().programmingMode() &&
          static_cast<uint32_t>(now - modeEnteredAt) >= 650) {
        setMenuPage(settingsStore.values().defaultMenuPage);
      }
      break;

    case MODE_RF_LEARNING:
      if (!learningActive) {
        modeManager.transitionTo(modeBeforeLearning);
      } else {
        serviceLearningTimer(now);
      }
      break;

    case MODE_MOTION_CONTROL:
      if (!relays.motionAllowed()) {
        relays.allOff(now);
        modeManager.transitionTo(MODE_MOTION);
      }
      break;

    case MODE_FLASH_MESSAGE:
      if (timeReached(now, flashMessageEndsAt)) {
        modeManager.transitionTo(editReturnMode);
      }
      break;

    case MODE_FAULT:
    case MODE_DOOR:
    case MODE_VOLTAGE:
    case MODE_CURRENT:
    case MODE_TLED:
    case MODE_TBT:
    case MODE_ILLUMINATION:
    case MODE_SOUND:
    case MODE_PWM:
    case MODE_RELAY:
    case MODE_KEYS:
    case MODE_USER_PWM:
    case MODE_USER_RELAYS:
    case MODE_MOTION:
    case MODE_RF:
    case MODE_ILLUMINATION_MODE_EDIT:
    case MODE_ILLUMINATION_ON_EDIT:
    case MODE_ILLUMINATION_OFF_EDIT:
    case MODE_SOUND_EDIT:
    case MODE_PWM_CHANNEL_EDIT:
    case MODE_PWM_VALUE_EDIT:
    case MODE_RELAY_CHANNEL_EDIT:
    case MODE_RELAY_VALUE_EDIT:
    case MODE_USER_PWM_CHANNEL_EDIT:
    case MODE_USER_PWM_VALUE_EDIT:
    case MODE_USER_RELAY_CHANNEL_EDIT:
    case MODE_USER_RELAY_BEHAVIOR_EDIT:
    case MODE_USER_RELAY_CONTROL:
    case MODE_SAVE_PROMPT:
    case MODE_UNDEFINED:
      break;
  }
}

// Emits one canonical audio acknowledgement for physical, RF, and host input.
void menuFeedback(bool fromRemote) {
  statusLeds.playCue(fromRemote ? StatusLedCue::Radio
                                : StatusLedCue::Menu,
                     260);
  buzzer.beep();
}

// Rolls an 8-bit brightness by the configured front-panel step.
uint8_t adjustedBrightness(uint8_t value, bool increase) {
  return TransitionMath::rollByte(value, ILLUMINATION_MENU_STEP, increase);
}

// Rolls Off/Auto/On and mirrors the result to the edit transaction.
void adjustIlluminationMode(bool increase, uint32_t now) {
  int8_t mode = static_cast<int8_t>(illumination.mode());
  mode += increase ? 1 : -1;
  if (mode < static_cast<int8_t>(IlluminationMode::Off)) {
    mode = static_cast<int8_t>(IlluminationMode::On);
  } else if (mode > static_cast<int8_t>(IlluminationMode::On)) {
    mode = static_cast<int8_t>(IlluminationMode::Off);
  }
  illumination.setMode(static_cast<IlluminationMode>(mode));
  markIlluminationSettingsChanged(now);
}

// Tests the currently selected R1..R8 relay against the live output mask.
bool selectedRelayActive() {
  return (relays.activeRelayMask() & _BV(relayMenuIndex)) != 0;
}

// Applies the selected R1..R8 test relay through safety sequencing.
void setSelectedRelay(bool active, uint32_t now) {
  relays.requestRelayForTest(static_cast<uint8_t>(relayMenuIndex + 1), active,
                             now);
}

// Tests the selected general-purpose relay R5..R8.
bool selectedUserRelayActive() {
  return (relays.activeRelayMask() &
          _BV(static_cast<uint8_t>(userRelayMenuIndex + 4))) != 0;
}

// Applies the selected general-purpose relay R5..R8.
void setSelectedUserRelay(bool active, uint32_t now) {
  relays.requestRelayForTest(
      static_cast<uint8_t>(userRelayMenuIndex + 5), active, now);
}

// Updates EEPROM-owned silent state and the live Timer1 tone player.
void setSilentMode(bool silent, uint32_t now) {
  settingsStore.values().setSilent(silent);
  if (!editTransactionActive) {
    settingsStore.markDirty(now);
  }
  buzzer.setMuted(silent);
}

// Expands an EEPROM 8-bit user value over the full 12-bit PWM range.
uint16_t userPwm12(uint8_t value) {
  return static_cast<uint16_t>(
      static_cast<uint16_t>(value) * 16U + value / 16U);
}

// Applies all persisted live settings without changing their EEPROM ownership.
void applyStoredSettings(uint32_t now) {
  const ControllerSettings &settings = settingsStore.values();
  streamPeriodMs = settings.streamPeriodMs;
  relays.setBreakBeforeDirectionMs(settings.motionBreakBeforeDirectionMs());
  relays.setRetainDirectionOnStop(settings.retainDirectionOnStop());
  if (settings.programmingMode()) {
    buzzer.stop();
    buzzer.setMuted(true);
    relays.setMotionAllowed(false, now);
    relays.allOff(now);
    pwm.tryAllOff();
    display.setBrightness(settings.displayBrightness);
    display.showText(commonText(TextProgram));
    modeManager.transitionTo(MODE_BOOT);
    return;
  }
  buzzer.setMuted(settings.silent());
  illumination.setMode(
      static_cast<IlluminationMode>(settings.illuminationMode));
  illumination.setOnBrightness(settings.illuminationOnBrightness);
  illumination.setOffBrightness(settings.illuminationOffBrightness);
  display.setBrightness(systemInputs.doorOpen()
                            ? settings.displayBrightness
                            : settings.displayClosedBrightness());
  statusLeds.setBrightness(settings.statusBrightness);
  statusLeds.setReadyColor(settings.statusColor());
  statusLeds.setPowerSignal(true);
  relays.setMotionAllowed(motionPolicyAllows(), now);
}

// Restores only explicitly enabled output domains after a normal cold start.
void restoreStoredOutputs(uint32_t now) {
  const ControllerSettings &settings = settingsStore.values();
  if (settings.programmingMode()) {
    return;
  }
  if (settings.rememberMotion()) {
    relays.requestSide(
        RelaySide::A,
        (settings.relayRestoreMask & _BV(RelayOutputs::R1SideADirection)) != 0
            ? RelayDirection::Reverse
            : RelayDirection::Forward,
        (settings.relayRestoreMask & _BV(RelayOutputs::R2SideAEnable)) != 0,
        now);
    relays.requestSide(
        RelaySide::B,
        (settings.relayRestoreMask & _BV(RelayOutputs::R3SideBDirection)) != 0
            ? RelayDirection::Reverse
            : RelayDirection::Forward,
        (settings.relayRestoreMask & _BV(RelayOutputs::R4SideBEnable)) != 0,
        now);
  }
  if (settings.rememberUserRelays()) {
    for (uint8_t general = 0; general < RelayOutputs::GeneralCount; ++general) {
      relays.setGeneral(
          general,
          (settings.relayRestoreMask &
           _BV(RelayOutputs::R5General1 + general)) != 0);
    }
  }
  if (settings.rememberUserPwm()) {
    for (uint8_t channel = 0; channel < PwmChannels::UserLightCount; ++channel) {
      pwm.setLogical(channel, userPwm12(settings.userPwm[channel]));
    }
  }
}

// Snapshots editable settings so the user can later discard atomically.
void beginEditTransaction(ProgramMode returnMode) {
  if (!editTransactionActive) {
    memcpy(editSnapshot, &settingsStore.values(), sizeof(editSnapshot));
    editTransactionActive = true;
  }
  editReturnMode = returnMode;
}

// Commits or restores an edit and enters the flashing result state.
void finishEditTransaction(bool save, uint32_t now) {
  if (!save) {
    memcpy(&settingsStore.values(), editSnapshot, sizeof(editSnapshot));
    applyStoredSettings(now);
  } else {
    settingsStore.markDirty(now);
    settingsStore.saveNow();
  }
  editTransactionActive = false;
  flashMessageSaved = save;
  flashMessageEndsAt = now + 900;
  if (save) {
    buzzer.success();
    statusLeds.playCue(StatusLedCue::Save, 900, now);
  } else {
    buzzer.error();
    statusLeds.playCue(StatusLedCue::Discard, 900, now);
  }
  modeManager.transitionTo(MODE_FLASH_MESSAGE);
}

// Dispatches physical, RF, or host navigation through the modal menu state machine.
void handleMenuAction(uint8_t action, bool fromRemote) {
  if (action > MENU_INCREASE) {
    return;
  }

  if (learningActive) {
    if (action == MENU_DECREASE) {
      endLearning(1, 0);
    }
    menuFeedback(fromRemote);
    return;
  }

  // Menu actions can arrive between loop snapshots (UART, RF, or key event),
  // so capture their exact event time before entering modal state logic.
  const uint32_t now = millis();
  if (modeManager.current() == MODE_BOOT ||
      modeManager.current() == MODE_FAULT) {
    menuFeedback(fromRemote);
    return;
  }

#if PCCONTROLLER_MENU_HIERARCHY
  if (menuCategorySelected() && isMenuMode(modeManager.current())) {
    const uint8_t category = menuTreeState & 0x03U;
    if (action == MENU_PREVIOUS || action == MENU_NEXT) {
      moveMenuCategory(action == MENU_NEXT, now);
    } else if (action == MENU_INCREASE) {
      const uint8_t page = firstConfiguredMenuPage(category);
      if (page != 0xFF) {
        setMenuPage(page);
        showMenuLabel(now);
      }
    } else {
      menuTreeState = menuCategory(menuPage);
      showMenuLabel(now);
    }
    menuFeedback(fromRemote);
    return;
  }

#endif

  // KEY owns all four actions, including K3 identification, before the generic
  // leaf hierarchy considers K3 as Back.
  if (modeManager.current() == MODE_KEYS) {
    identifiedKey = static_cast<uint8_t>(action + 1);
    identifiedKeyEndsAt = now + 900;
    menuFeedback(fromRemote);
    return;
  }

  switch (modeManager.current()) {
    case MODE_ILLUMINATION_MODE_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_ILLUMINATION_ON_EDIT);
      } else {
        adjustIlluminationMode(action == MENU_INCREASE, now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_ILLUMINATION_ON_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_ILLUMINATION_MODE_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_ILLUMINATION_OFF_EDIT);
      } else {
        illumination.setOnBrightness(adjustedBrightness(
            illumination.onBrightness(), action == MENU_INCREASE));
        markIlluminationSettingsChanged(now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_ILLUMINATION_OFF_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_ILLUMINATION_ON_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else {
        illumination.setOffBrightness(adjustedBrightness(
            illumination.offBrightness(), action == MENU_INCREASE));
        markIlluminationSettingsChanged(now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_SOUND_EDIT:
      if (action == MENU_PREVIOUS) {
        if (settingsMenuItem == 0) {
          modeManager.transitionTo(MODE_SAVE_PROMPT);
        } else {
          --settingsMenuItem;
          showSettingsLabel(now);
        }
      } else if (action == MENU_NEXT) {
        if (++settingsMenuItem >= SettingsItemCount) {
          settingsMenuItem = static_cast<uint8_t>(SettingsItemCount - 1);
          modeManager.transitionTo(MODE_SAVE_PROMPT);
        } else {
          showSettingsLabel(now);
        }
      } else {
        const bool increase = action == MENU_INCREASE;
        ControllerSettings &settings = settingsStore.values();
        switch (settingsMenuItem) {
          case 0:
            setSilentMode(!increase, now);
            break;
          case 1:
            settings.displayBrightness = static_cast<uint8_t>(
                (settings.displayBrightness + (increase ? 1U : 7U)) & 0x07U);
            display.setBrightness(systemInputs.doorOpen()
                                      ? settings.displayBrightness
                                      : settings.displayClosedBrightness());
            break;
          case 2:
            settings.setDisplayClosedBrightness(static_cast<uint8_t>(
                (settings.displayClosedBrightness() +
                 (increase ? 1U : 7U)) & 0x07U));
            break;
          case 3:
            settings.statusBrightness =
                adjustedBrightness(settings.statusBrightness, increase);
            statusLeds.setBrightness(settings.statusBrightness);
            break;
          case 4:
            settings.setStatusColor(static_cast<uint8_t>(
                (settings.statusColor() + (increase ? 1U : 4U)) % 5U));
            statusLeds.setReadyColor(settings.statusColor());
            break;
          case 5:
            settings.setVoltageDecimals(static_cast<uint8_t>(
                (settings.voltageDecimals() + (increase ? 1U : 2U)) % 3U));
            break;
          case 6:
            settings.setCurrentDecimals(static_cast<uint8_t>(
                (settings.currentDecimals() + (increase ? 1U : 2U)) % 3U));
            break;
        }
      }
      menuFeedback(fromRemote);
      return;

    case MODE_PWM_CHANNEL_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_PWM_VALUE_EDIT);
      } else {
        pwm.adjustChannel(action == MENU_INCREASE ? 1 : -1, now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_PWM_VALUE_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_PWM_CHANNEL_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else {
        pwm.adjustValue(action == MENU_INCREASE ? PWM_MENU_STEP
                                                    : -PWM_MENU_STEP,
                            now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_USER_PWM_CHANNEL_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_USER_PWM_VALUE_EDIT);
      } else if (action == MENU_INCREASE) {
        userPwmMenuIndex = static_cast<uint8_t>(
            (userPwmMenuIndex + 1) % 8);
      } else {
        userPwmMenuIndex =
            userPwmMenuIndex == 0
                ? 7
                : static_cast<uint8_t>(userPwmMenuIndex - 1);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_USER_PWM_VALUE_EDIT: {
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_USER_PWM_CHANNEL_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else {
        uint8_t &value =
            settingsStore.values().userPwm[userPwmMenuIndex];
        value = adjustedBrightness(value, action == MENU_INCREASE);
        pwm.setLogical(userPwmMenuIndex, userPwm12(value));
      }
      menuFeedback(fromRemote);
      return;
    }

    case MODE_USER_RELAY_CHANNEL_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_USER_RELAYS);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_USER_RELAY_BEHAVIOR_EDIT);
      } else if (action == MENU_INCREASE) {
        userRelayMenuIndex =
            static_cast<uint8_t>((userRelayMenuIndex + 1) % 4);
      } else {
        userRelayMenuIndex =
            userRelayMenuIndex == 0
                ? 3
                : static_cast<uint8_t>(userRelayMenuIndex - 1);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_USER_RELAY_BEHAVIOR_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_USER_RELAY_CHANNEL_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_USER_RELAY_CONTROL);
      } else {
        userRelayBehavior = action == MENU_INCREASE ? 1 : 0;
      }
      menuFeedback(fromRemote);
      return;

    case MODE_USER_RELAY_CONTROL:
      if (action == MENU_PREVIOUS) {
        setSelectedUserRelay(false, now);
        modeManager.transitionTo(MODE_USER_RELAY_BEHAVIOR_EDIT);
      } else if (action == MENU_NEXT) {
        setSelectedUserRelay(false, now);
        modeManager.transitionTo(MODE_USER_RELAYS);
      } else if (action == MENU_DECREASE) {
        setSelectedUserRelay(false, now);
      } else if (userRelayBehavior == 0) {
        setSelectedUserRelay(!selectedUserRelayActive(), now);
      } else {
        setSelectedUserRelay(true, now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_MOTION_CONTROL: {
      const uint8_t side = action >= MENU_DECREASE ? 1 : 0;
      const bool reverse =
          action == MENU_NEXT || action == MENU_INCREASE;
      const bool accepted = relays.requestSide(
          static_cast<::RelaySide>(side),
          reverse ? RelayDirection::Reverse : RelayDirection::Forward, true,
          now);
      if (accepted && fromRemote) {
        remoteMomentaryKind = RemoteActionKind::Side;
        remoteMomentaryValue = side;
        remoteMomentaryEndsAt = now + 350;
      }
      menuFeedback(fromRemote);
      return;
    }

    case MODE_SAVE_PROMPT:
      finishEditTransaction(action == MENU_NEXT ||
                                action == MENU_INCREASE,
                            now);
      return;

    case MODE_FLASH_MESSAGE:
      return;

    case MODE_RELAY_CHANNEL_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_RELAY);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_RELAY_VALUE_EDIT);
      } else if (action == MENU_INCREASE) {
        relayMenuIndex = static_cast<uint8_t>((relayMenuIndex + 1) % 8);
      } else {
        relayMenuIndex =
            relayMenuIndex == 0 ? 7 : static_cast<uint8_t>(relayMenuIndex - 1);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_RELAY_VALUE_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_RELAY_CHANNEL_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_RELAY);
      } else {
        setSelectedRelay(action == MENU_INCREASE, now);
      }
      menuFeedback(fromRemote);
      return;

    default:
      break;
  }

  switch (action) {
    case MENU_PREVIOUS:
#if PCCONTROLLER_MENU_VISIBILITY
      setMenuPage(nextConfiguredMenuPage(
          menuPage, false
#if PCCONTROLLER_MENU_HIERARCHY
          , menuTreeState
#endif
          ));
#else
      setMenuPage(menuPage == 0 ? PAGE_COUNT - 1 : menuPage - 1);
#endif
      break;
    case MENU_NEXT:
#if PCCONTROLLER_MENU_VISIBILITY
      setMenuPage(nextConfiguredMenuPage(
          menuPage, true
#if PCCONTROLLER_MENU_HIERARCHY
          , menuTreeState
#endif
          ));
#else
      setMenuPage(static_cast<uint8_t>((menuPage + 1) % PAGE_COUNT));
#endif
      break;
    case MENU_DECREASE:
      // The shared K3 dispatch preserves the two leaf-owned actions: KEY was
      // handled above, while rELY is All-Off. Other leaves open their parent.
      if (leafDecreaseAction(modeManager.current()) ==
          LeafDecreaseAction::AllRelaysOff) {
        relays.allOff(now);
#if PCCONTROLLER_MENU_HIERARCHY
      } else {
        menuTreeState = static_cast<uint8_t>(
            MenuCategorySelector | menuCategory(menuPage));
        showMenuCategory(now);
#endif
      }
      break;
    case MENU_INCREASE:
      if (menuPage == PAGE_ILLUMINATION) {
        beginEditTransaction(MODE_ILLUMINATION);
        modeManager.transitionTo(MODE_ILLUMINATION_MODE_EDIT);
      } else if (menuPage == PAGE_SOUND) {
        beginEditTransaction(MODE_SOUND);
        settingsMenuItem = 0;
        modeManager.transitionTo(MODE_SOUND_EDIT);
      } else if (menuPage == PAGE_PWM) {
        beginEditTransaction(MODE_PWM);
        modeManager.transitionTo(MODE_PWM_CHANNEL_EDIT);
      } else if (menuPage == PAGE_RELAY) {
        relays.allOff(now);
        modeManager.transitionTo(MODE_RELAY_CHANNEL_EDIT);
      } else if (menuPage == PAGE_USER_PWM) {
        beginEditTransaction(MODE_USER_PWM);
        for (uint8_t channel = 0; channel < 8; ++channel) {
          pwm.setLogical(
              channel,
              userPwm12(settingsStore.values().userPwm[channel]));
        }
        modeManager.transitionTo(MODE_USER_PWM_CHANNEL_EDIT);
      } else if (menuPage == PAGE_USER_RELAYS) {
        modeManager.transitionTo(MODE_USER_RELAY_CHANNEL_EDIT);
      } else if (menuPage == PAGE_MOTION) {
        if (relays.motionAllowed()) {
          modeManager.transitionTo(MODE_MOTION_CONTROL);
        } else {
          buzzer.error();
        }
      } else if (menuPage == PAGE_RF) {
        beginLearning(RF_LEARN_INDEFINITE, 0);
      } else {
        display.showText(commonText(TextError));
        menuLabelEndsAt = now + 650;
        statusLeds.playCue(StatusLedCue::Discard, 650, now);
        buzzer.error();
        return;
      }
      break;
  }
  menuFeedback(fromRemote);
}

// Applies one classified physical or remote gesture without duplicating the
// local safety path in protocol dispatch.
void applyKeyGesture(uint8_t bit, KeyEvent event) {
  const ProgramMode mode = modeManager.current();
  const bool momentary = mode == MODE_MOTION_CONTROL ||
                         (mode == MODE_USER_RELAY_CONTROL &&
                          userRelayBehavior && bit == BoardPins::KeyIncrease);
  if (momentary) {
    if (event == KeyEvent::Down) {
      handleMenuAction(bit);
    } else if (event == KeyEvent::Up) {
      const uint32_t releasedAt = millis();
      if (mode == MODE_MOTION_CONTROL) {
        relays.stopSide(
            static_cast<::RelaySide>(bit <= BoardPins::KeyNext ? 0 : 1),
            releasedAt);
      } else {
        setSelectedUserRelay(false, releasedAt);
      }
    }
  } else if (event == KeyEvent::Click || event == KeyEvent::HoldStart ||
             (event == KeyEvent::HoldRepeat && mode != MODE_KEYS)) {
    // HoldStart supplies the one action suppressed by the absent Click.
    handleMenuAction(bit);
  } else if (event == KeyEvent::DoubleClick && bit == BoardPins::KeyPrevious) {
    setMenuPage(settingsStore.values().defaultMenuPage);
  }
}

// Keeps raw momentary outputs immediate while deferring normal menu actions
// until a click or hold classification is known.
void keyGesture(uint8_t bit, KeyEvent event, void *) {
  if ((hostLcdFlags & HOST_PANEL_CAPTURED) == 0) {
    applyKeyGesture(bit, event);
  }

  appEvents.key(bit, static_cast<uint8_t>(event));
}

// Stops motion and exits its modal page after either side's two-key hold.
void serviceMotionExit(uint32_t now) {
  const bool motionControl = modeManager.current() == MODE_MOTION_CONTROL;
  const bool sideAExit =
      menuKeys[0].isPressed() && menuKeys[1].isPressed();
  const bool sideBExit =
      menuKeys[2].isPressed() && menuKeys[3].isPressed();
  if (!motionControl || (!sideAExit && !sideBExit)) {
    motionExitStartedAt = 0;
    return;
  }
  if (motionControl) {
    relays.allOff(now);
  }
  if (motionExitStartedAt == 0xFFFFFFFFUL) {
    return;
  }
  if (motionExitStartedAt == 0) {
    motionExitStartedAt = now;
  } else if (static_cast<uint32_t>(now - motionExitStartedAt) >=
             static_cast<uint16_t>(
                 settingsStore.values().motionExitHoldSeconds()) * 1000U) {
    setMenuPage(PAGE_MOTION);
    buzzer.success();
    motionExitStartedAt = 0xFFFFFFFFUL;
  }
}

// Debounces door/BT states, emits edges, and enforces door-dependent motion policy.
void serviceSystemInputs(uint32_t now) {
  systemInputs.update(shiftRegisters.rawInputs(), now);

  bool value;
  if (systemInputs.consumeDoorChange(value)) {
    appEvents.door(value);
    if (settingsStore.values().doorAudioEnabled()) {
      buzzer.beep(45, value ? 1700 : 1100);
    }
    statusLeds.playCue(value ? StatusLedCue::DoorOpen
                             : StatusLedCue::DoorClosed,
                       720, now);
    relays.setMotionAllowed(motionPolicyAllows(), now);
    if (!relays.motionAllowed()) {
      if (modeManager.current() == MODE_MOTION_CONTROL) {
        modeManager.transitionTo(MODE_MOTION);
      }
    }
    if (!value && !editTransactionActive &&
        modeManager.current() != MODE_BOOT &&
        modeManager.current() != MODE_FAULT) {
      if (modeManager.current() == MODE_MOTION_CONTROL) {
        relays.allOff(now);
      }
      setMenuPage(settingsStore.values().defaultMenuPage);
      if (settingsStore.values().saveLastMenuPage()) {
        // A door close is a natural commit point and avoids losing the most
        // recent page if power is removed immediately afterwards.
        settingsStore.saveNow();
      }
    }
  }
  // Clear the per-edge notification; the categorized state below is reported
  // only when it changes, so a blinking LED cannot flood the UART.
  systemInputs.consumeBluetoothEdge(value);

  static bool initialized = false;
  static BluetoothIndicatorState reported = BluetoothIndicatorState::Off;
  const BluetoothIndicatorState current = systemInputs.bluetoothState(now);
  if (!initialized) {
    initialized = true;
    reported = current;
  } else if (reported != current) {
    reported = current;
    appEvents.bluetooth(static_cast<uint8_t>(current));
    statusLeds.playCue(StatusLedCue::Bluetooth, 600, now);
  }
}

// Polls both shift registers, then advances keys and motion-exit detection.
void serviceShiftRegisterAndKeys(uint32_t now) {
  if (static_cast<uint32_t>(now - lastShiftPollAt) < SHIFT_POLL_MS) {
    return;
  }
  lastShiftPollAt = now;
  shiftRegisters.service();
  serviceSystemInputs(now);
  for (Key &key : menuKeys) {
    key.update(now);
  }
  serviceMotionExit(now);
}

// Renders the current illumination mode from the packed flash text table.
void showIlluminationMode() {
  display.showText(commonText(pgm_read_byte(
      ModeTextOffsets + static_cast<uint8_t>(illumination.mode()))));
}

// Renders the selected PWM channel without a hidden demo/mode state.
void showPwmChannel() {
  const uint8_t channel = pwm.channel();
  char label[5] = {
      'P',
      '-',
      static_cast<char>('0' + channel / 10),
      static_cast<char>('0' + channel % 10),
      '\0',
  };
  display.showText(label);
}

// Alternates timer total/remaining while indefinite learning keeps LErn.
void showLearningProgress(uint32_t now) {
  if (learningMode != RF_LEARN_TIMER) {
    display.showText(commonText(TextLearn));
    return;
  }
  const bool remaining = ((now / 1000UL) & 1U) != 0;
  const uint8_t seconds = remaining ? learningRemainingSeconds(now)
                                    : learningTotalSeconds;
  char text[5] = {
      remaining ? 'r' : 't',
      static_cast<char>(seconds >= 100 ? '0' + seconds / 100 : ' '),
      static_cast<char>(seconds >= 10 ? '0' + (seconds / 10) % 10 : ' '),
      static_cast<char>('0' + seconds % 10),
      '\0',
  };
  display.showText(text);
}

// Advances one cached four-character window of a host-owned Door-page scroll.
void showHostSegmentText(uint32_t now) {
  if (hostSegmentTextLength <= 4) {
    display.showText(hostSegmentText);
    return;
  }
  if (timeReached(now, hostSegmentTextEndsAt)) {
    if (++hostSegmentScrollIndex >= hostSegmentTextLength) {
      hostSegmentScrollIndex = 0;
    }
    hostSegmentTextEndsAt = now + hostSegmentStepMs;
  }
  char window[5];
  for (uint8_t index = 0; index < 4; ++index) {
    uint8_t source = static_cast<uint8_t>(hostSegmentScrollIndex + index);
    if (source >= hostSegmentTextLength) {
      source = static_cast<uint8_t>(source - hostSegmentTextLength);
    }
    window[index] = hostSegmentText[source];
  }
  window[4] = '\0';
  display.showText(window);
}

// Refreshes only changed front-panel content at a smooth 20 ms service cadence.
void serviceDisplay(uint32_t now) {
  if (static_cast<uint32_t>(now - lastDisplayRefreshAt) <
      DISPLAY_REFRESH_MS) {
    return;
  }
  lastDisplayRefreshAt = now;

  const ProgramMode currentMode = modeManager.current();
  if (currentMode == MODE_BOOT || currentMode == MODE_FAULT) {
    return;
  }
  if (learningActive) {
    showLearningProgress(now);
    return;
  }
  if (currentMode == MODE_FLASH_MESSAGE) {
    if (((now / 150U) & 1U) != 0) {
      display.showText(commonText(flashMessageSaved ? TextSave
                                                   : TextDiscard));
    } else {
      display.clear();
    }
    return;
  }
  if (currentMode == MODE_SAVE_PROMPT) {
    display.showText(commonText(((now / 600U) & 1U) != 0
                                    ? TextSave
                                    : TextDiscard));
    return;
  }
#if PCCONTROLLER_MENU_HIERARCHY
  if (menuCategorySelected() && isMenuMode(currentMode)) {
    showMenuCategory(now);
    return;
  }
#endif
  if (!timeReached(now, menuLabelEndsAt)) {
    return;
  }
  if (hostSegmentTextActive && hostSegmentTextLength <= 4 &&
      hostSegmentTextEndsAt != 0 && timeReached(now, hostSegmentTextEndsAt)) {
    hostSegmentTextActive = false;
  }
  // Host text overlays ordinary pages only. A long message is intentionally
  // scoped to Door; all editors, warnings, learning, and programming win.
  if (hostSegmentTextActive && isMenuMode(currentMode) &&
      (hostSegmentTextLength <= 4 || currentMode == MODE_DOOR)) {
    showHostSegmentText(now);
    return;
  }
  if (currentMode >= MODE_ILLUMINATION_MODE_EDIT &&
      currentMode <= MODE_USER_RELAY_BEHAVIOR_EDIT &&
      ((now / 300U) & 1U) == 0) {
    display.clear();
    return;
  }
  // Render ordinary menu pages as explicit early returns. Keeping them out of
  // a second dense AVR jump table prevents the layout-sensitive reset that was
  // reproduced when navigating among otherwise healthy pages.
  if (currentMode == MODE_DOOR) {
    display.showText(commonText(systemInputs.doorOpen() ? TextOpen
                                                       : TextClosed));
    return;
  }
  if (currentMode == MODE_VOLTAGE) {
    if (sensors.supplyMilliVolts == INVALID_I32) {
      display.showUnavailable();
    } else {
      const uint8_t decimals = settingsStore.values().voltageDecimals();
      display.showFixed(
          scaledMilliValue(sensors.supplyMilliVolts, decimals), decimals);
    }
    return;
  }
  if (currentMode == MODE_CURRENT) {
    if (sensors.currentMilliAmps == INVALID_I32) {
      display.showUnavailable();
    } else {
      const uint8_t decimals = settingsStore.values().currentDecimals();
      display.showFixed(
          scaledMilliValue(sensors.currentMilliAmps, decimals), decimals);
    }
    return;
  }
  if (currentMode == MODE_TLED || currentMode == MODE_TBT) {
    const uint8_t index = currentMode == MODE_TLED ? 0 : 1;
    // Temperature text is prepared only when a conversion completes. Keep this
    // path identical to the proven host-text early return and keep arithmetic
    // out of the time-critical TM1637 renderer.
    display.showText(temperatureSegmentText[index]);
    return;
  }
  if (currentMode == MODE_ILLUMINATION) {
    showIlluminationMode();
    return;
  }
  if (currentMode == MODE_SOUND) {
    display.showText(commonText(settingsStore.values().silent()
                                    ? TextMute
                                    : TextBeep));
    return;
  }
  if (currentMode == MODE_PWM) {
    if (!pwm.available()) {
      display.showUnavailable();
    } else if (((now / 900UL) & 1U) == 0) {
      showPwmChannel();
    } else {
      display.showInteger(pwm.value());
    }
    return;
  }
  if (currentMode == MODE_RELAY) {
    if (((now / 900UL) & 1U) == 0) {
      const char relayLabel[4] = {
          'r',
          '-',
          static_cast<char>('0' + (relayMenuIndex + 1) / 10),
          static_cast<char>('0' + (relayMenuIndex + 1) % 10),
      };
      display.showText(relayLabel);
    } else {
      display.showText(commonText(selectedRelayActive() ? TextOn : TextOff));
    }
    return;
  }
  if (currentMode == MODE_KEYS) {
    if (!timeReached(now, identifiedKeyEndsAt) && identifiedKey != 0) {
      display.showInteger(identifiedKey);
    } else {
      display.showText(commonText(TextKey));
    }
    return;
  }
  if (currentMode == MODE_USER_PWM) {
    if (((now / 900UL) & 1U) == 0) {
      display.showInteger(static_cast<int32_t>(userPwmMenuIndex + 1));
    } else {
      display.showInteger(settingsStore.values().userPwm[userPwmMenuIndex]);
    }
    return;
  }
  if (currentMode == MODE_USER_RELAYS) {
    display.showInteger(static_cast<int32_t>(
        relays.activeRelayMask() >> 4));
    return;
  }
  if (currentMode == MODE_MOTION) {
    display.showText(commonText(systemInputs.doorOpen() ? TextOpen
                                                       : TextClosed));
    return;
  }
  if (currentMode == MODE_RF) {
    if (learnedRemotes.count() == 0) {
      display.showUnavailable();
    } else {
      display.showInteger(learnedRemotes.count());
    }
    return;
  }

  switch (currentMode) {
    case MODE_ILLUMINATION_MODE_EDIT:
      showIlluminationMode();
      return;
    case MODE_ILLUMINATION_ON_EDIT:
      display.showInteger(illumination.onBrightness());
      return;
    case MODE_ILLUMINATION_OFF_EDIT:
      display.showInteger(illumination.offBrightness());
      return;
    case MODE_SOUND_EDIT:
      if (settingsMenuItem == 0) {
        display.showText(commonText(settingsStore.values().silent()
                                        ? TextMute
                                        : TextBeep));
      } else {
        const ControllerSettings &settings = settingsStore.values();
        uint8_t value;
        switch (settingsMenuItem) {
          case 1:
            value = settings.displayBrightness;
            break;
          case 2:
            value = settings.displayClosedBrightness();
            break;
          case 3:
            value = settings.statusBrightness;
            break;
          case 4:
            value = settings.statusColor();
            break;
          case 5:
            value = settings.voltageDecimals();
            break;
          default:
            value = settings.currentDecimals();
            break;
        }
        display.showInteger(value);
      }
      return;
    case MODE_PWM_CHANNEL_EDIT:
      display.showInteger(pwm.channel());
      return;
    case MODE_PWM_VALUE_EDIT:
      display.showInteger(pwm.value());
      return;
    case MODE_RELAY_CHANNEL_EDIT:
      display.showInteger(static_cast<int32_t>(relayMenuIndex + 1));
      return;
    case MODE_RELAY_VALUE_EDIT:
      display.showText(commonText(selectedRelayActive() ? TextOn : TextOff));
      return;
    case MODE_USER_PWM_CHANNEL_EDIT:
      display.showInteger(static_cast<int32_t>(userPwmMenuIndex + 1));
      return;
    case MODE_USER_PWM_VALUE_EDIT:
      display.showInteger(
          settingsStore.values().userPwm[userPwmMenuIndex]);
      return;
    case MODE_USER_RELAY_CHANNEL_EDIT:
      display.showInteger(static_cast<int32_t>(userRelayMenuIndex + 5));
      return;
    case MODE_USER_RELAY_BEHAVIOR_EDIT:
      display.showText(commonText(userRelayBehavior == 0 ? TextToggle
                                                        : TextPush));
      return;
    case MODE_USER_RELAY_CONTROL:
      display.showText(
          commonText(selectedUserRelayActive() ? TextOn : TextOff));
      return;
    case MODE_MOTION_CONTROL:
      display.showInteger(relays.activeRelayMask());
      return;
    default:
      break;
  }

}
