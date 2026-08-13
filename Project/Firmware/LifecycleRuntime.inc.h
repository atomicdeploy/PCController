// Implementation fragment compiled once; owns startup and service composition.
// -----------------------------------------------------------------------------
// Arduino lifecycle
// -----------------------------------------------------------------------------

// Initializes safety first, then UI, buses, sensors, RF, and readiness events.
static inline __attribute__((always_inline)) void initializeController() {
  // The former Wire timeout path caused reproducible live-menu resets. The
  // compact master, startup bus recovery, and this watchdog bound every path.
  wdt_enable(WDTO_2S);
  wdt_reset();
  now = millis();
  appProtocol.begin(PCCONTROLLER_UART_BAUD, handleProtocolFrame);
  resetTelemetry.begin();
  // Announce as soon as UART0 is ready. Opening a USB serial adapter often
  // resets the MCU; serving HELLO during initialization prevents the host's
  // first request from being lost behind sensor/LCD setup.
  sendHello(0);
  appProtocol.service();
  wdt_reset();
  now = millis();
  const uint32_t startupNow = now;
  shiftRegisters.begin();
  relays.begin(startupNow);
  systemInputs.begin(shiftRegisters.rawInputs(), startupNow);
  buzzer.begin();
  audioCues.begin();
  AddressableLeds::begin();
  loadIlluminationSettings();
  const ControllerSettings &settings = settingsStore.values();
  const bool programming = settings.programmingMode();
  menuPage = canonicalFrontPanelPage(settings.defaultMenuPage);
  if (!frontPanelPageCompiled(menuPage)) {
    menuPage = PAGE_KEYS;
  }
  buzzer.setMuted(effectiveSilentMode() || programming);
  streamPeriodMs = settings.streamPeriodMs;
  display.begin(programming || systemInputs.doorOpen()
                    ? settings.displayBrightness
                    : settings.displayClosedBrightness());
  display.showText(commonText(programming ? TextProgram : TextBoot));

  for (Key &key : menuKeys) {
    key.setEventCallback(keyGesture);
  }
  appProtocol.service();
  wdt_reset();

  const bool i2cReady = prepareI2cBus();
  if (i2cReady) {
    i2cBus.begin();
    // A missing optional peripheral costs one bounded turn, not a perceptible
    // key-latency stall.
    i2cBus.setWireTimeout(5000UL, true);
    pwmAvailable = pwmDriver.begin();
    if (pwmAvailable) {
      pwmAvailable =
          pwmDriver.setFrequency(
              static_cast<uint16_t>(BoardPins::PwmFrequencyHz)) &&
          normalizePwmMode2();
    }

    ina219Available = ina219.begin();
  }
  pwm.begin(pwmAvailable, startupNow);
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

  temperatureBus.begin();
  discoverTemperatureSensors();
  requestTemperatures(startupNow);
  appProtocol.service();
  wdt_reset();

  learnedRemotes.begin();
  radioReceiver.setReceiveTolerance(70);
  radioReceiver.enableReceive(digitalPinToInterrupt(BoardPins::RcReceive));

  // Unsolicited cue policy is a build capability; TonePlayer/Buzzer itself
  // remains available to macros and host commands.
  firmwareReady = true;
  appEvents.reset(resetTelemetry.cause(), resetTelemetry.count());
  sendHello(0);
  sendTelemetry(0);
}

