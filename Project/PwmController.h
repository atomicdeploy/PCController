#pragma once

#include <Arduino.h>

#include "../LocalLib/BoardPins.h"
#include "../ProjectConfig.h"
#include "PwmExpanderDriver.h"

#ifndef PCCONTROLLER_PWM_ACTIVE_LOW
#define PCCONTROLLER_PWM_ACTIVE_LOW 0
#endif

namespace PwmChannels {

// Logical ownership map for the board's sixteen PWM-expander channels.
constexpr uint8_t UserLightFirst = 0;
constexpr uint8_t UserLightCount = 8;
constexpr uint8_t UserPwmFirst = 8;
constexpr uint8_t UserPwmCount = 3;
constexpr uint8_t EnclosureIllumination = 11;
constexpr uint8_t PowerSignal = 12;
constexpr uint8_t StatusRed = 13;
constexpr uint8_t StatusGreen = 14;
constexpr uint8_t StatusBlue = 15;
constexpr uint8_t Count = 16;

constexpr uint16_t UserTestMask = 0x07FFU; // Channels 0..10 only.
constexpr uint16_t AllMask = 0xFFFFU;

} // namespace PwmChannels

namespace PowerSignalFallback {
constexpr uint16_t Step = 256;
constexpr uint16_t FullBrightness = 4095;
constexpr uint16_t IntervalMs = 32;

// Host-connected and Prog states never alter channel 12. Only an offline
// operational controller advances one bounded fade step toward full power.
constexpr uint16_t nextValue(uint16_t current, bool hostOffline,
                             bool programming) {
  if (!hostOffline || programming || current >= FullBrightness) {
    return current;
  }
  return current > static_cast<uint16_t>(FullBrightness - Step)
             ? FullBrightness
             : static_cast<uint16_t>(current + Step);
}
} // namespace PowerSignalFallback

// Front-panel PWM editing must visit both electrical endpoints before wrapping:
// 3840 + 256 shows 4095, and only the next increment returns to zero. The
// same applies in reverse. Keeping this pure helper shared with native tests
// prevents the seven-segment value from appearing to skip its rollover point.
namespace PwmValueRollover {
constexpr uint16_t next(uint16_t current, int16_t delta) {
  if (current > 4095) {
    current = 4095;
  }
  if (delta > 0) {
    if (current == 4095) {
      return 0;
    }
    const uint16_t candidate = static_cast<uint16_t>(current + delta);
    return candidate >= 4095 ? 4095 : candidate;
  }
  if (delta < 0) {
    if (current == 0) {
      return 4095;
    }
    const uint16_t magnitude =
        static_cast<uint16_t>(-static_cast<int32_t>(delta));
    return current <= magnitude ? 0
                                : static_cast<uint16_t>(current - magnitude);
  }
  return current;
}
} // namespace PwmValueRollover

// PwmChannelRole records the logical ownership of one 16-channel output.
enum class PwmChannelRole : uint8_t {
  UserLight,
  UserPwm,
  EnclosureIllumination,
  PowerSignal,
  StatusRed,
  StatusGreen,
  StatusBlue,
  Invalid,
};

// Logical values always use 0 = electrically inactive and 4095 = fully
// active. The optional polarity mapping keeps callers independent of register
// FULL_ON/FULL_OFF details if a later board revision inverts the stages.
class PwmController {
public:
  explicit PwmController(PwmExpanderDriver &driver);

  void begin(bool available, uint32_t now = millis());

  // Front-panel commissioning controls.
  void setChannel(uint8_t channel, uint32_t now = millis());
  void adjustChannel(int8_t delta, uint32_t now = millis());
  uint8_t channel() const;
  void setValue(uint16_t value, uint32_t now = millis());
  void adjustValue(int16_t delta, uint32_t now = millis());
  uint16_t value() const;
  bool available() const;
  uint8_t errorCount() const;

  // Result-bearing hardware API for new integrations.
  bool tryAllOff();
  bool clearMask(uint16_t channelMask);
  bool stopUserOutputs();
  bool setLogical(uint8_t channel, uint16_t value);
  uint16_t logicalValue(uint8_t channel) const;
  bool cacheValid(uint8_t channel) const;
  PwmChannelRole role(uint8_t channel) const;

  bool setUserLight(uint8_t lightIndex, uint16_t value);
  bool setUserPwm(uint8_t pwmIndex, uint16_t value);
  bool setEnclosureIllumination(uint16_t value);
  bool setPowerSignal(bool active);
  bool setStatusRgb12(uint16_t red, uint16_t green, uint16_t blue);
  bool setStatusRgb8(uint8_t red, uint8_t green, uint8_t blue);

  // PWM is at 0x41 while INA219 stays at 0x40, avoiding an I2C collision.
  static constexpr uint8_t PwmI2cAddress = 0x41;
  static constexpr uint8_t CurrentSensorI2cAddress = 0x40;
  static constexpr float PwmFrequencyHz = 1000.0F;

  // With active-low mapping, OUTNE=01 makes an asserted OE pin drive all
  // outputs HIGH (inactive). For normal/active-high builds OE must instead
  // force LOW, so OUTNE remains 00.
  static constexpr uint8_t recommendedMode2() {
#if PCCONTROLLER_PWM_ACTIVE_LOW
    return 0x05;
#else
    return 0x04;
#endif
  }

private:
  bool writeLogical(uint8_t channel, uint16_t value, bool force = false);
  void tripUnavailable();
  static uint16_t from8Bit(uint8_t value);

  PwmExpanderDriver &driver_;
  uint16_t cachedValues_[PwmChannels::Count] = {};
  uint16_t cacheValidMask_ = 0;
  uint8_t channel_ = 0;
  uint16_t value_ = 0;
  uint8_t errorCount_ = 0;
  uint8_t consecutiveWriteErrors_ = 0;
  bool available_ = false;
};

static_assert(BoardPins::PwmAddress == PwmController::PwmI2cAddress,
              "PWM driver must use I2C address 0x41");
static_assert(BoardPins::Ina219Address == PwmController::CurrentSensorI2cAddress,
              "INA219 must use I2C address 0x40");
static_assert(BoardPins::PwmFrequencyHz == PwmController::PwmFrequencyHz,
              "PWM driver must run at 1 kHz");
