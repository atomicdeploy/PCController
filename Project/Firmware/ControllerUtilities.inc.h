// Implementation fragment compiled once; provides wrap-safe and wire helpers.
// -----------------------------------------------------------------------------
// Utility helpers
// -----------------------------------------------------------------------------

// Tests a 32-bit deadline without breaking across millis() rollover.
bool __attribute__((noinline)) timeReached(uint32_t at,
                                           uint32_t deadline) {
  return static_cast<int32_t>(at - deadline) >= 0;
}

// Expires the host's bounded cooperative I2C lease using its 16-bit deadline.
bool i2cLeaseActive(uint32_t at) {
  if (i2cLeaseAddress != 0 &&
      static_cast<int16_t>(static_cast<uint16_t>(at) - i2cLeaseUntil) >= 0) {
    i2cLeaseAddress = 0;
  }
  return i2cLeaseAddress != 0;
}

// Evaluates the EEPROM door policy against the current debounced reed input.
bool motionPolicyAllows() {
  return motionDoorPolicyAllows(settingsStore.values().motionDoorPolicy(),
                                systemInputs.doorOpen());
}

// Rounds milli-units into the configured 0..2 decimal fixed-point value.
int32_t scaledMilliValue(int32_t value, uint8_t decimalPlaces) {
  const uint16_t divisor =
      decimalPlaces == 0 ? 1000U : (decimalPlaces == 1 ? 100U : 10U);
  const int32_t rounding = divisor / 2U;
  return value >= 0 ? (value + rounding) / divisor
                    : (value - rounding) / divisor;
}

// Loads EEPROM once, then mirrors persisted illumination fields into the controller.
void loadIlluminationSettings() {
  settingsStore.begin(now);
  const ControllerSettings &settings = settingsStore.values();
  illumination.setMode(
      static_cast<IlluminationMode>(settings.illuminationMode));
  illumination.setOnBrightness(settings.illuminationOnBrightness);
  illumination.setOffBrightness(settings.illuminationOffBrightness);
}

// Copies edited illumination values back to MCU-owned settings and marks them dirty.
void markIlluminationSettingsChanged(uint32_t at) {
  ControllerSettings &settings = settingsStore.values();
  settings.illuminationMode = static_cast<uint8_t>(illumination.mode());
  settings.illuminationOnBrightness = illumination.onBrightness();
  settings.illuminationOffBrightness = illumination.offBrightness();
  if (!editTransactionActive) {
    settingsStore.markDirty(at);
  }
}

// Defers EEPROM writes during timing-sensitive RF learning and menu transactions.
void serviceIlluminationSettings(uint32_t at) {
  settingsStore.service(at, !learningActive && !editTransactionActive);
}

// Clears every scheduled segment state so a later message always starts at
// its first frame instead of inheriting an interval wait or repeat mode.
void clearHostSegmentText() {
  hostSegmentTextActive = false;
  hostSegmentTextLength = 0;
  hostSegmentScrollIndex = 0;
  hostSegmentOptions = 0;
  hostSegmentTextEndsAt = 0;
}

// Releases a host overlay and restores the configured local default page.
void releaseHostPanel() {
  hostLcdFlags &= static_cast<uint8_t>(~HOST_PANEL_CAPTURED);
  hostPanelMeta = 0;
  clearHostSegmentText();
  setMenuPage(settingsStore.values().defaultMenuPage);
}

// The PC preloads a fixed offline page in hidden LCD DDRAM. On abrupt heartbeat
// loss the MCU needs only shift that page into view; no scanner, strings, or
// full HD44780 renderer are duplicated in scarce flash.
void showHostOfflineOnLcd() {
  if (hostLcdAddress == 0) {
    return;
  }
  // Two HD44780 command nibbles expanded into PCF8574 enable-low/high/low
  // pulses. This stays in RAM because CompactI2c's bulk write dereferences an
  // SRAM pointer; moving it to PROGMEM requires a flash-aware write path.
  static const uint8_t shiftCommand[] = {
      0x18, 0x1C, 0x18, 0x88, 0x8C, 0x88,
  };
  for (uint8_t batch = 0; batch < 4; ++batch) {
    i2cBus.beginTransmission(hostLcdAddress);
    const uint8_t commands = batch == 3 ? 1 : 5;
    for (uint8_t index = 0; index < commands; ++index) {
      // PCF8574 pulses for HD44780 command 0x18 (shift display left).
      i2cBus.write(shiftCommand, sizeof(shiftCommand));
    }
    (void)i2cBus.endTransmission();
  }
}

