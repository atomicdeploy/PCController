#include "Project/Core/MotionPolicy.h"

#include <cstdlib>

namespace {

using ControllerCore::MotionDoorPolicy;

void require(bool condition) {
  if (!condition) {
    std::abort();
  }
}

void testPolicyMatrix() {
  struct Case {
    MotionDoorPolicy policy;
    bool doorOpen;
    bool allowed;
  };
  const Case cases[] = {
      {MotionDoorPolicy::Always, false, true},
      {MotionDoorPolicy::Always, true, true},
      {MotionDoorPolicy::ClosedOnly, false, true},
      {MotionDoorPolicy::ClosedOnly, true, false},
      {MotionDoorPolicy::OpenOnly, false, false},
      {MotionDoorPolicy::OpenOnly, true, true},
      {MotionDoorPolicy::Never, false, false},
      {MotionDoorPolicy::Never, true, false},
  };
  for (const Case &policyCase : cases) {
    require(ControllerCore::motionDoorPolicyAllows(policyCase.policy,
                                                   policyCase.doorOpen) ==
            policyCase.allowed);
  }
}

void testFlagRoundTripPreservesNeighbors() {
  constexpr uint8_t adjacentFlags = 0xE7U;
  const MotionDoorPolicy policies[] = {
      MotionDoorPolicy::Always,
      MotionDoorPolicy::ClosedOnly,
      MotionDoorPolicy::OpenOnly,
      MotionDoorPolicy::Never,
  };
  for (MotionDoorPolicy policy : policies) {
    const uint8_t flags =
        ControllerCore::motionDoorPolicyIntoFlags(adjacentFlags, policy);
    require((flags & static_cast<uint8_t>(~ControllerCore::MotionDoorPolicyMask)) ==
            adjacentFlags);
    require(ControllerCore::motionDoorPolicyFromFlags(flags) == policy);
  }
}

} // namespace

int main() {
  testPolicyMatrix();
  testFlagRoundTripPreservesNeighbors();
  return 0;
}
