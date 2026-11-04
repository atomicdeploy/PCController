#pragma once

#include "Core/RelayMotionMachine.h"

// KEY is a human-facing surface, not a decimal relay-mask diagnostic. Decode
// only enabled motion pairs; a retained direction bit or user relay is no key.
// A single active key is right-aligned; simultaneous sides read "1  3", etc.
inline bool formatMotionKeyLabels(uint8_t mask, char (&text)[4]) {
  using namespace ControllerCore::RelayOutputs;
  const uint8_t a = (mask & (1U << SideAEnable))
                        ? ((mask & (1U << SideADirection)) ? 2 : 1)
                        : 0;
  const uint8_t b = (mask & (1U << SideBEnable))
                        ? ((mask & (1U << SideBDirection)) ? 4 : 3)
                        : 0;
  text[0] = a && b ? static_cast<char>('0' + a) : ' ';
  text[1] = text[2] = ' ';
  text[3] = a || b ? static_cast<char>('0' + (b ? b : a)) : ' ';
  return a || b;
}
