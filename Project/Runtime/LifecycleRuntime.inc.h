// Implementation fragment compiled once; owns startup and service composition.
// -----------------------------------------------------------------------------
// Arduino lifecycle
// -----------------------------------------------------------------------------

void controllerRelayApplied(uint8_t mask, uint32_t appliedAtUs) {
  macroPlayback.recordRelay(mask, appliedAtUs);
  appEvents.relay(mask, appliedAtUs);
#if PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS
  automationExecutor.dispatch(AutomationEventKind::Relay, mask, now);
#endif
}

// Initializes safety first, then UI, buses, sensors, RF, and readiness events.
static inline __attribute__((always_inline)) void initializeController() {
  // The former Wire timeout path caused reproducible live-menu resets. The
  // compact master, startup bus recovery, and this watchdog bound every path.
  wdt_enable(WDTO_2S);
  wdt_reset();
  now = millis();
  appProtocol.begin(Serial, PCCONTROLLER_UART_BAUD, handleProtocolFrame);
  resetTelemetry.begin();
#if PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS
  boardAutomations.begin();
  automationExecutor.reset();
#endif
  // Announce as soon as UART0 is ready. Opening a USB serial adapter often
  // resets the MCU; serving HELLO during initialization prevents the host's
  // first request from being lost behind sensor/LCD setup.
  sendHello(0);
  appProtocol.service();
  wdt_reset();
  now = millis();
  const uint32_t startupNow = now;
  shiftRegisters.begin();
  relays.begin(shiftRegisters, controllerRelayApplied, startupNow);
  systemInputs.begin(shiftRegisters.rawInputs(), startupNow);
  buzzer.begin(BoardPins::Buzzer);
#if PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES
  audioCues.begin();
#endif
  AddressableLeds::bindWorkspace(macroPlayback.claimSharedWorkspace());
  AddressableLeds::begin();
  loadIlluminationSettings();
#if PCCONTROLLER_ENABLE_EEPROM_MENU_LABELS
  EepromMenuLabels::begin();
#endif
  const ControllerSettings &settings = settingsStore.values();
  const bool programming = settings.programmingMode();
  menuPage = settings.defaultMenuPage;
  buzzer.setMuted(settings.silent() || programming);
  streamPeriodMs = settings.streamPeriodMs;
  display.begin(programming || systemInputs.doorOpen()
                    ? settings.displayBrightness
                    : settings.displayClosedBrightness());
  display.showText(commonText(programming ? TextProgram : TextBoot));

  for (uint8_t index = 0; index < 4; ++index) {
    menuKeys[index].begin(index);
    menuKeys[index].setEventCallback(keyGesture);
  }
  appProtocol.service();
  wdt_reset();

  const bool i2cReady = prepareI2cBus();
  if (i2cReady) {
    i2cBus.begin();
    // Reset the TWI peripheral if any state remains blocked for 25 ms.
    i2cBus.setWireTimeout(25000UL, true);
    pwmAvailable = pwmDriver.begin();
    if (pwmAvailable) {
      pwmAvailable =
          pwmDriver.setFrequency(
              static_cast<uint16_t>(BoardPins::PwmFrequencyHz)) &&
          normalizePwmMode2();
    }

    ina219Available = ina219.begin();
  }
  pwm.begin(pwmDriver, pwmAvailable, startupNow);
  if (programming) {
    // A durable programming latch must not briefly restore an On/Auto light
    // between PWM initialization and the normal all-off latch enforcement.
    illumination.setMode(IlluminationMode::Off);
    illumination.setOffBrightness(0);
  }
  illumination.begin(pwm, systemInputs.doorOpen(), startupNow);
  statusLeds.begin(pwm, programming ? 0 : settings.statusBrightness, startupNow,
                   !programming);
  applyStoredSettings(startupNow);
  restoreStoredOutputs(startupNow);
  appProtocol.service();
  wdt_reset();

  temperatureBus.begin(BoardPins::OneWireData);
  discoverTemperatureSensors();
  requestTemperatures(startupNow);
  appProtocol.service();
  wdt_reset();

  learnedRemotes.begin();
  radio.enableTransmit(BoardPins::RcTransmit);
  radio.setReceiveTolerance(70);
  radio.enableReceive(digitalPinToInterrupt(BoardPins::RcReceive));

  // EEPROM boot records are deliberately deferred until every relay/PWM/
  // safety policy and radio initialization above has completed. They reuse the
  // normal opcode dispatcher and cannot contain any output/motion/reset/I2C
  // operation outside BootOpcodeSequence's fixed safe whitelist.
  if (!programming) {
#if PCCONTROLLER_ENABLE_EEPROM_BOOT_OPCODES
    firmwareReady = true;
    BootOpcodeSequence::dispatch(appProtocol, handleProtocolFrame,
                                 BootOpcodeSequence::executionContext());
#else
    playBootMelody();
#endif
  }
  firmwareReady = true;
  appEvents.reset(resetTelemetry.cause(), resetTelemetry.count());
#if PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS
  automationExecutor.dispatch(AutomationEventKind::Boot, 0, now);
#endif
  sendHello(0);
  sendTelemetry(0);
}

