#pragma once

#include <Arduino.h>
#include <limits.h>

// The shared DS18B20 data line requires an external 4.7 kOhm pull-up to VCC.
// This driver assumes the sensors' VDD pins are externally powered;
// parasite-power conversion would additionally require a strong pull-up.
using Ds18b20Address = uint8_t[8];

// Non-blocking Dallas/DS18B20 bus for the two-sensor ControllerBoardMini.
//
// begin() enumerates and caches at most two valid DS18B20 ROMs. Conversion is
// deliberately split between requestTemperatures() and getTempCentiC(), so the
// caller can wait for the configured 9..12-bit conversion without blocking.
class DallasTemperatureBus {
public:
  static constexpr int16_t DisconnectedCentiC = INT16_MIN;

  explicit DallasTemperatureBus(uint8_t pin);

  void begin();
  uint8_t getDeviceCount() const;
  bool getAddress(Ds18b20Address destination, uint8_t index) const;
  bool setResolution(const uint8_t address[8], uint8_t resolutionBits);

  // Compatibility with the old DallasTemperature call site. This driver is
  // always asynchronous, so no state needs to be changed here.
  void setWaitForConversion(bool) {}

  bool requestTemperatures();
  int16_t getTempCentiC(const uint8_t address[8]);
  float getTempC(const uint8_t address[8]);

private:
  bool reset();
  void select(const uint8_t address[8]);
  void writeBit(bool value);
  bool readBit();
  void writeByte(uint8_t value);
  uint8_t readByte();

  bool searchNext(uint8_t rom[8], uint8_t &lastDiscrepancy,
                  bool &lastDevice);
  static uint8_t crc8(const uint8_t *data, uint8_t length);

  void driveLow();
  void release();
  bool sample() const;

  uint8_t pin_;
  uint8_t bitMask_ = 0;
  volatile uint8_t *outputRegister_ = nullptr;
  volatile uint8_t *modeRegister_ = nullptr;
  volatile uint8_t *inputRegister_ = nullptr;
  uint8_t addresses_[2][8]{};
  uint8_t addressCount_ = 0;
};
