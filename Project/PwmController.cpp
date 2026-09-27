#include "PwmController.h"

namespace {

// Repeated transport failures mark the shared PWM expander unavailable.
constexpr uint8_t MaximumConsecutiveWriteErrors = 3;

bool channelInMask(uint16_t mask, uint8_t channel) {
  return channel < PwmChannels::Count && (mask & _BV(channel)) != 0;
}

} // namespace

PwmController::PwmController(PwmExpanderDriver &driver)
    : driver_(driver) {}

void PwmController::begin(bool available, uint32_t now) {
  (void)now;
  available_ = available;
  channel_ = 0;
  value_ = 0;
  errorCount_ = 0;
  consecutiveWriteErrors_ = 0;
  cacheValidMask_ = 0;

  for (uint8_t channel = 0; channel < PwmChannels::Count; ++channel) {
    cachedValues_[channel] = 0;
  }

  if (!available_) {
    return;
  }
  if (!tryAllOff()) {
    tripUnavailable();
  }
}

bool PwmController::tryAllOff() {
  if (!available_) {
    return false;
  }
#if PCCONTROLLER_PWM_ACTIVE_LOW
  constexpr uint16_t allOffOn = 4096;
  constexpr uint16_t allOffOff = 0;
#else
  constexpr uint16_t allOffOn = 0;
  constexpr uint16_t allOffOff = 4096;
#endif
  if (driver_.setAllPWM(allOffOn, allOffOff) != 0) {
    tripUnavailable();
    return false;
  }
  for (uint8_t channel = 0; channel < PwmChannels::Count; ++channel) {
    cachedValues_[channel] = 0;
  }
  cacheValidMask_ = PwmChannels::AllMask;
  value_ = 0;
  consecutiveWriteErrors_ = 0;
  return true;
}

bool PwmController::clearMask(uint16_t channelMask) {
  if (!available_) {
    return false;
  }
  if (channelMask == PwmChannels::AllMask) {
    return tryAllOff();
  }

  uint16_t failedMask = channelMask;
  // One forced retry handles transient bus noise while still refusing to
  // claim a safe all-off state when any requested channel remains unknown.
  for (uint8_t attempt = 0; attempt < 2 && failedMask != 0 && available_;
       ++attempt) {
    const uint16_t pendingMask = failedMask;
    failedMask = 0;
    uint16_t channelBit = 1;
    for (uint8_t channel = 0; channel < PwmChannels::Count; ++channel) {
      if ((pendingMask & channelBit) != 0 &&
          !writeLogical(channel, 0, attempt != 0)) {
        failedMask |= channelBit;
      }
      channelBit <<= 1;
    }
  }

  if (channelInMask(channelMask, channel_)) {
    value_ = 0;
  }
  if (failedMask != 0 || !available_) {
    tripUnavailable();
    return false;
  }
  return true;
}

bool PwmController::stopUserOutputs() {
  return clearMask(PwmChannels::UserTestMask);
}

void PwmController::setChannel(uint8_t channel, uint32_t now) {
  (void)now;
  if (channel >= PwmChannels::Count) {
    channel = PwmChannels::Count - 1;
  }
  channel_ = channel;
  value_ = logicalValue(channel_);
}

void PwmController::adjustChannel(int8_t delta, uint32_t now) {
  int16_t next = static_cast<int16_t>(channel_) + delta;
  if (next < 0) {
    next = PwmChannels::Count - 1;
  } else if (next >= PwmChannels::Count) {
    next = 0;
  }
  setChannel(static_cast<uint8_t>(next), now);
}

uint8_t PwmController::channel() const { return channel_; }

void PwmController::setValue(uint16_t value, uint32_t now) {
  (void)now;
  if (value > 4095) {
    value = 4095;
  }
  if (!writeLogical(channel_, value)) {
    return;
  }
  value_ = value;
}

void PwmController::adjustValue(int16_t delta, uint32_t now) {
  setValue(PwmValueRollover::next(value_, delta), now);
}

uint16_t PwmController::value() const { return value_; }

bool PwmController::available() const { return available_; }

uint8_t PwmController::errorCount() const { return errorCount_; }

bool PwmController::setLogical(uint8_t channel, uint16_t value) {
  if (channel >= PwmChannels::Count) {
    return false;
  }
  channel_ = channel;
  const bool success = writeLogical(channel, value);
  if (success) {
    value_ = value > 4095 ? 4095 : value;
  }
  return success;
}

uint16_t PwmController::logicalValue(uint8_t channel) const {
  if (!cacheValid(channel)) {
    return 0;
  }
  return cachedValues_[channel];
}

bool PwmController::cacheValid(uint8_t channel) const {
  return channel < PwmChannels::Count &&
         (cacheValidMask_ & _BV(channel)) != 0;
}

