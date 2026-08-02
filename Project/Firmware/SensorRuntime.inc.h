// Implementation fragment compiled once; owns I2C recovery and sensor sampling.
// -----------------------------------------------------------------------------
// Sensors and PWM
// -----------------------------------------------------------------------------

// Releases a stuck fixed-port I2C bus with nine clocks and a generated STOP.
bool prepareI2cBus() {
  constexpr uint8_t sda = _BV(PORTC4);
  constexpr uint8_t scl = _BV(PORTC5);
  // Release both open-drain lines with their weak pull-ups enabled.
  PORTC |= static_cast<uint8_t>(sda | scl);
  DDRC &= static_cast<uint8_t>(~(sda | scl));
  delayMicroseconds(10);

  if ((PINC & scl) == 0) {
    return false;
  }

  // Let a slave stranded mid-byte advance and release SDA before TWI starts.
  if ((PINC & sda) == 0) {
    for (uint8_t pulse = 0; pulse < 9; ++pulse) {
      PORTC &= static_cast<uint8_t>(~scl);
      DDRC |= scl;
      delayMicroseconds(5);
      PORTC |= scl;
      DDRC &= static_cast<uint8_t>(~scl);
      delayMicroseconds(5);
      if ((PINC & scl) == 0) {
        return false;
      }
    }

    // Generate a STOP condition after the recovery clocks.
    PORTC &= static_cast<uint8_t>(~sda);
    DDRC |= sda;
    delayMicroseconds(5);
    PORTC |= scl;
    DDRC &= static_cast<uint8_t>(~scl);
    delayMicroseconds(5);
    PORTC |= sda;
    DDRC &= static_cast<uint8_t>(~sda);
    delayMicroseconds(5);
  }

  return (PINC & static_cast<uint8_t>(sda | scl)) ==
         static_cast<uint8_t>(sda | scl);
}

// Restores the active-high/low MODE2 contract after PWM startup.
bool normalizePwmMode2() {
  constexpr uint8_t expectedMode2 = PwmController::recommendedMode2();
  constexpr uint8_t PwmMode2Register = 0x01;
  i2cBus.beginTransmission(BoardPins::PwmAddress);
  i2cBus.write(PwmMode2Register);
  // Normal active-high PWM uses MODE2=0x04; active-low builds use 0x05.
  i2cBus.write(expectedMode2);
  return i2cBus.endTransmission() == 0;
}

// Low-pass a completed INA219 sample in-place without another history buffer.
__attribute__((noinline)) int32_t
smoothInaValue(int32_t previous, int32_t sample, bool currentOrPower) {
  if (previous == INVALID_I32) {
    return sample;
  }
  // Voltage uses a 1/4 EMA with a 1 mV deadband; noisier current/power use
  // 1/8 with a two-unit deadband. First-valid and sensor errors remain fast.
  return TransitionMath::smoothSample(previous, sample,
                                      currentOrPower ? 3 : 2,
                                      currentOrPower ? 2 : 1);
}

// Samples and filters INA219 at the door-dependent cadence.
void sampleIna219(uint32_t at) {
  const uint16_t samplePeriod =
      systemInputs.doorOpen() ? INA219_DOOR_OPEN_SAMPLE_MS
                              : INA219_SAMPLE_MS;
  if (!ina219Available ||
      static_cast<uint32_t>(at - lastIna219SampleAt) < samplePeriod) {
    return;
  }
  lastIna219SampleAt = at;
  Ina219Reading reading;
  if (!ina219.read(reading)) {
    ina219Available = false;
    return;
  }
  int32_t *filtered = &sensors.supplyMilliVolts;
  const int32_t *sample = &reading.supplyMilliVolts;
  for (uint8_t index = 0; index < 4; ++index) {
    filtered[index] = smoothInaValue(filtered[index], sample[index],
                                     index >= 2);
  }
}

