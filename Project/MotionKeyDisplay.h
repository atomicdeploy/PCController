#pragma once

#include "Core/RelayMotionMachine.h"

// KEY is a human-facing surface, not a decimal relay-mask diagnostic. Decode
// only enabled motion pairs; a retained direction bit or user relay is no key.
// A single active key is right-aligned; simultaneous sides read "1  3", etc.
// Return the enabled-pair bits (zero means idle) without materializing a bool;
// compose the right digit in a register before writing the four-byte buffer.
inline uint8_t formatMotionKeyLabels(uint8_t mask, char (&text)[4]) {
  using namespace ControllerCore::RelayOutputs;
  text[0] = text[1] = text[2] = ' ';
  char right = ' ';
  if (mask & (1U << SideAEnable)) {
    right = static_cast<char>('1' + ((mask >> SideADirection) & 1U));
  }
  if (mask & (1U << SideBEnable)) {
    text[0] = right;
    right = (mask & (1U << SideBDirection)) ? '4' : '3';
  }
  text[3] = right;
  return mask & ((1U << SideAEnable) | (1U << SideBEnable));
}
