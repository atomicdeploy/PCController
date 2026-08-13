#pragma once

#include <Arduino.h>

#include "BoardPins.h"

// ShiftInput names the eight active-low positions sampled from the 74HC165.
enum ShiftInput : uint8_t {
  IN_1 = 0,
  IN_2,
  IN_3,
  IN_4,
  IN_5,
  IN_6,
  IN_7,
  IN_8
};

// ShiftOutput names the eight active-low positions driven through the 74HC595.
enum ShiftOutput : uint8_t {
  OUT_1 = 0,
  OUT_2,
  OUT_3,
  OUT_4,
  OUT_5,
  OUT_6,
  OUT_7,
  OUT_8
};

// ShiftRegisters owns the shared-clock 74HC165/74HC595 input-output chain.
class ShiftRegisters {
public:
  void begin();
  void service();

  bool inputActive(uint8_t bit) const;
  uint8_t activeInputs() const;
  uint8_t rawInputs() const;

  void setOutput(uint8_t bit, bool active);
  // Applies all logical relay outputs in one cache update before service().
  void setActiveOutputs(uint8_t activeMask);
  void allOutputsOff();
  uint8_t activeOutputs() const;

  // Clearing a virtual bit simulates an active-low physical input for tests or
  // remote input injection.
  void setVirtualInput(uint8_t bit, bool active);
  void clearVirtualInputs();

private:
  uint8_t inputRegister_ = 0xFF;
  uint8_t outputRegister_ = 0xFF;
  uint8_t virtualInputs_ = 0xFF;
};

// shiftRegisters is the single board-wide shift-register service.
extern ShiftRegisters shiftRegisters;
