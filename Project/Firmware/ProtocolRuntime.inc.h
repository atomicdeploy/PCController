// Implementation fragment compiled once; owns native responses and dispatch.
// -----------------------------------------------------------------------------
// Native UART protocol
// -----------------------------------------------------------------------------

// Reports build identity, record shape, and independently testable capabilities.
void sendHello(uint8_t sequence) {
  constexpr uint32_t capabilities =
      (1UL << 0) |  // INA219
      (1UL << 1) |  // two DS18B20 sensors
      (1UL << 2) |  // 16-channel PWM
      (1UL << 3) |  // relay safety controller
      (1UL << 4) |  // 433 MHz RX/TX, learning, and action mapping
      (1UL << 5) |  // TM1637
      (1UL << 6) |  // I2C LCD
      (1UL << 7) |  // addressable LEDs
      (1UL << 8) |  // persistent settings
      (1UL << 9) |  // menu remote control
      (1UL << 10) | // named temperature identities
      (1UL << 12) | // host display text and asynchronous events
      (1UL << 13) | // exact front-panel snapshot
      (1UL << 14) | // host-injected key lifecycle; Down acts immediately
      (1UL << 15) | // multi/indefinite RF learning
      (1UL << 16) | // bounded generic I2C transaction lease
#if PCCONTROLLER_ENABLE_MENU_DIRECTORY
      (1UL << 17) | // board-authoritative paged menu directory
#endif
      (1UL << 18) | // host-staged learned-RF record replacement (opcode 0x3F)
      (1UL << 19) | // host-captured front-panel session (DisplayText targets 3/4)
      (1UL << 20) | // status bit 12 means buzzer queue/voice is busy
      (1UL << 21) | // EEPROM-selectable 1..255 ms motion break time
      (1UL << 22) | // MCU-timed events/ACKs and queued macro schema 2
#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
      (1UL << 23) | // persistent visible-mask and stable-ID rank permutation
#endif
      (1UL << 24) | // host-owned Idle/Running application state (opcode 0x45)
      (1UL << 25) | // scheduled segment once/loop/interval presentation
      (1UL << 26) | // unsolicited changed-only TM1637 state frames
      (1UL << 27) | // unsolicited buzzer frequency/duration frames
      (1UL << 28) | // MCU-owned procedural status LED effects
      (1UL << 29) | // unsolicited rendered status LED state frames
      (1UL << 30) | // EEPROM-resident condition status profiles
      (1UL << 31) | // checksum-backed operator board name (up to 8 ASCII chars)
      0;
  // HelloPayload is the fixed build identity and capability response.
  struct __attribute__((packed)) HelloPayload {
    uint8_t schema;
    uint8_t boardKind;
    uint32_t capabilities;
    uint32_t buildHash;
    uint32_t buildTimestamp;
  } payload = {3, 1, capabilities,
               pgm_read_dword(&firmwareIdentity.sourceHash),
               pgm_read_dword(&firmwareIdentity.packedTimestamp)};
  appProtocol.send(ControllerProtocol::HelloResponse, sequence,
                   reinterpret_cast<const uint8_t *>(&payload),
                   sizeof(payload));
}

// Serializes the exact packed live telemetry snapshot.
void sendTelemetry(uint8_t sequence) {
  TelemetryPayload payload;
  payload.uptimeMs = now;
  payload.sensors = sensors;
  payload.flags = statusFlags();
  payload.rawInputs = systemInputs.rawInputs();
  payload.activeKeys =
      static_cast<uint8_t>(shiftRegisters.activeInputs() & 0x0F);
  payload.relayMask = relays.activeRelayMask();
  payload.menuPage = menuPage;
  payload.mode = static_cast<uint8_t>(modeManager.current());
  payload.doorOpen = systemInputs.doorOpen();
  payload.bluetoothState =
      static_cast<uint8_t>(systemInputs.bluetoothState(payload.uptimeMs));
  payload.pwmAvailable = pwm.available() ? 1 : 0;
  payload.pwmChannel = pwm.channel();
  payload.pwmValue = pwm.value();
  payload.lcdAddress = 0; // The host publishes its active LCD address.
  payload.pwmErrors = pwm.errorCount();
  payload.framingErrors = appProtocol.framingErrors();
  payload.crcErrors = appProtocol.crcErrors();
  payload.resetCause = resetTelemetry.cause();
  payload.resetCount = resetTelemetry.count();
  appProtocol.send(ControllerProtocol::StatusResponse, sequence,
                   reinterpret_cast<const uint8_t *>(&payload),
                   sizeof(payload));
}

