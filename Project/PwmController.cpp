#include "PwmController.h"

namespace {

constexpr uint16_t AutoStepIntervalMs = 20;
constexpr uint16_t AutoStep = 128;
constexpr uint8_t MaximumConsecutiveWriteErrors = 3;

bool channelInMask(uint16_t mask, uint8_t channel) {
  return channel < PwmChannels::Count && (mask & _BV(channel)) != 0;
}

} // namespace

PwmController::PwmController(PwmExpanderDriver &driver)
    : driver_(driver) {}

void PwmController::begin(bool available, uint32_t now) {
  available_ = available;
  mode_ = available ? PwmTestMode::Auto : PwmTestMode::Off;
  channel_ = firstAutoChannel();
  value_ = 0;
  rising_ = true;
  errorCount_ = 0;
  consecutiveWriteErrors_ = 0;
  cacheValidMask_ = 0;
  lastStepAt_ = now;
  autoChannelChanged_ = false;

  for (uint8_t channel = 0; channel < PwmChannels::Count; ++channel) {
    cachedValues_[channel] = 0;
  }

  if (!available_) {
    return;
  }
  if (!tryAllOff()) {
    tripUnavailable();
    return;
  }

  mode_ = PwmTestMode::Auto;
  autoChannelChanged_ = true;
}

void PwmController::service(uint32_t now) {
  if (!available_ || mode_ != PwmTestMode::Auto ||
      autoTestMask_ == 0 ||
      static_cast<uint32_t>(now - lastStepAt_) < AutoStepIntervalMs) {
    return;
  }
  lastStepAt_ = now;

  if (!channelInMask(autoTestMask_, channel_)) {
    channel_ = firstAutoChannel();
    value_ = 0;
    rising_ = true;
    autoChannelChanged_ = true;
  }

  if (rising_) {
    const uint16_t sum = static_cast<uint16_t>(value_ + AutoStep);
    const uint16_t next = sum >= 4095 ? 4095 : sum;
    if (!writeLogical(channel_, next)) {
      return;
    }
    value_ = next;
    if (value_ == 4095) {
      rising_ = false;
    }
    return;
  }

  const uint16_t next =
      value_ > AutoStep ? static_cast<uint16_t>(value_ - AutoStep) : 0;
  if (!writeLogical(channel_, next)) {
    return;
  }
  value_ = next;
  if (value_ != 0) {
    return;
  }

  channel_ = nextAutoChannel(channel_);
  rising_ = true;
  autoChannelChanged_ = true;
}

bool PwmController::tryAllOff() {
  return clearMask(PwmChannels::AllMask);
}

bool PwmController::clearMask(uint16_t channelMask) {
  if (!available_) {
    return false;
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

void PwmController::setMode(PwmTestMode mode, uint32_t now) {
  if (mode == PwmTestMode::Off) {
    (void)clearMask(autoTestMask_);
    mode_ = PwmTestMode::Off;
    value_ = logicalValue(channel_);
    rising_ = true;
    autoChannelChanged_ = false;
    return;
  }
  if (!available_ || mode_ == mode) {
    return;
  }
  if (!clearMask(autoTestMask_)) {
    return;
  }
  mode_ = mode;
  value_ = 0;
  rising_ = true;
  lastStepAt_ = now;
  if (mode_ == PwmTestMode::Auto) {
    channel_ = firstAutoChannel();
    autoChannelChanged_ = true;
  } else {
    autoChannelChanged_ = false;
  }
}

PwmTestMode PwmController::mode() const { return mode_; }

void PwmController::setChannel(uint8_t channel, uint32_t now) {
  if (channel >= PwmChannels::Count) {
    channel = PwmChannels::Count - 1;
  }
  if (mode_ == PwmTestMode::Manual && channel != channel_ && value_ != 0) {
    if (!writeLogical(channel_, 0)) {
      return;
    }
    value_ = 0;
  }
  if (!prepareManual(now)) {
    return;
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
  if (value > 4095) {
    value = 4095;
  }
  if (!prepareManual(now) || !writeLogical(channel_, value)) {
    return;
  }
  value_ = value;
}

void PwmController::adjustValue(int16_t delta, uint32_t now) {
  int32_t next = static_cast<int32_t>(value_) + delta;
  if (next < 0) {
    next = 4095;
  } else if (next > 4095) {
    next = 0;
  }
  setValue(static_cast<uint16_t>(next), now);
}

uint16_t PwmController::value() const { return value_; }

bool PwmController::available() const { return available_; }

bool PwmController::rising() const { return rising_; }

uint8_t PwmController::errorCount() const { return errorCount_; }

bool PwmController::consumeAutoChannelChange(uint8_t &channelValue) {
  if (!autoChannelChanged_) {
    return false;
  }
  autoChannelChanged_ = false;
  channelValue = channel_;
  return true;
}

bool PwmController::setLogical(uint8_t channel, uint16_t value) {
  return writeLogical(channel, value);
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
  if (mode_ == PwmTestMode::Auto && !prepareManual(millis())) {
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
  if (mode_ == PwmTestMode::Auto && !prepareManual(millis())) {
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

void PwmController::setAutoTestMask(uint16_t channelMask, uint32_t now) {
  if (channelMask == autoTestMask_) {
    return;
  }
  const uint16_t previousMask = autoTestMask_;
  (void)clearMask(previousMask);
  autoTestMask_ = channelMask;
  channel_ = firstAutoChannel();
  value_ = 0;
  rising_ = true;
  lastStepAt_ = now;
  autoChannelChanged_ =
      available_ && mode_ == PwmTestMode::Auto && autoTestMask_ != 0;
  if (autoTestMask_ == 0 && mode_ == PwmTestMode::Auto) {
    mode_ = PwmTestMode::Off;
  }
}

uint16_t PwmController::autoTestMask() const { return autoTestMask_; }

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

bool PwmController::prepareManual(uint32_t now) {
  if (!available_) {
    return false;
  }
  if (mode_ != PwmTestMode::Manual) {
    if (!stopTestOutput()) {
      return false;
    }
    mode_ = PwmTestMode::Manual;
    value_ = 0;
    rising_ = true;
    lastStepAt_ = now;
    autoChannelChanged_ = false;
  }
  return true;
}

bool PwmController::stopTestOutput() {
  if (!available_) {
    value_ = 0;
    return false;
  }
  if (!writeLogical(channel_, 0)) {
    return false;
  }
  value_ = 0;
  return true;
}

void PwmController::tripUnavailable() {
  available_ = false;
  mode_ = PwmTestMode::Off;
  value_ = 0;
  rising_ = true;
  autoChannelChanged_ = false;
  cacheValidMask_ = 0;
}

uint8_t PwmController::firstAutoChannel() const {
  for (uint8_t channel = 0; channel < PwmChannels::Count; ++channel) {
    if (channelInMask(autoTestMask_, channel)) {
      return channel;
    }
  }
  return 0;
}

uint8_t PwmController::nextAutoChannel(uint8_t current) const {
  for (uint8_t offset = 1; offset <= PwmChannels::Count; ++offset) {
    const uint8_t candidate =
        static_cast<uint8_t>((current + offset) % PwmChannels::Count);
    if (channelInMask(autoTestMask_, candidate)) {
      return candidate;
    }
  }
  return current;
}

uint16_t PwmController::from8Bit(uint8_t value) {
  // Exact endpoint-preserving 8-bit to 12-bit expansion.
  return static_cast<uint16_t>(static_cast<uint16_t>(value) * 16U +
                               value / 16U);
}
