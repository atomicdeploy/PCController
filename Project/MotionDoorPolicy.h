#pragma once

#include <Arduino.h>

// MotionDoorPolicy determines which enclosure states permit motion output.
enum class MotionDoorPolicy : uint8_t {
  Always = 0,
  ClosedOnly,
  OpenOnly,
  Never,
};

// Applies the four persisted policy modes to one debounced enclosure state.
inline bool motionDoorPolicyAllows(MotionDoorPolicy policy, bool doorOpen) {
  return policy == MotionDoorPolicy::Always ||
         (policy == MotionDoorPolicy::ClosedOnly && !doorOpen) ||
         (policy == MotionDoorPolicy::OpenOnly && doorOpen);
}