// Reports the MCU-owned EEPROM settings representation.
void sendSettings(uint8_t sequence) {
  const ControllerSettings &settings = settingsStore.values();
  uint8_t payload[26];
  payload[0] = 3;
  memcpy(payload + 1, &settings, ControllerSettingsPrefixSize);
  payload[8] = static_cast<uint8_t>(settings.streamPeriodMs);
  payload[9] = static_cast<uint8_t>(settings.streamPeriodMs >> 8);
  payload[10] = settings.defaultMenuPage;
  payload[11] = settings.menuFlags;
  payload[12] = settings.displayOptions;
  payload[13] = settings.relayRestoreMask;
  payload[14] = settings.motionBreakMs;
  payload[15] = settingsStore.persisted() ? 1U : 0U;
  // The EEPROM length/name record maps directly onto payload bytes 17..25;
  // byte 16 is the public persisted marker.
  payload[16] = settingsStore.boardName(payload + 17) ? 1U : 0U;
  appProtocol.send(ControllerProtocol::SettingsResponse, sequence, payload,
                    static_cast<uint8_t>(18 + payload[17]));
}

// Reports all logical PWM values plus controller availability and selection.
void sendPwmValues(uint8_t sequence) {
  uint8_t payload[34];
  uint8_t index = 0;
  payload[index++] = pwm.available() ? 1 : 0;
  payload[index++] = pwm.channel();
  for (uint8_t channel = 0; channel < PwmChannels::Count; ++channel) {
    appendU16(payload, index, pwm.logicalValue(channel));
  }
  appProtocol.send(ControllerProtocol::PwmValuesResponse, sequence, payload,
                   index);
}

// Pages detected DS18B20 ROM identities, roles, and current readings.
void sendTemperatureList(uint8_t sequence) {
  uint8_t payload[24] = {1, temperatureAddressCount};
  uint8_t index = 2;
  for (uint8_t addressIndex = 0;
       addressIndex < temperatureAddressCount; ++addressIndex) {
    const uint8_t role = temperatureRole(addressIndex);
    payload[index++] = role; // 0=tLED, 1=tBT
    memcpy(payload + index, temperatureAddresses[addressIndex], 8);
    index += 8;
    appendI16(payload, index, sensors.temperatureCentiC[role]);
  }
  appProtocol.send(ControllerProtocol::TemperatureListResponse, sequence,
                   payload, index);
}

// Mirrors physical/host panel segments, keys, LCD metadata, and macro progress.
void sendFrontPanel(uint8_t sequence) {
  uint8_t payload[47];
  payload[0] = 2;
  memcpy(payload + 1, display.rawSegments(), 4);
  payload[5] = display.brightness();
  const ProgramMode mode = modeManager.current();
  const bool blinking =
      mode == MODE_FLASH_MESSAGE || mode == MODE_SAVE_PROMPT ||
      (mode >= MODE_ILLUMINATION_MODE_EDIT &&
       mode <= MODE_USER_RELAY_BEHAVIOR_EDIT);
  payload[6] = static_cast<uint8_t>((blinking ? 1U : 0U) |
                                    ((payload[1] | payload[2] | payload[3] |
                                      payload[4]) != 0
                                         ? 2U
                                         : 0U)
#if PCCONTROLLER_MENU_HIERARCHY
                                    | (menuCategorySelected() ? 4U : 0U)
#endif
                                    );
  payload[7] = 0;
  payload[8] = 0;
  memcpy(payload + 9, hostLcdText, sizeof(hostLcdText));
  payload[41] =
      static_cast<uint8_t>(shiftRegisters.activeInputs() & 0x0FU);
  payload[42] = menuPage;
  payload[43] = static_cast<uint8_t>(mode);
  payload[44] = static_cast<uint8_t>(
      ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0 ? 0x80U : 0U) |
      ((hostPanelMeta >> 12) & 0x0FU));
  payload[45] = static_cast<uint8_t>(hostPanelMeta);
  payload[46] = static_cast<uint8_t>((hostPanelMeta >> 8) & 0x0FU);
  appProtocol.send(ControllerProtocol::FrontPanelResponse, sequence, payload,
                   sizeof(payload));
}