// Advances every cooperative domain without blocking UART or safety deadlines.
__attribute__((noinline)) void serviceController() {
  now = millis();
  // Keep the shared snapshot in registers across driver calls. Re-reading the
  // file-scope value grows this byte-tight AVR image past its identity boundary.
  const uint32_t loopNow = now;
  const bool i2cReserved = i2cLeaseActive(loopNow);
  wdt_reset();

  // Dispatch one due macro step before accepting ordinary host traffic. This
  // keeps a continuous presentation/strip stream from turning an MCU-timed
  // macro delta into serial-backlog latency while retaining one-frame fairness
  // for UART, physical keys, radio, and the other cooperative domains.
  if (!settingsStore.values().programmingMode()) {
    ControllerProtocol::Frame queuedMacroFrame;
    if (macroPlayback.dequeueDue(queuedMacroFrame)) {
      const uint16_t errors = appProtocol.responseErrors();
      handleProtocolFrame(queuedMacroFrame, nullptr);
      macroPlayback.completeStep(errors == appProtocol.responseErrors());
      wdt_reset();
    }
    if (macroPlayback.takeSafeStopRequest()) {
      safeStopMacroOutputs();
    }
  }

  appProtocol.service();
  if (settingsStore.values().programmingMode()) {
    return;
  }
  serviceRadio();
  const bool hostOffline = hostUnavailable();
#if PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS
  if (hostOffline != hostWasUnavailable) {
    if (hostOffline) {
      safeStopMacroOutputs();
    }
    automationExecutor.dispatch(AutomationEventKind::Host,
                                static_cast<uint8_t>(!hostOffline), loopNow);
    hostWasUnavailable = hostOffline;
  }
#endif
  if (hostOffline && (hostLcdFlags & HOST_LCD_OFFLINE) == 0) {
    if ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0) {
      releaseHostPanel();
    } else {
      clearHostSegmentText();
    }
    showHostOfflineOnLcd();
    hostLcdFlags |= HOST_LCD_OFFLINE;
  }
  if (macroPlayback.active() && !macroPlayback.recording() &&
      hostOffline) {
    macroPlayback.cancel(false);
    if (macroPlayback.takeSafeStopRequest()) {
      safeStopMacroOutputs();
    }
  }
  serviceShiftRegisterAndKeys(loopNow);
  serviceRemoteMomentary(loopNow);
  serviceTemperatures(loopNow);
  if (!i2cReserved) {
    sampleIna219(loopNow);
  }
  programService(loopNow);

  // Edge state suppresses duplicate events while a cadence allows repeated
  // local HOT alarms during a sustained unsafe condition.
  const bool hot = temperatureHot();
  static bool hotReported = false;
  static uint32_t lastHotAlertAt = 0;
  if (hot && (!hotReported ||
              static_cast<uint32_t>(loopNow - lastHotAlertAt) >= 10000UL)) {
    lastHotAlertAt = loopNow;
  }
  if (hot != hotReported) {
    appEvents.alert(ControllerAlertKind::Hot, hot);
#if PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS
    automationExecutor.dispatch(
        AutomationEventKind::Alert,
        static_cast<uint8_t>((static_cast<uint8_t>(ControllerAlertKind::Hot)
                              << 1) |
                             static_cast<uint8_t>(hot)),
        loopNow);
#endif
  }
  hotReported = hot;

  // Report fault entry and recovery exactly once per transition.
  const bool firmwareFault = modeManager.current() == MODE_FAULT;
  static bool faultReported = false;
  if (firmwareFault != faultReported) {
    appEvents.alert(ControllerAlertKind::Fault, firmwareFault);
#if PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS
    automationExecutor.dispatch(
        AutomationEventKind::Alert,
        static_cast<uint8_t>((static_cast<uint8_t>(ControllerAlertKind::Fault)
                              << 1) |
                             static_cast<uint8_t>(firmwareFault)),
        loopNow);
#endif
    faultReported = firmwareFault;
  }

  // Critical local safety conditions dominate host overrides and transient
  // informational cues. Base operational state remains host-owned.
  StatusLedMode desiredLedMode;
  if (modeManager.current() == MODE_BOOT) {
    desiredLedMode = StatusLedMode::Boot;
  } else if (modeManager.current() == MODE_FAULT || hostOffline ||
             ((hostLcdFlags & HOST_PROGRAM_RUNNING) != 0 &&
              systemInputs.doorOpen())) {
    desiredLedMode = StatusLedMode::Fault;
  } else if (hot) {
    desiredLedMode = StatusLedMode::Warning;
  } else if (learningActive) {
    desiredLedMode = StatusLedMode::Learning;
  } else if ((hostLcdFlags & HOST_STATUS_OVERRIDE) != 0) {
    desiredLedMode = StatusLedMode::Custom;
  } else if ((hostLcdFlags & HOST_PROGRAM_RUNNING) != 0) {
    desiredLedMode = StatusLedMode::Running;
  } else {
    const BluetoothIndicatorState btState = systemInputs.bluetoothState(loopNow);
    // A blinking indicator means powered but waiting for connection; keep it
    // distinct from the deliberate green/red powered-off indication.
    desiredLedMode = btState == BluetoothIndicatorState::On
                         ? StatusLedMode::Connected
                         : (btState == BluetoothIndicatorState::Blinking
                                ? StatusLedMode::Waiting
                                : StatusLedMode::Disconnected);
  }
  if (statusLeds.mode() != desiredLedMode) {
    statusLeds.setMode(desiredLedMode, loopNow);
  }

  illumination.service(systemInputs.doorOpen(), !i2cReserved, loopNow);
  serviceIlluminationSettings(loopNow);
  relays.service(loopNow);
  const uint8_t relayMask = relays.activeRelayMask();
  if (relayMask != lastRelayMask) {
#if PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES
    if (settingsStore.values().relayAudioEnabled() &&
        ((relayMask ^ lastRelayMask) & 0xFAU) != 0) {
      audioCues.play((relayMask & ~lastRelayMask) != 0
                         ? AudioCue::OutputOn
                         : AudioCue::OutputOff);
    }
#endif
    settingsStore.values().relayRestoreMask = relayMask;
    settingsStore.markDirty(loopNow);
    lastRelayMask = relayMask;
  }
  const ControllerSettings &displaySettings = settingsStore.values();
  display.serviceBrightness(systemInputs.doorOpen()
                                ? displaySettings.displayBrightness
                                : displaySettings.displayClosedBrightness(),
                            loopNow);
  serviceDisplay(loopNow);
  serviceSegmentPush();
  if (!i2cReserved) {
    statusLeds.service(loopNow);
  }
  serviceStatusLedPush();
#if PCCONTROLLER_ENABLE_TASK_SCHEDULER
  taskManager.update(loopNow);
#endif
  buzzer.update(loopNow);
  serviceBuzzerPush();

  if (streamPeriodMs != 0 &&
      static_cast<uint32_t>(loopNow - lastTelemetryAt) >= streamPeriodMs) {
    lastTelemetryAt = loopNow;
    display.setBrightness(display.brightness());
    sendTelemetry(0);
  }

  safeReset.service(relays, pwm, loopNow);
}
