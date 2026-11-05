#pragma once

#include "Core/RelayMotionMachine.h"

// KEY is a human-facing surface, not a decimal relay-mask diagnostic. Decode
// only enabled motion pairs; a retained direction bit or user relay is no key.
// A single active key is right-aligned; simultaneous sides read "1  3", etc.
inline bool formatMotionKeyLabels(uint8_t mask, char (&text)[4]) {
  using namespace ControllerCore::RelayOutputs;
  text[0] = text[1] = text[2] = text[3] = ' ';
  if (mask & (1U << SideAEnable)) {
    text[3] = (mask & (1U << SideADirection)) ? '2' : '1';
  }
  if (mask & (1U << SideBEnable)) {
    text[0] = text[3];
    text[3] = (mask & (1U << SideBDirection)) ? '4' : '3';
  }
  return mask & ((1U << SideAEnable) | (1U << SideBEnable));
}
