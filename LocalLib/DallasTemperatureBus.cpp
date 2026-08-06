// SPDX-FileCopyrightText: 2000 Dallas Semiconductor Corporation
// SPDX-FileCopyrightText: 2007 Jim Studt and OneWire contributors
// SPDX-FileCopyrightText: 2026 David Refoua and PCController contributors
// SPDX-License-Identifier: MIT

#include "DallasTemperatureBus.h"

#include <string.h>

#if defined(__AVR__)
#include <avr/interrupt.h>
#include <avr/io.h>
#endif

namespace {

#if defined(__AVR__)
class InterruptGuard {
public:
  InterruptGuard() : savedSreg_(SREG) { cli(); }
  ~InterruptGuard() { SREG = savedSreg_; }

private:
  uint8_t savedSreg_;
};
#else
class InterruptGuard {
public:
  InterruptGuard() { noInterrupts(); }
  ~InterruptGuard() { interrupts(); }
};
#endif

// OneWire ROM and DS18B20 function commands used by the bounded bus driver.
constexpr uint8_t SearchRomCommand = 0xF0;
constexpr uint8_t MatchRomCommand = 0x55;
constexpr uint8_t SkipRomCommand = 0xCC;
constexpr uint8_t ConvertTemperatureCommand = 0x44;
constexpr uint8_t WriteScratchpadCommand = 0x4E;
constexpr uint8_t ReadScratchpadCommand = 0xBE;
constexpr uint8_t Ds18b20Family = 0x28;

} // namespace

DallasTemperatureBus::DallasTemperatureBus(uint8_t pin) : pin_(pin) {}

void DallasTemperatureBus::begin() {
  const uint8_t port = digitalPinToPort(pin_);
  bitMask_ = digitalPinToBitMask(pin_);
  outputRegister_ = portOutputRegister(port);
  modeRegister_ = portModeRegister(port);
  inputRegister_ = portInputRegister(port);
  release();

  addressCount_ = 0;
  uint8_t rom[8] = {};
  uint8_t lastDiscrepancy = 0;
  bool lastDevice = false;
  // A missing pull-up can make a floating/stuck-low bus look like an
  // unlimited family of invalid ROMs. Require an idle-high bus and cap search
  // passes so startup always returns promptly.
  release();
  delayMicroseconds(10);
  if (!sample()) {
    return;
  }
  uint8_t searchPasses = 0;
  while (addressCount_ < 2 && searchPasses++ < 8 &&
         searchNext(rom, lastDiscrepancy, lastDevice)) {
    if (rom[0] == Ds18b20Family && crc8(rom, 7) == rom[7]) {
      memcpy(addresses_[addressCount_], rom, sizeof(rom));
      ++addressCount_;
    }
  }
}

uint8_t DallasTemperatureBus::getDeviceCount() const {
  return addressCount_;
}

bool DallasTemperatureBus::getAddress(Ds18b20Address destination,
                                      uint8_t index) const {
  if (index >= addressCount_) {
    return false;
  }
  memcpy(destination, addresses_[index], sizeof(addresses_[index]));
  return true;
}

bool DallasTemperatureBus::setResolution(const uint8_t address[8],
                                         uint8_t resolutionBits) {
  if (address == nullptr || resolutionBits < 9 || resolutionBits > 12 ||
      !reset()) {
    return false;
  }

  select(address);
  writeByte(WriteScratchpadCommand);
  writeByte(75); // TH alarm register; retained at a harmless default.
  writeByte(70); // TL alarm register; retained at a harmless default.
  writeByte(static_cast<uint8_t>(0x1F | ((resolutionBits - 9) << 5)));
  return true;
}

bool DallasTemperatureBus::requestTemperatures() {
  if (addressCount_ == 0 || !reset()) {
    return false;
  }
  writeByte(SkipRomCommand);
  writeByte(ConvertTemperatureCommand);
  release();
  return true;
}

int16_t DallasTemperatureBus::getTempCentiC(const uint8_t address[8]) {
  if (address == nullptr || !reset()) {
    return DisconnectedCentiC;
  }

  select(address);
  writeByte(ReadScratchpadCommand);

  uint8_t scratchpad[9];
  for (uint8_t index = 0; index < sizeof(scratchpad); ++index) {
    scratchpad[index] = readByte();
  }
  if (crc8(scratchpad, 8) != scratchpad[8]) {
    return DisconnectedCentiC;
  }

  int16_t raw = static_cast<int16_t>(
      static_cast<uint16_t>(scratchpad[0]) |
      (static_cast<uint16_t>(scratchpad[1]) << 8));

  // Respect the sensor's actual resolution in case configuration was lost.
  switch (scratchpad[4] & 0x60) {
  case 0x00:
    raw &= static_cast<int16_t>(~7);
    break;
  case 0x20:
    raw &= static_cast<int16_t>(~3);
    break;
  case 0x40:
    raw &= static_cast<int16_t>(~1);
    break;
  default:
    break;
  }

  const int32_t scaled = static_cast<int32_t>(raw) * 100;
  return static_cast<int16_t>(
      scaled >= 0 ? (scaled + 8) / 16 : (scaled - 8) / 16);
}

