#pragma once

// Target-neutral persisted motion/door policy contract.
//
// This header owns the compact two-bit EEPROM representation and the decision
// predicate, without taking an Arduino, storage, GPIO, or scheduler dependency.

#include <stdint.h>

namespace ControllerCore {

// Determines which debounced enclosure states permit a motion-start request.
enum class MotionDoorPolicy : uint8_t {
  Always = 0,
  ClosedOnly = 1,
  OpenOnly = 2,
  Never = 3,
};

// The persisted two-bit policy field lives in the shared settings-flags byte.
constexpr uint8_t MotionDoorPolicyShift = 3U;
constexpr uint8_t MotionDoorPolicyMask =
    static_cast<uint8_t>(0x03U << MotionDoorPolicyShift);

// Decodes the policy while ignoring adjacent persisted settings bits.
inline MotionDoorPolicy motionDoorPolicyFromFlags(uint8_t flags) {
  return static_cast<MotionDoorPolicy>(
      (flags & MotionDoorPolicyMask) >> MotionDoorPolicyShift);
}

// Replaces only the policy field, preserving every adjacent settings bit.
inline uint8_t motionDoorPolicyIntoFlags(uint8_t flags,
                                         MotionDoorPolicy policy) {
  return static_cast<uint8_t>(
      (flags & static_cast<uint8_t>(~MotionDoorPolicyMask)) |
      ((static_cast<uint8_t>(policy) & 0x03U) << MotionDoorPolicyShift));
}

// Applies the four persisted modes to one debounced enclosure state.
inline bool motionDoorPolicyAllows(MotionDoorPolicy policy, bool doorOpen) {
  return policy == MotionDoorPolicy::Always ||
         (policy == MotionDoorPolicy::ClosedOnly && !doorOpen) ||
         (policy == MotionDoorPolicy::OpenOnly && doorOpen);
}

} // namespace ControllerCore