// Pushes only the changed TM1637 state. The full front-panel request remains
// available for initial synchronization and explicit refreshes.
void serviceSegmentPush() {
  const uint8_t *segments = display.rawSegments();
  const uint8_t brightness = display.brightness();
  if (brightness == lastPushedSegmentBrightness &&
      memcmp(segments, lastPushedSegments, 4) == 0) {
    return;
  }
  uint8_t payload[5];
  memcpy(payload, segments, 4);
  payload[4] = brightness;
  memcpy(lastPushedSegments, segments, 4);
  lastPushedSegmentBrightness = brightness;
  appProtocol.send(ControllerProtocol::SegmentChanged, 0, payload,
                   sizeof(payload));
}

// Mirrors the exact note/pause dequeued by Timer1 playback. This event never
// drives local audio and therefore cannot add jitter to the hardware waveform.
void serviceBuzzerPush() {
  const uint8_t revision = buzzer.revision();
  if (revision == lastPushedBuzzerRevision) {
    return;
  }
  lastPushedBuzzerRevision = revision;
  uint8_t payload[5];
  payload[0] = static_cast<uint8_t>(buzzer.activeFrequencyHz());
  payload[1] = static_cast<uint8_t>(buzzer.activeFrequencyHz() >> 8);
  payload[2] = static_cast<uint8_t>(buzzer.activeDurationMs());
  payload[3] = static_cast<uint8_t>(buzzer.activeDurationMs() >> 8);
  payload[4] = buzzer.muted() ? 1U : 0U;
  appProtocol.send(ControllerProtocol::BuzzerChanged, 0, payload,
                   sizeof(payload));
}

// Mirrors the physical PWM RGB result after the board compositor has applied
// local safety priority, cues, brightness, and a host-requested effect.
void serviceStatusLedPush() {
  uint8_t payload[6] = {
      statusLeds.renderedRed(), statusLeds.renderedGreen(),
      statusLeds.renderedBlue(), statusLeds.brightness(),
      static_cast<uint8_t>(statusLeds.effect()), statusLeds.condition()};
  if (memcmp(payload, lastPushedStatusLed, sizeof(payload)) == 0) {
    return;
  }
  memcpy(lastPushedStatusLed, payload, sizeof(payload));
  appProtocol.send(ControllerProtocol::StatusLedChanged, 0, payload,
                   sizeof(payload));
}

#if PCCONTROLLER_ENABLE_MENU_DIRECTORY
// Reports one built-in page's stable ID, parent category, flags, and label.
void sendMenuList(uint8_t sequence, uint8_t cursor) {
  uint8_t payload[46] = {1, PAGE_COUNT, 0xFF, 0};
  uint8_t index = 4;
  while (cursor < PAGE_COUNT && payload[3] < 7) {
    payload[index++] = cursor;
    payload[index++] = static_cast<uint8_t>(pageToMode(cursor));
    for (uint8_t character = 0; character < 4; ++character) {
      payload[index++] = pgm_read_byte(
          MenuLabels + static_cast<uint8_t>(cursor * 4U + character));
    }
    ++cursor;
    ++payload[3];
  }
  if (cursor < PAGE_COUNT) {
    payload[2] = cursor;
  }
  appProtocol.send(ControllerProtocol::MenuListResponse, sequence, payload,
                   index);
}
#endif

#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
// Reports the compact schema-2 visibility mask and packed presentation order.
void sendMenuLayout(uint8_t sequence) {
  const ControllerSettings &settings = settingsStore.values();
  uint8_t payload[4 + PersistentMenuOrderWireBytes] = {
      2, PAGE_COUNT, static_cast<uint8_t>(settings.visibleMenuMask),
      static_cast<uint8_t>(settings.visibleMenuMask >> 8)};
  memcpy(payload + 4, settings.menuOrder, PersistentMenuOrderWireBytes);
  appProtocol.send(ControllerProtocol::MenuLayoutResponse, sequence, payload,
                   sizeof(payload));
}

