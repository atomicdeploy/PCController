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
  // Sentinel returned by getTempCentiC() when reset, address, or CRC fails.
  static constexpr int16_t DisconnectedCentiC = INT16_MIN;

  explicit DallasTemperatureBus(uint8_t pin);

  // Enumerates and caches at most two valid DS18B20 ROM addresses.
  void begin();
  // Returns the number of cached sensors (zero through two).
  uint8_t getDeviceCount() const;
  // Copies the indexed eight-byte ROM into destination; false means missing.
  bool getAddress(Ds18b20Address destination, uint8_t index) const;
  // Configures a cached sensor to 9..12-bit resolution; false means rejected
  // resolution, unknown address, or a failed 1-Wire reset.
  bool setResolution(const uint8_t address[8], uint8_t resolutionBits);

  // Starts conversion on every sensor; the caller owns the resolution-based
  // wait before reading the scratchpads.
  bool requestTemperatures();
  // Returns rounded hundredths of a degree Celsius, or DisconnectedCentiC.
  int16_t getTempCentiC(const uint8_t address[8]);
  // Returns degrees Celsius, or -127.0F for the disconnected/error sentinel.
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