// Gives DS18B20 probes a deterministic ROM order before role assignment.
void sortTemperatureAddresses() {
  if (temperatureAddressCount < 2 ||
      memcmp(temperatureAddresses[0], temperatureAddresses[1], 8) <= 0) {
    return;
  }
  for (uint8_t i = 0; i < 8; ++i) {
    const uint8_t temporary = temperatureAddresses[0][i];
    temperatureAddresses[0][i] = temperatureAddresses[1][i];
    temperatureAddresses[1][i] = temporary;
  }
}

// Enumerates at most two CRC-valid probes and configures asynchronous 11-bit reads.
void discoverTemperatureSensors() {
  temperatureAddressCount = 0;
  const uint8_t discovered = temperatureBus.getDeviceCount();
  for (uint8_t index = 0; index < discovered &&
                          temperatureAddressCount < 2;
       ++index) {
    if (temperatureBus.getAddress(
            temperatureAddresses[temperatureAddressCount], index)) {
      ++temperatureAddressCount;
    }
  }
  sortTemperatureAddresses();
  for (uint8_t index = 0; index < temperatureAddressCount; ++index) {
    // 11-bit conversion is 375 ms with 0.125 C resolution, allowing a more
    // responsive open-enclosure display without blocking UART servicing.
    temperatureBus.setResolution(temperatureAddresses[index], 11);
  }
}

// Maps sorted ROM index to tLED/tBT, honoring the EEPROM swap option.
uint8_t temperatureRole(uint8_t addressIndex) {
  return TemperatureRoles::fromSortedIndex(
      addressIndex, settingsStore.values().swapTemperatureSensors());
}

// Starts a nonblocking conversion unless RF learning owns interrupt timing.
void requestTemperatures(uint32_t at) {
  if (temperatureAddressCount == 0 || learningActive) {
    return;
  }
  temperatureBus.requestTemperatures();
  temperatureConversionPending = true;
  lastTemperatureRequestAt = at;
}

// Prepares cached four-character tLED/tBT text outside the display hot path.
void formatTemperatureSegmentText(uint8_t index, int16_t centiC) {
  char *text = temperatureSegmentText[index];
  if (centiC < 0 || centiC >= 9950) {
    text[1] = '-';
    text[2] = '-';
    return;
  }
  const uint8_t wholeC =
      static_cast<uint8_t>((centiC + 50) / 100);
  text[1] = static_cast<char>('0' + wholeC / 10);
  text[2] = static_cast<char>('0' + wholeC % 10);
}

// Completes ready conversions and schedules the next door-dependent request.
void serviceTemperatures(uint32_t at) {
  // OneWire briefly masks interrupts. Keep it idle during RF learning so
  // receiver pulse timing is not disturbed.
  if (learningActive || temperatureAddressCount == 0) {
    return;
  }

  if (temperatureConversionPending) {
    if (static_cast<uint32_t>(at - lastTemperatureRequestAt) <
        TEMPERATURE_CONVERSION_MS) {
      return;
    }
    for (uint8_t index = 0; index < temperatureAddressCount; ++index) {
      // Factory semantics assign the first sorted ROM to tLED and the second
      // to tBT; the EEPROM swap flag reverses them. Physical probe identity
      // still requires the illumination-heating test after installation.
      const uint8_t destination = temperatureRole(index);
      const int16_t sample =
          temperatureBus.getTempCentiC(temperatureAddresses[index]);
      sensors.temperatureCentiC[destination] =
          smoothTemperature(sensors.temperatureCentiC[destination], sample);
      formatTemperatureSegmentText(
          destination, sensors.temperatureCentiC[destination]);
    }
    temperatureConversionPending = false;
    return;
  }

  const uint16_t samplePeriod =
      systemInputs.doorOpen() ? TEMPERATURE_DOOR_OPEN_PERIOD_MS
                              : TEMPERATURE_PERIOD_MS;
  if (static_cast<uint32_t>(at - lastTemperatureRequestAt) >=
      samplePeriod) {
    requestTemperatures(at);
  }
}