// Validates the canonical schema-2 prefix and ignores appended extension data.
bool applyMenuLayout(const uint8_t *payload, uint8_t length, uint32_t at) {
  if (length < static_cast<uint8_t>(4 + PersistentMenuOrderWireBytes) ||
      payload[0] != 2 ||
      payload[1] != PAGE_COUNT) {
    return false;
  }
  const uint16_t visibleMask = readU16(payload + 2);
  const uint8_t firstVisible =
      firstVisiblePersistentMenuPage(visibleMask, payload + 4);
  if (firstVisible == 0xFF) {
    return false;
  }
  ControllerSettings &settings = settingsStore.values();
  settings.visibleMenuMask = visibleMask;
  memcpy(settings.menuOrder, payload + 4, PersistentMenuOrderWireBytes);
  if (!settings.menuPageVisible(settings.defaultMenuPage)) {
    settings.defaultMenuPage = firstVisible;
  }
  if (!settings.menuPageVisible(menuPage)) {
    if (modeManager.current() == MODE_MOTION_CONTROL) {
      relays.allOff(at);
    }
    setMenuPage(firstVisible);
  }
  settingsStore.markDirty(at);
  return true;
}
#endif

// Generic I2C access is deliberately bounded. A short cooperative lease
// pauses firmware-owned polling while the PC reads or writes any bus address.
// Executes write, read, or repeated-start transfer and returns byte-level status.
void transferI2c(uint8_t sequence, const uint8_t *request, uint8_t length,
                 uint32_t at) {
  if (length < 4) {
    appProtocol.sendError(sequence, ControllerProtocol::I2cTransfer,
                          ControllerProtocol::BadPayload);
    return;
  }
  const uint8_t address = request[0];
  const uint8_t leaseSeconds = request[1];
  const uint8_t writeLength = request[2];
  const uint8_t readLength = request[3];
  if (address == 0) {
    i2cLeaseAddress = 0;
    appProtocol.sendAck(sequence, ControllerProtocol::I2cTransfer);
    return;
  }
  if (leaseSeconds > 10 || writeLength > 16 || readLength > 16 ||
      length < static_cast<uint8_t>(4 + writeLength)) {
    appProtocol.sendError(sequence, ControllerProtocol::I2cTransfer,
                          ControllerProtocol::BadPayload);
    return;
  }
  if (leaseSeconds != 0) {
    i2cLeaseAddress = address;
    i2cLeaseUntil = static_cast<uint16_t>(
        at + static_cast<uint16_t>(leaseSeconds) * 1000U);
  }

  uint8_t response[19];
  response[0] = 0;
  response[1] = address;
  response[2] = 0;
  if (writeLength != 0 || readLength == 0) {
    i2cBus.beginTransmission(address);
    i2cBus.write(request + 4, writeLength);
    response[0] = i2cBus.endTransmission(readLength == 0);
    if (response[0] == 0 && writeLength != 0 &&
        (address == 0x27 || address == 0x3F)) {
      hostLcdAddress = address;
    }
  }
  if (response[0] == 0 && readLength != 0) {
    (void)i2cBus.requestFrom(address, readLength);
    while (response[2] < readLength && i2cBus.available() > 0) {
      response[3 + response[2]++] = i2cBus.read();
    }
  }
  appProtocol.send(ControllerProtocol::I2cTransferResponse, sequence, response,
                   static_cast<uint8_t>(3 + response[2]));
}

// Pages EEPROM-backed learned RF entries without allocating a list in SRAM.
void sendLearnedRemotes(uint8_t sequence, uint8_t cursor) {
  uint8_t payload[40] = {1, learnedRemotes.count(), 0xFF, 0};
  uint8_t scan = cursor;
  uint8_t index = 4;
  LearnedRemote remote;
  while (scan < RemoteLearningStore::Capacity && payload[3] < 3) {
    if (learnedRemotes.get(scan, remote)) {
      memcpy(payload + index, &remote, sizeof(remote));
      index = static_cast<uint8_t>(index + sizeof(remote));
      ++payload[3];
    }
    ++scan;
  }
  while (scan < RemoteLearningStore::Capacity) {
    if (learnedRemotes.get(scan, remote)) {
      payload[2] = scan;
      break;
    }
    ++scan;
  }
  appProtocol.send(ControllerProtocol::RadioLearnListResponse, sequence,
                   payload, index);
}

