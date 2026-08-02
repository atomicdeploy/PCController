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
  ::now = millis();
  appProtocol.begin(PCCONTROLLER_UART_BAUD, handleProtocolFrame);
  resetTelemetry.begin();
  // Announce as soon as UART0 is ready. Opening a USB serial adapter often
  // resets the MCU; serving HELLO during initialization prevents the host's
  // first request from being lost behind sensor/LCD setup.
  sendHello(0);
  appProtocol.service();
  wdt_reset();
  const uint32_t now = millis();
  ::now = now;
  shiftRegisters.begin();
  relays.begin(now);
  systemInputs.begin(shiftRegisters.rawInputs(), now);
  buzzer.begin();
  AddressableLeds::begin();
  loadIlluminationSettings();
  const ControllerSettings &settings = settingsStore.values();
  const bool programming = settings.programmingMode();
  menuPage = settings.defaultMenuPage;
  buzzer.setMuted(settings.silent() || programming);
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
  pwm.begin(pwmAvailable, now);
  if (programming) {
    // A durable programming latch must not briefly restore an On/Auto light
    // between PWM initialization and the normal all-off latch enforcement.
    illumination.setMode(IlluminationMode::Off);
    illumination.setOffBrightness(0);
  }
  illumination.begin(pwm, systemInputs.doorOpen(), now);
  statusLeds.begin(pwm, programming ? 0 : settings.statusBrightness, now,
                   !programming);
  applyStoredSettings(now);
  restoreStoredOutputs(now);
  appProtocol.service();
  wdt_reset();

  temperatureBus.begin();
  discoverTemperatureSensors();
  requestTemperatures(now);
  appProtocol.service();
  wdt_reset();

  learnedRemotes.begin();
  radioTransmitter.enableTransmit(BoardPins::RcTransmit);
  radioReceiver.setReceiveTolerance(70);
  radioReceiver.enableReceive(digitalPinToInterrupt(BoardPins::RcReceive));

  if (!programming) {
    playBootMelody();
  }
  firmwareReady = true;
  appEvents.reset(resetTelemetry.cause(), resetTelemetry.count());
  sendHello(0);
  sendTelemetry(0);
}

// Advances every cooperative domain without blocking UART or safety deadlines.
static inline __attribute__((always_inline)) void serviceController() {
  const uint32_t now = millis();
  ::now = now;
  const bool i2cReserved = i2cLeaseActive(now);
  wdt_reset();

  appProtocol.service();
  if (settingsStore.values().programmingMode()) {
    return;
  }
  serviceRadio();
  ControllerProtocol::Frame queuedMacroFrame;
  while (macroPlayback.dequeueDue(queuedMacroFrame)) {
    const uint16_t errors = appProtocol.responseErrors();
    handleProtocolFrame(queuedMacroFrame, nullptr);
    macroPlayback.completeStep(errors == appProtocol.responseErrors());
    wdt_reset();
  }
  if (macroPlayback.takeSafeStopRequest()) {
    safeStopMacroOutputs();
  }
  const bool hostOffline = hostUnavailable();
  if (hostOffline && (hostLcdFlags & HOST_LCD_OFFLINE) == 0) {
    if ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0) {
      releaseHostPanel();
    } else {
      hostSegmentTextActive = false;
      hostSegmentTextLength = 0;
    }
    showHostOfflineOnLcd();
    hostLcdFlags |= HOST_LCD_OFFLINE;
  }
  if (macroPlayback.active() &&
      hostOffline) {
    macroPlayback.cancel(false);
    if (macroPlayback.takeSafeStopRequest()) {
      safeStopMacroOutputs();
    }
  }
  serviceShiftRegisterAndKeys(now);
  serviceRemoteMomentary(now);
  serviceTemperatures(now);
  if (!i2cReserved) {
    sampleIna219(now);
  }
  programService(now);

  // Edge state suppresses duplicate events while a cadence allows repeated
  // local HOT alarms during a sustained unsafe condition.
  const bool hot = temperatureHot();
  static bool hotReported = false;
  static uint32_t lastHotAlertAt = 0;
  if (hot && (!hotReported ||
              static_cast<uint32_t>(now - lastHotAlertAt) >= 10000UL)) {
    buzzer.error();
    lastHotAlertAt = now;
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
    const BluetoothIndicatorState btState = systemInputs.bluetoothState(now);
    // A blinking indicator means powered but waiting for connection; keep it
    // distinct from the deliberate green/red powered-off indication.
    desiredLedMode = btState == BluetoothIndicatorState::On
                         ? StatusLedMode::Connected
                         : (btState == BluetoothIndicatorState::Blinking
                                ? StatusLedMode::Waiting
                                : StatusLedMode::Disconnected);
  }
  if (statusLeds.mode() != desiredLedMode) {
    statusLeds.setMode(desiredLedMode, now);
  }

  illumination.service(systemInputs.doorOpen(), !i2cReserved, now);
  serviceIlluminationSettings(now);
  relays.service(now);
  const uint8_t relayMask = relays.activeRelayMask();
  if (relayMask != lastRelayMask) {
    appEvents.relay(relayMask);
    if (settingsStore.values().relayAudioEnabled() &&
        ((relayMask ^ lastRelayMask) & 0xFAU) != 0) {
      buzzer.beep(35, (relayMask & ~lastRelayMask) != 0 ? 1900 : 1250);
    }
    settingsStore.values().relayRestoreMask = relayMask;
    settingsStore.markDirty(now);
    lastRelayMask = relayMask;
  }
  const ControllerSettings &displaySettings = settingsStore.values();
  display.serviceBrightness(systemInputs.doorOpen()
                                ? displaySettings.displayBrightness
                                : displaySettings.displayClosedBrightness(),
                            now);
  serviceDisplay(now);
  if (!i2cReserved) {
    statusLeds.service(now);
  }
  taskManager.update(now);
  buzzer.update(now);

  if (streamPeriodMs != 0 &&
      static_cast<uint32_t>(now - lastTelemetryAt) >= streamPeriodMs) {
    lastTelemetryAt = now;
    sendTelemetry(0);
  }

  safeReset.service(relays, pwm, now);
}
