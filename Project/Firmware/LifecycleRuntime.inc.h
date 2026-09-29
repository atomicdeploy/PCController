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
  menuPage = settings.defaultMenuPage;
  buzzer.setMuted(settings.silent());
  streamPeriodMs = settings.streamPeriodMs;
  display.begin(settings.displayBrightness);
  display.showText(commonText(TextBoot));

  for (Key &key : menuKeys) {
    key.setPressCallback(keyPressed);
    key.setReleaseCallback(keyReleased);
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
  illumination.begin(pwm, systemInputs.doorOpen(), now);
  statusLeds.begin(pwm, settings.statusBrightness, now);
  applyStoredSettings(now);
  appProtocol.service();
  wdt_reset();

  temperatureBus.begin();
  temperatureBus.setWaitForConversion(false);
  discoverTemperatureSensors();
  requestTemperatures(now);
  appProtocol.service();
  wdt_reset();

  learnedRemotes.begin();
  radioTransmitter.enableTransmit(BoardPins::RcTransmit);
  radioReceiver.setReceiveTolerance(70);
  radioReceiver.enableReceive(digitalPinToInterrupt(BoardPins::RcReceive));

  playBootMelody();
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

  serviceRadio();
  appProtocol.service();
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
  const bool hostOffline = (hostLcdFlags & HOST_SEEN) != 0 &&
                           static_cast<uint32_t>(now - lastHostActivityAt) >
                               HOST_OFFLINE_MS;
  if (hostOffline && (hostLcdFlags & HOST_LCD_OFFLINE) == 0) {
    if ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0) {
      releaseHostPanel();
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

  const bool hot =
      sensors.temperatureCentiC[0] >= HOT_TEMPERATURE_CENTI_C ||
      sensors.temperatureCentiC[1] >= HOT_TEMPERATURE_CENTI_C;
  if (modeManager.current() == MODE_BOOT) {
    if (statusLeds.mode() != StatusLedMode::Boot) {
      statusLeds.setMode(StatusLedMode::Boot, now);
    }
  } else if (modeManager.current() == MODE_FAULT || hostOffline) {
    if (statusLeds.mode() != StatusLedMode::Fault) {
      statusLeds.setMode(StatusLedMode::Fault, now);
    }
  } else if (learningActive) {
    if (statusLeds.mode() != StatusLedMode::Learning) {
      statusLeds.setMode(StatusLedMode::Learning, now);
    }
  } else if (hot) {
    if (statusLeds.mode() != StatusLedMode::Warning) {
      statusLeds.setMode(StatusLedMode::Warning, now);
    }
  } else if (statusLeds.mode() == StatusLedMode::Boot ||
             statusLeds.mode() == StatusLedMode::Learning ||
             statusLeds.mode() == StatusLedMode::Warning ||
             statusLeds.mode() == StatusLedMode::Fault) {
    statusLeds.setMode(StatusLedMode::Ready, now);
  }

  illumination.service(systemInputs.doorOpen(),
                       !i2cReserved && !learningActive, now);
  serviceIlluminationSettings(now);
  if (!i2cReserved && modeManager.current() != MODE_BOOT) {
    pwm.service(now);
  }
  uint8_t pwmChannel;
  if (pwm.consumeAutoChannelChange(pwmChannel)) {
    appEvents.pwmChannel(pwmChannel);
  }
  relays.service(now);
  const uint8_t relayMask = relays.activeRelayMask();
  if (relayMask != lastRelayMask) {
    appEvents.relay(relayMask);
    if (settingsStore.values().relayAudioEnabled() &&
        ((relayMask ^ lastRelayMask) & 0xFAU) != 0) {
      buzzer.beep(35, (relayMask & ~lastRelayMask) != 0 ? 1900 : 1250);
    }
    lastRelayMask = relayMask;
  }
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