// Applies the canonical settings prefix plus its exact optional board-name
// tail; all other positional tails are rejected.
bool applySettings(const uint8_t *payload, uint8_t length, uint32_t at) {
  const bool hasBoardName = length != 15;
  if ((hasBoardName &&
       (length < 16 || length != static_cast<uint8_t>(16 + payload[15]))) ||
      payload[0] != 3 || payload[2] > 2 || payload[5] > 7 ||
      payload[14] == 0 ||
      (payload[7] & ~OutputPersistence::AllowedMask) != 0
#if PCCONTROLLER_MENU_VISIBILITY
      || !settingsStore.values().menuPageVisible(payload[10])
#endif
      || payload[10] >= PAGE_COUNT
      ) {
    return false;
  }
  const uint16_t newStreamPeriod = readU16(payload + 8);
  if (newStreamPeriod != 0 && newStreamPeriod < 100) {
    return false;
  }
  if (hasBoardName &&
      !settingsStore.setBoardName(payload + 16, payload[15])) {
    return false;
  }

  ControllerSettings &settings = settingsStore.values();
  const bool wasProgramming = settings.programmingMode();
  memcpy(&settings, payload + 1, ControllerSettingsPrefixSize);
  settings.flags &=
      SettingsFlags::Silent |
      SettingsFlags::ProgrammingMode |
      SettingsFlags::SwapTemperatureSensors |
      SettingsFlags::MotionDoorPolicyMask |
      SettingsFlags::DoorAudioDisabled |
      SettingsFlags::RelayAudioDisabled;
  settings.streamPeriodMs = newStreamPeriod;
  settings.defaultMenuPage = payload[10];
  settings.menuFlags = payload[11];
  settings.displayOptions = payload[12];
  settings.relayRestoreMask = payload[13];
  settings.motionBreakMs = payload[14];

  applyStoredSettings(at);
  if (wasProgramming && !settings.programmingMode()) {
    restoreStoredOutputs(at);
  }
  settingsStore.saveNow();
  return true;
}

