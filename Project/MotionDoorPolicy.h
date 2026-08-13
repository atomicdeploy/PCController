#pragma once

#include "Core/MotionPolicy.h"

// AVR-facing compatibility names retain established source call sites while
// the target-neutral ControllerCore contract owns their representation.
using MotionDoorPolicy = ControllerCore::MotionDoorPolicy;
using ControllerCore::motionDoorPolicyAllows;