float DallasTemperatureBus::getTempC(const uint8_t address[8]) {
  const int16_t centiC = getTempCentiC(address);
  return centiC == DisconnectedCentiC ? -127.0F
                                     : static_cast<float>(centiC) / 100.0F;
}

bool DallasTemperatureBus::reset() {
  if (bitMask_ == 0) {
    return false;
  }

  release();
  delayMicroseconds(5);
  // A valid externally pulled-up 1-Wire bus must idle high. Returning here
  // prevents a missing pull-up or short-to-ground from being mistaken for an
  // endless set of devices during ROM search.
  if (!sample()) {
    return false;
  }
  driveLow();
  delayMicroseconds(480);

  bool present;
  {
    // Only release-to-sample timing is critical. Keeping interrupts enabled
    // during the long reset pulse/recovery avoids starving UART0 at 115200.
    InterruptGuard guard;
    release();
    delayMicroseconds(70);
    present = !sample();
  }
  delayMicroseconds(410);
  return present;
}

void DallasTemperatureBus::select(const uint8_t address[8]) {
  writeByte(MatchRomCommand);
  for (uint8_t index = 0; index < 8; ++index) {
    writeByte(address[index]);
  }
}

void DallasTemperatureBus::writeBit(bool value) {
  if (value) {
    {
      InterruptGuard guard;
      driveLow();
      delayMicroseconds(6);
      release();
    }
    delayMicroseconds(64);
  } else {
    {
      InterruptGuard guard;
      driveLow();
      delayMicroseconds(60);
      release();
    }
    delayMicroseconds(10);
  }
}

bool DallasTemperatureBus::readBit() {
  bool value;
  {
    InterruptGuard guard;
    driveLow();
    delayMicroseconds(3);
    release();
    delayMicroseconds(10);
    value = sample();
  }
  delayMicroseconds(53);
  return value;
}

void DallasTemperatureBus::writeByte(uint8_t value) {
  for (uint8_t bit = 0; bit < 8; ++bit) {
    writeBit(value & 0x01);
    value >>= 1;
  }
}

uint8_t DallasTemperatureBus::readByte() {
  uint8_t value = 0;
  for (uint8_t bit = 0; bit < 8; ++bit) {
    value >>= 1;
    if (readBit()) {
      value |= 0x80;
    }
  }
  return value;
}

bool DallasTemperatureBus::searchNext(uint8_t rom[8],
                                      uint8_t &lastDiscrepancy,
                                      bool &lastDevice) {
  if (lastDevice || !reset()) {
    lastDiscrepancy = 0;
    lastDevice = false;
    return false;
  }

  writeByte(SearchRomCommand);
  uint8_t bitNumber = 1;
  uint8_t lastZero = 0;
  uint8_t byteNumber = 0;
  uint8_t byteMask = 1;

  while (byteNumber < 8) {
    const bool idBit = readBit();
    const bool complementBit = readBit();
    if (idBit && complementBit) {
      return false;
    }

    bool direction;
    if (idBit != complementBit) {
      direction = idBit;
    } else {
      direction = bitNumber < lastDiscrepancy
                      ? (rom[byteNumber] & byteMask) != 0
                      : bitNumber == lastDiscrepancy;
      if (!direction) {
        lastZero = bitNumber;
      }
    }

    if (direction) {
      rom[byteNumber] |= byteMask;
    } else {
      rom[byteNumber] &= static_cast<uint8_t>(~byteMask);
    }
    writeBit(direction);

    ++bitNumber;
    byteMask <<= 1;
    if (byteMask == 0) {
      ++byteNumber;
      byteMask = 1;
    }
  }

  lastDiscrepancy = lastZero;
  lastDevice = lastDiscrepancy == 0;
  return true;
}

uint8_t DallasTemperatureBus::crc8(const uint8_t *data, uint8_t length) {
  uint8_t crc = 0;
  while (length-- != 0) {
    uint8_t value = *data++;
    for (uint8_t bit = 0; bit < 8; ++bit) {
      const uint8_t mix = (crc ^ value) & 0x01;
      crc >>= 1;
      if (mix != 0) {
        crc ^= 0x8C;
      }
      value >>= 1;
    }
  }
  return crc;
}

void DallasTemperatureBus::driveLow() {
  *outputRegister_ &= static_cast<uint8_t>(~bitMask_);
  *modeRegister_ |= bitMask_;
}

void DallasTemperatureBus::release() {
  if (outputRegister_ == nullptr || modeRegister_ == nullptr) {
    pinMode(pin_, INPUT);
    digitalWrite(pin_, LOW);
    return;
  }
  *outputRegister_ &= static_cast<uint8_t>(~bitMask_);
  *modeRegister_ &= static_cast<uint8_t>(~bitMask_);
}

bool DallasTemperatureBus::sample() const {
  return (*inputRegister_ & bitMask_) != 0;
}