// Central synchronous opcode dispatcher; every path ACKs, responds, or errors.
void handleProtocolFrame(const ControllerProtocol::Frame &frame,
                         void *context) {
  using namespace ControllerProtocol;
  const uint8_t *payload = frame.payload;
  const uint8_t length = frame.payloadLength;
  now = millis();
  const uint32_t frameNow = now;
  // Internal boot records intentionally share normal opcode validation and
  // peripheral dispatch, but they are not UART traffic and must neither forge
  // host activity nor emit unsolicited ACK/error frames during startup.
#if PCCONTROLLER_ENABLE_EEPROM_BOOT_OPCODES
  const bool internalBootFrame =
      BootOpcodeSequence::isExecutionContext(context);
#else
  static_cast<void>(context);
  const bool internalBootFrame = false;
#endif
  if (!internalBootFrame) {
    lastHostActivityAt = frameNow;
    hostLcdFlags = static_cast<uint8_t>(
        (hostLcdFlags | HOST_SEEN) & ~HOST_LCD_OFFLINE);
  }

  if (!firmwareReady && frame.opcode != Hello) {
    goto busy;
  }
  if (editTransactionActive &&
      (frame.opcode == SetStreamPeriod ||
       frame.opcode == SetSettings ||
#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
       frame.opcode == MenuLayoutSet ||
#endif
       frame.opcode == PwmSet)) {
    goto busy;
  }
  switch (frame.opcode) {
    case Hello:
      sendHello(frame.sequence);
      return;

    case GetStatus:
      sendTelemetry(frame.sequence);
      return;

    case SetStreamPeriod: {
      const uint16_t period = length >= 2 ? readU16(payload) : 0;
      if (length < 2 || (period != 0 && period < 100)) {
        goto badPayload;
      }
      streamPeriodMs = period;
      settingsStore.values().streamPeriodMs = streamPeriodMs;
      settingsStore.markDirty(frameNow);
      goto acknowledged;
    }

    case GetSettings:
      sendSettings(frame.sequence);
      return;

    case SetSettings:
      if (!applySettings(payload, length, frameNow)) {
        goto badPayload;
      }
      goto acknowledged;

    case TemperatureList:
      sendTemperatureList(frame.sequence);
      return;

    case FrontPanelGet:
      sendFrontPanel(frame.sequence);
      return;

    case MenuList:
#if PCCONTROLLER_ENABLE_MENU_DIRECTORY
      if (length < 1 || payload[0] >= PAGE_COUNT) {
        goto badPayload;
      }
      sendMenuList(frame.sequence, payload[0]);
      return;
#else
      goto unsupported;
#endif

    case MenuLayoutGet:
#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
      sendMenuLayout(frame.sequence);
      return;
#else
      goto unsupported;
#endif

    case MenuLayoutSet:
#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
      if (!applyMenuLayout(payload, length, frameNow)) {
        goto badPayload;
      }
      goto acknowledged;
#else
      goto unsupported;
#endif

    case I2cTransfer:
      transferI2c(frame.sequence, payload, length, frameNow);
      return;

    case Buzzer:
      if (length < 4) {
        goto badPayload;
      }
      buzzer.beep(readU16(payload + 2), readU16(payload));
      goto acknowledged;

    case PwmSet: {
      const uint16_t value = length >= 3 ? readU16(payload + 1) : 0;
      if (length < 3 || payload[0] >= PwmChannels::Count || value > 4095) {
        goto badPayload;
      }
      if (!pwm.setLogical(payload[0], value)) {
        goto hardwareUnavailable;
      }
      if (payload[0] < 8) {
        storeUserPwmValue(payload[0], value);
      }
      goto acknowledged;
    }

    case PwmAllOff:
      if (!pwm.tryAllOff()) {
        goto hardwareUnavailable;
      }
      goto acknowledged;

    case StatusRgb:
      if (length < 4) {
        goto badPayload;
      }
      hostLcdFlags |= HOST_STATUS_OVERRIDE;
      statusLeds.setBrightness(payload[3]);
      statusLeds.setCustom(payload[0], payload[1], payload[2]);
      goto acknowledged;

    case StatusEffect:
      // [kind][RGB A][RGB B][brightness][minimum brightness][period u16]
      // [repeats]. Repeats zero loops; 1..255 are MCU-counted cycles.
      if (length == 1 && payload[0] == 0) {
        hostLcdFlags &= static_cast<uint8_t>(~HOST_STATUS_OVERRIDE);
        statusLeds.cancelEffect();
        goto acknowledged;
      }
      if (length < 12 || payload[0] == 0 || payload[0] > 4 ||
          !statusLeds.setEffect(
              static_cast<StatusLedEffect>(payload[0]), payload[1], payload[2],
              payload[3], payload[4], payload[5], payload[6], payload[7],
              payload[8], readU16(payload + 9), payload[11], frameNow)) {
        goto badPayload;
      }
      hostLcdFlags |= HOST_STATUS_OVERRIDE;
      goto acknowledged;

    case StatusProfileGet: {
      if (length < 1 || payload[0] >= StatusLedController::ProfileCount) {
        goto badPayload;
      }
      uint8_t response[2 + StatusLedController::ProfilePayloadBytes];
      response[0] = payload[0];
      response[1 + StatusLedController::ProfilePayloadBytes] =
          statusLeds.profile(payload[0], response + 1) ? 1U : 0U;
      appProtocol.send(ControllerProtocol::StatusProfileResponse,
                       frame.sequence, response, sizeof(response));
      return;
    }

    case StatusProfileSet:
      if (length < 1 + StatusLedController::ProfilePayloadBytes ||
          !statusLeds.setProfile(payload[0], payload + 1, frameNow)) {
        goto badPayload;
      }
      goto acknowledged;

    case ProgramState:
      // Only the semantic one-byte prefix is required; future appended state
      // metadata is deliberately ignored by this small MCU implementation.
      if (length < 1 || payload[0] > 1) {
        goto badPayload;
      }
      if (payload[0] == 0) {
        hostLcdFlags &= static_cast<uint8_t>(~HOST_PROGRAM_RUNNING);
      } else {
        hostLcdFlags |= HOST_PROGRAM_RUNNING;
      }
      hostLcdFlags &= static_cast<uint8_t>(~HOST_STATUS_OVERRIDE);
      statusLeds.cancelEffect();
      goto acknowledged;

    case PwmGet:
      sendPwmValues(frame.sequence);
      return;

    case AddressableLed: {
      // [pixel 0..10, or 0xFF=fill][R][G][B][brightness].
      if (length < 5 ||
          (payload[0] != 0xFF &&
           payload[0] >= AddressableLeds::PixelCount)) {
        goto badPayload;
      }
      const RgbColor color(payload[1], payload[2], payload[3]);
      if (payload[0] == 0xFF) {
        AddressableLeds::fill(color);
      } else {
        AddressableLeds::buffer()[payload[0]] = color;
      }
      AddressableLeds::show();
      goto acknowledged;
    }

    case RadioTransmit:
      if (length < 8 ||
          !transmitRadio(readU32(payload), payload[4], payload[5],
                         readU16(payload + 6))) {
        goto badPayload;
      }
      goto acknowledged;

    case RadioLearnStart:
      if (length != 2 || payload[0] > RF_LEARN_TIMER ||
          (payload[0] == RF_LEARN_INDEFINITE && payload[1] != 0) ||
          (payload[0] == RF_LEARN_TIMER &&
           (payload[1] == 0 || payload[1] > MAX_LEARNING_SECONDS))) {
        goto badPayload;
      }
      beginLearning(payload[0], payload[1]);
      goto acknowledged;

    case RadioLearnCancel:
      endLearning(1, 0);
      goto acknowledged;

    case RadioLearnClear:
      endLearning(1, 0);
      learnedRemotes.clear();
      goto acknowledged;

    case RadioLearnList:
      if (length < 1 || payload[0] >= RemoteLearningStore::Capacity) {
        goto badPayload;
      }
      sendLearnedRemotes(frame.sequence, payload[0]);
      return;

    case RadioLearnRemove:
      if (length < 1 || !learnedRemotes.remove(payload[0])) {
        goto badPayload;
      }
      goto acknowledged;

    case RadioLearnReplace: {
      if (length < sizeof(LearnedRemote)) {
        goto badPayload;
      }
      LearnedRemote remote;
      memcpy(&remote, payload, sizeof(remote));
      if (!learnedRemotes.replace(remote)) {
        goto badPayload;
      }
      goto acknowledged;
    }

    case ControllerProtocol::MenuAction:
      if (length < 1 || payload[0] > MENU_INCREASE) {
        goto badPayload;
      }
      handleMenuAction(payload[0], true);
      goto acknowledged;

    case RemoteKeyGesture:
      if (length < 2 || payload[0] > MENU_INCREASE ||
          payload[1] > static_cast<uint8_t>(KeyEvent::Up)) {
        goto badPayload;
      }
      applyKeyGesture(payload[0], static_cast<KeyEvent>(payload[1]));
      appEvents.key(payload[0], payload[1], InputEventSource::Host);
      goto acknowledged;

    case MenuSetPage:
      if (length < 1 || payload[0] >= PAGE_COUNT) {
        goto badPayload;
      }
      if (modeManager.current() == MODE_MOTION_CONTROL) {
        relays.allOff(frameNow);
      }
      if (editTransactionActive) {
        restoreEditTransaction(frameNow);
        editTransactionActive = false;
      }
      setMenuPage(payload[0]);
      goto acknowledged;

    case DisplayText: {
      if (length < 4 || payload[0] > 5 || payload[3] > 40 ||
          (payload[0] != 5 &&
           length < static_cast<uint8_t>(4 + payload[3])) ||
          (payload[0] == 5 &&
           (length < 8 ||
            length < static_cast<uint8_t>(8 + payload[3]) ||
            (payload[4] & 0x03U) > 2 ||
            (payload[4] & 0x7CU) != 0 ||
            ((payload[4] & 0x03U) == 2 && payload[7] == 0))) ||
          (payload[0] == 3 && (payload[3] < 4 || payload[3] > 36)) ||
          (payload[0] == 4 && payload[3] != 0)) {
        goto badPayload;
      }
      const uint8_t target = payload[0];
      const uint16_t duration = readU16(payload + 1);
      const uint8_t textLength = payload[3];
      const bool scheduledSegments = target == 5;
      const uint8_t textOffset = scheduledSegments ? 8 : 4;
      if (target == 4) {
        releaseHostPanel();
        goto acknowledged;
      }
      if (target == 3) {
        hostLcdFlags |= HOST_PANEL_CAPTURED;
        hostPanelMeta = duration; // high nibble=state, low 12 bits=value
      }
      if (target == 0 || target == 2 || target == 3 || scheduledSegments) {
        hostSegmentTextActive = textLength != 0;
        hostSegmentScrollIndex = 0;
        hostSegmentOptions = scheduledSegments ? payload[4] : 0;
        hostSegmentHoldMs = scheduledSegments ? readU16(payload + 5) : duration;
        hostSegmentIntervalSeconds = scheduledSegments ? payload[7] : 0;
        const bool scrolling =
            (scheduledSegments && ((hostSegmentOptions & 0x80U) != 0 ||
                                   textLength > 4)) ||
            (!scheduledSegments && target == 0 && textLength > 4);
        if (!scheduledSegments && scrolling) {
          hostSegmentOptions = 1; // legacy long text remains an explicit loop
        }
        const uint8_t copyLength = scrolling
                                       ? textLength
                                       : (textLength > 4 ? 4 : textLength);
        hostSegmentTextLength = copyLength;
        memset(hostSegmentText, scrolling ? 0 : ' ',
               sizeof(hostSegmentText));
        if (copyLength != 0) {
          memcpy(hostSegmentText, payload + textOffset, copyLength);
        }
        if (scrolling) {
          hostSegmentStepMs = duration == 0 ? 260 :
              (duration < 80 ? 80 : duration);
          hostSegmentTextEndsAt = frameNow + hostSegmentStepMs;
        } else {
          const uint16_t hold = scheduledSegments ? hostSegmentHoldMs : duration;
          hostSegmentTextEndsAt = target == 3 || hold == 0 ||
                                          (hostSegmentOptions & 0x03U) == 1
                                      ? 0
                                      : frameNow + hold;
        }
      }
      if (target == 1 || target == 2 || target == 3) {
        memset(hostLcdText, ' ', sizeof(hostLcdText));
        uint8_t lcdLength =
            target == 3 ? static_cast<uint8_t>(textLength - 4) : textLength;
        if (lcdLength > sizeof(hostLcdText)) {
          lcdLength = sizeof(hostLcdText);
        }
        memcpy(hostLcdText, payload + (target == 3 ? 8 : 4), lcdLength);
      }
      goto acknowledged;
    }

    case MacroStart:
    case MacroCancel:
    case MacroStep:
      macroPlayback.handle(frame);
      if (macroPlayback.takeSafeStopRequest()) {
        safeStopMacroOutputs();
      }
      return;

    case RelaySet:
      if (length < 2 || payload[0] > 7 || payload[1] > 1) {
        goto badPayload;
      }
      if (!relays.requestRelayForTest(static_cast<uint8_t>(payload[0] + 1),
                                      payload[1] != 0, frameNow)) {
        goto unsafe;
      }
      goto acknowledged;

    case ControllerProtocol::RelaySide:
      if (length < 2 || payload[0] > 1 || payload[1] > 2) {
        goto badPayload;
      }
      if (payload[1] == 0) {
        relays.stopSide(static_cast<::RelaySide>(payload[0]), frameNow);
      } else {
        if (!relays.requestSide(
                static_cast<::RelaySide>(payload[0]),
                payload[1] == 1 ? RelayDirection::Forward
                                : RelayDirection::Reverse,
                true, frameNow)) {
          goto unsafe;
        }
      }
      goto acknowledged;

    case RelayAllOff:
      relays.allOff(frameNow);
      goto acknowledged;

    case Reset:
      if (length < 1 || payload[0] > 1) {
        goto badPayload;
      }
      endLearning(1, 0);
      stopRemoteMomentary(frameNow);
      buzzer.stop();
      safeReset.request(relays, pwm, statusLeds, frameNow);
      goto acknowledged;

    default:
      goto unsupported;
  }

acknowledged:
  if (!internalBootFrame) {
    appProtocol.sendAck(frame.sequence, frame.opcode);
  }
  return;
badPayload:
  if (!internalBootFrame) {
    appProtocol.sendError(frame.sequence, frame.opcode, BadPayload);
  }
  return;
hardwareUnavailable:
  if (!internalBootFrame) {
    appProtocol.sendError(frame.sequence, frame.opcode, HardwareUnavailable);
  }
  return;
unsafe:
  if (!internalBootFrame) {
    appProtocol.sendError(frame.sequence, frame.opcode, Unsafe);
  }
  return;
busy:
  if (!internalBootFrame) {
    appProtocol.sendError(frame.sequence, frame.opcode, Busy);
  }
  return;
unsupported:
  if (!internalBootFrame) {
    appProtocol.sendError(frame.sequence, frame.opcode, Unsupported);
  }
}
