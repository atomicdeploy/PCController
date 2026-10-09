#pragma once

#include <Arduino.h>

// Small, allocation-free transition helpers shared by front-panel domains.
namespace TransitionMath {

// Rolls a byte setting through both electrical endpoints before wrapping.
inline uint8_t rollByte(uint8_t value, uint8_t step, bool increase) {
  if (increase) {
    if (value == 255) {
      return 0;
    }
    const uint16_t next = static_cast<uint16_t>(value) + step;
    return static_cast<uint8_t>(next >= 255 ? 255 : next);
  }
  if (value == 0) {
    return 255;
  }
  return value <= step ? 0 : static_cast<uint8_t>(value - step);
}

// Moves one 8-bit PWM frame toward its endpoint with a damped ease-out.
inline uint8_t easedByte(uint8_t current, uint8_t target) {
  if (current == target) {
    return current;
  }
  const uint8_t distance = current > target
                               ? static_cast<uint8_t>(current - target)
                               : static_cast<uint8_t>(target - current);
  uint8_t step = static_cast<uint8_t>(distance >> 4);
  if (step == 0) {
    step = 1;
  } else if (step > 8) {
    step = 8;
  }
  if (current < target) {
    const uint16_t next = static_cast<uint16_t>(current) + step;
    return static_cast<uint8_t>(next > target ? target : next);
  }
  return static_cast<uint8_t>(current - target > step ? current - step
                                                       : target);
}

// Damps one 12-bit RGB channel while guaranteeing eventual convergence.
inline uint16_t easedChannel(uint16_t current, uint16_t target) {
  const int16_t delta = static_cast<int16_t>(target - current);
  if (delta == 0) {
    return current;
  }
  int16_t step = static_cast<int16_t>(delta / 4);
  if (step == 0) {
    step = delta > 0 ? 1 : -1;
  }
  return static_cast<uint16_t>(static_cast<int16_t>(current) + step);
}

// Applies a power-of-two EMA with a small noise deadband and no history RAM.
inline int32_t smoothSample(int32_t previous, int32_t sample, uint8_t shift,
                            uint8_t deadband) {
  const int32_t delta = sample - previous;
  if (delta >= -static_cast<int32_t>(deadband) &&
      delta <= static_cast<int32_t>(deadband)) {
    return previous;
  }
  int32_t step = delta >> shift;
  if (step == 0) {
    step = delta > 0 ? 1 : -1;
  }
  return previous + step;
}

} // namespace TransitionMath