// Advances every cooperative domain without blocking UART or safety deadlines.
static inline __attribute__((always_inline)) void serviceController() {
  now = millis();
  // Keep the shared snapshot in registers across driver calls. Re-reading the
  // file-scope value grows this byte-tight AVR image past its identity boundary.
  const uint32_t loopNow = now;
  wdt_reset();

  // A due macro step is timestamped against the MCU clock. Run one before
  // accepting an ordinary UART frame so a continuous HOST presentation stream
  // cannot turn a precise macro delta into serial-backlog latency. Physical
  // key/RF work still receives its own turn below; macro dispatch is bounded
  // to one ordinary opcode per controller pass.
  ControllerProtocol::Frame queuedMacroFrame;
  if (macroPlayback.dequeueDue(queuedMacroFrame)) {
    const uint16_t errors = appProtocol.responseErrors();
    handleProtocolFrame(queuedMacroFrame, nullptr);
    macroPlayback.completeStep(errors == appProtocol.responseErrors());
    wdt_reset();
  }
  if (macroPlayback.takeSafeStopRequest()) safeStopMacroOutputs();

  appProtocol.service();
  // I2cTransfer is dispatched above and may establish/release a lease in this
  // same controller turn. Snapshot only after UART dispatch so no firmware-
  // owned PCA/INA/LCD transaction can overlap a newly granted host lease.
  const bool i2cReserved = i2cLeaseActive(loopNow);
  if (settingsStore.values().programmingMode()) {
    // The host may just have queued the durable Prog latch. Keep publishing
    // that record cooperatively while all ordinary outputs remain disabled.
    servicePersistence(loopNow);
    return;
  }
  serviceRadio();
#if !PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI
  serviceLearningTimer(loopNow);
#endif
  // Give physical keys and expiring RF momentary actions a turn before macro
  // output. Combined with one UART frame and one macro step per loop, this is
  // the cooperative fairness contract for physical, RF, and virtual input.
  serviceShiftRegisterAndKeys(loopNow);
  serviceRemoteMomentary(loopNow);
  const bool hostOffline = hostUnavailable();
  servicePowerSignalFallback(hostOffline, i2cReserved, loopNow);
  if (hostOffline && (hostLcdFlags & HOST_LCD_OFFLINE) == 0 &&
      !i2cReserved) {
    if ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0) {
      releaseHostPanel();
    } else {
      clearHostSegmentText();
    }
    showHostOfflineOnLcd();
    hostLcdFlags |= HOST_LCD_OFFLINE;
  }
  if (macroPlayback.hostDependent() && hostOffline) {
    macroPlayback.cancel(false);
    if (macroPlayback.takeSafeStopRequest()) {
      safeStopMacroOutputs();
    }
  }
  serviceDeferredMacroPwmSafeStop(i2cReserved);
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
  }
  hotReported = hot;

  // Report fault entry and recovery exactly once per transition.
  const bool firmwareFault = modeManager.current() == MODE_FAULT;
  static bool faultReported = false;
  if (firmwareFault != faultReported) {
    appEvents.alert(ControllerAlertKind::Fault, firmwareFault);
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
#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
    const BluetoothIndicatorState btState = systemInputs.bluetoothState(loopNow);
    // A blinking indicator means powered but waiting for connection; keep it
    // distinct from the deliberate green/red powered-off indication.
    desiredLedMode = btState == BluetoothIndicatorState::On
                         ? StatusLedMode::Connected
                         : (btState == BluetoothIndicatorState::Blinking
                                ? StatusLedMode::Waiting
                                : StatusLedMode::Disconnected);
#else
    desiredLedMode = StatusLedMode::Connected;
#endif
  }
  if (statusLeds.mode() != desiredLedMode) {
    statusLeds.setMode(desiredLedMode, loopNow);
  }

  illumination.service(systemInputs.doorOpen(), !i2cReserved, loopNow);
  relays.service(loopNow);
  const uint8_t relayMask = relays.activeRelayMask();
  if (relayMask != lastRelayMask) {
    appEvents.relay(relayMask);
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
#if PCCONTROLLER_ENABLE_ASYNC_SEGMENT_EVENTS
  serviceSegmentPush();
#endif
  if (!i2cReserved) {
    statusLeds.service(loopNow);
  }
#if PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS && \
    PCCONTROLLER_ENABLE_PCA9685 && PCCONTROLLER_ENABLE_STATUS_LED_ENGINE
  serviceStatusLedPush();
#endif
#if PCCONTROLLER_ENABLE_TASK_SCHEDULER
  taskManager.update(loopNow);
#endif
  buzzer.update(loopNow);
#if PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS
  serviceBuzzerPush();
#endif

  if (streamPeriodMs != 0 &&
      static_cast<uint32_t>(loopNow - lastTelemetryAt) >= streamPeriodMs) {
    lastTelemetryAt = loopNow;
    sendTelemetry(0);
  }

  // Launch at most one asynchronous EEPROM byte only after every latency-
  // sensitive domain has had its turn. The next loop checks EEPE before any
  // EEPROM read/write path can run.
  servicePersistence(loopNow);
  safeReset.service(relays, pwm, loopNow);
}