// Appends little-endian integers without allocating a serializer object.
void appendU16(uint8_t *buffer, uint8_t &index, uint16_t value) {
  buffer[index++] = static_cast<uint8_t>(value);
  buffer[index++] = static_cast<uint8_t>(value >> 8);
}

// Appends a signed 16-bit value with the same little-endian wire representation.
void appendI16(uint8_t *buffer, uint8_t &index, int16_t value) {
  appendU16(buffer, index, static_cast<uint16_t>(value));
}

// Reads an unaligned little-endian 16-bit protocol value.
uint16_t readU16(const uint8_t *buffer) {
  return static_cast<uint16_t>(buffer[0]) |
         (static_cast<uint16_t>(buffer[1]) << 8);
}

// Reads an unaligned little-endian 32-bit protocol value.
uint32_t readU32(const uint8_t *buffer) {
  return static_cast<uint32_t>(buffer[0]) |
         (static_cast<uint32_t>(buffer[1]) << 8) |
         (static_cast<uint32_t>(buffer[2]) << 16) |
         (static_cast<uint32_t>(buffer[3]) << 24);
}

// Returns relays and user PWM channels to a safe state after macro termination.
void safeStopMacroOutputs() {
  relays.allOff(now);
  pwm.clearMask(PwmChannels::UserTestMask);
  hostLcdFlags &= static_cast<uint8_t>(~HOST_STATUS_OVERRIDE);
  statusLeds.cancelEffect();
}

// Treats a never-seen or timed-out PC as unavailable after firmware startup.
bool hostUnavailable() {
  return firmwareReady &&
         (((hostLcdFlags & HOST_SEEN) == 0) ||
          static_cast<uint32_t>(now - lastHostActivityAt) > HOST_OFFLINE_MS);
}

// Collapses both named temperature channels into the safety warning state.
bool temperatureHot() {
  return sensors.temperatureCentiC[0] >= HOT_TEMPERATURE_CENTI_C ||
         sensors.temperatureCentiC[1] >= HOT_TEMPERATURE_CENTI_C;
}

// Composes the compact availability/activity bitmap used by StatusResponse.
uint16_t statusFlags() {
  uint16_t flags = 0;
  if (ina219Available) {
    flags |= STATUS_INA219;
  }
  if (pwm.available()) {
    flags |= STATUS_PWM;
  }
  if (sensors.temperatureCentiC[0] != INVALID_I16) {
    flags |= STATUS_TLED;
  }
  if (sensors.temperatureCentiC[1] != INVALID_I16) {
    flags |= STATUS_TBT;
  }
  if (learnedRemotes.count() != 0) {
    flags |= STATUS_RF_LEARNED;
  }
  if (learningActive) {
    flags |= STATUS_RF_LEARNING;
  }
  if (streamPeriodMs != 0) {
    flags |= STATUS_STREAMING;
  }
  if (radioState.lastCode != 0) {
    flags |= STATUS_RF_RECEIVED;
  }
  if (settingsStore.values().silent()) {
    flags |= STATUS_SILENT;
  }
  if (relays.anySideBusy()) {
    flags |= STATUS_RELAY_BUSY;
  }
  if (systemInputs.doorOpen()) {
    flags |= STATUS_DOOR_OPEN;
  }
  if (buzzer.isBusy()) {
    flags |= STATUS_BUZZER_BUSY;
  }
  if ((hostLcdFlags & HOST_PROGRAM_RUNNING) != 0) {
    flags |= STATUS_PROGRAM_RUNNING;
  }
  if (hostUnavailable()) {
    flags |= STATUS_HOST_OFFLINE;
  }
  if (temperatureHot()) {
    flags |= STATUS_HOT;
  }
  return flags;
}