PwmChannelRole PwmController::role(uint8_t channel) const {
  if (channel < PwmChannels::UserLightCount) {
    return PwmChannelRole::UserLight;
  }
  if (channel >= PwmChannels::UserPwmFirst &&
      channel < PwmChannels::UserPwmFirst + PwmChannels::UserPwmCount) {
    return PwmChannelRole::UserPwm;
  }
  switch (channel) {
    case PwmChannels::EnclosureIllumination:
      return PwmChannelRole::EnclosureIllumination;
    case PwmChannels::PowerSignal:
      return PwmChannelRole::PowerSignal;
    case PwmChannels::StatusRed:
      return PwmChannelRole::StatusRed;
    case PwmChannels::StatusGreen:
      return PwmChannelRole::StatusGreen;
    case PwmChannels::StatusBlue:
      return PwmChannelRole::StatusBlue;
    default:
      return PwmChannelRole::Invalid;
  }
}

bool PwmController::setUserLight(uint8_t lightIndex, uint16_t value) {
  if (lightIndex >= PwmChannels::UserLightCount) {
    return false;
  }
  channel_ = static_cast<uint8_t>(PwmChannels::UserLightFirst + lightIndex);
  const bool success = writeLogical(channel_, value);
  if (success) {
    value_ = value > 4095 ? 4095 : value;
  }
  return success;
}

bool PwmController::setUserPwm(uint8_t pwmIndex, uint16_t value) {
  if (pwmIndex >= PwmChannels::UserPwmCount) {
    return false;
  }
  channel_ = static_cast<uint8_t>(PwmChannels::UserPwmFirst + pwmIndex);
  const bool success = writeLogical(channel_, value);
  if (success) {
    value_ = value > 4095 ? 4095 : value;
  }
  return success;
}

bool PwmController::setEnclosureIllumination(uint16_t value) {
  return writeLogical(PwmChannels::EnclosureIllumination, value);
}

bool PwmController::setPowerSignal(bool active) {
  return writeLogical(PwmChannels::PowerSignal, active ? 4095 : 0);
}

bool PwmController::setStatusRgb12(uint16_t red, uint16_t green,
                                   uint16_t blue) {
  bool success = true;
  if (!writeLogical(PwmChannels::StatusRed, red)) {
    success = false;
  }
  if (!writeLogical(PwmChannels::StatusGreen, green)) {
    success = false;
  }
  if (!writeLogical(PwmChannels::StatusBlue, blue)) {
    success = false;
  }
  return success;
}

bool PwmController::setStatusRgb8(uint8_t red, uint8_t green, uint8_t blue) {
  return setStatusRgb12(from8Bit(red), from8Bit(green), from8Bit(blue));
}

bool PwmController::writeLogical(uint8_t channel, uint16_t value, bool force) {
  if (!available_ || channel >= PwmChannels::Count) {
    return false;
  }
  if (value > 4095) {
    value = 4095;
  }
  if (!force && cacheValid(channel) && cachedValues_[channel] == value) {
    return true;
  }

#if PCCONTROLLER_PWM_ACTIVE_LOW
  uint16_t on = 0;
  uint16_t off = 0;
  if (value == 0) {
    on = 4096; // Output HIGH: active-low load is electrically inactive.
  } else if (value == 4095) {
    off = 4096; // Output LOW: active-low load is fully active.
  } else {
    off = static_cast<uint16_t>(4095 - value);
  }
#else
  uint16_t on = 0;
  uint16_t off = 0;
  if (value == 4095) {
    on = 4096;
  } else if (value == 0) {
    off = 4096;
  } else {
    off = value;
  }
#endif

  if (driver_.setPWM(channel, on, off) == 0) {
    cachedValues_[channel] = value;
    cacheValidMask_ |= _BV(channel);
    if (channel == channel_) {
      value_ = value;
    }
    consecutiveWriteErrors_ = 0;
    return true;
  }

  cacheValidMask_ &= static_cast<uint16_t>(~_BV(channel));
  if (errorCount_ < 255) {
    ++errorCount_;
  }
  if (consecutiveWriteErrors_ < 255) {
    ++consecutiveWriteErrors_;
  }
  if (consecutiveWriteErrors_ >= MaximumConsecutiveWriteErrors) {
    tripUnavailable();
  }
  return false;
}

void PwmController::tripUnavailable() {
  available_ = false;
  value_ = 0;
  cacheValidMask_ = 0;
}

uint16_t PwmController::from8Bit(uint8_t value) {
  // Exact endpoint-preserving 8-bit to 12-bit expansion.
  return static_cast<uint16_t>(static_cast<uint16_t>(value) * 16U +
                               value / 16U);
}
