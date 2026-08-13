#include "ShiftRegisters.h"

#if defined(__AVR__)
#include <avr/io.h>
#include <util/delay.h>
#endif

ShiftRegisters shiftRegisters;

void ShiftRegisters::begin() {
#if defined(__AVR__)
  // Fixed ControllerBoardMini wiring: PC0 input, PC1..3 outputs, PD4/5/7
  // controls, and PB0 active-low output enable. Direct port setup avoids the
  // generic Arduino pin lookup on every shift-register edge.
  DDRC = static_cast<uint8_t>((DDRC & ~_BV(DDC0)) |
                              _BV(DDC1) | _BV(DDC2) | _BV(DDC3));
  PORTC = static_cast<uint8_t>((PORTC | _BV(PORTC0)) &
                               ~(_BV(PORTC1) | _BV(PORTC2) | _BV(PORTC3)));
  DDRD |= static_cast<uint8_t>(_BV(DDD4) | _BV(DDD5) | _BV(DDD7));
  PORTD |= static_cast<uint8_t>(_BV(PORTD4) | _BV(PORTD5) | _BV(PORTD7));
  DDRB &= static_cast<uint8_t>(~_BV(DDB0));
  PORTB |= _BV(PORTB0);
#else
  pinMode(BoardPins::ShiftDataIn, INPUT_PULLUP);
  pinMode(BoardPins::ShiftDataOut, OUTPUT);
  pinMode(BoardPins::ShiftClock, OUTPUT);
  pinMode(BoardPins::ShiftInputLoad, OUTPUT);
  pinMode(BoardPins::ShiftOutputLatch, OUTPUT);

  // Hold both chains in a safe state while their control pins are configured.
  digitalWrite(BoardPins::ShiftClock, LOW);
  digitalWrite(BoardPins::ShiftInputLoad, HIGH);

  pinMode(BoardPins::ShiftOutputEnable, INPUT_PULLUP);
  digitalWrite(BoardPins::ShiftOutputEnable, HIGH);
  pinMode(BoardPins::ShiftMasterReset, OUTPUT);
  digitalWrite(BoardPins::ShiftMasterReset, HIGH);
  pinMode(BoardPins::ShiftClockEnable, OUTPUT);
  digitalWrite(BoardPins::ShiftClockEnable, HIGH);
#endif

  allOutputsOff();
  service();

#if defined(__AVR__)
  PORTB &= static_cast<uint8_t>(~_BV(PORTB0));
  DDRB |= _BV(DDB0);
#else
  pinMode(BoardPins::ShiftOutputEnable, OUTPUT);
  digitalWrite(BoardPins::ShiftOutputEnable, LOW);
#endif
}

void ShiftRegisters::service() {
#if defined(__AVR__)
  // Shift the active-low 74HC595 byte LSB first, then latch it.
  PORTC &= static_cast<uint8_t>(~(_BV(PORTC2) | _BV(PORTC1)));
  uint8_t outputs = outputRegister_;
  for (uint8_t bit = 0; bit < 8; ++bit) {
    if ((outputs & 1U) != 0) {
      PORTC |= _BV(PORTC3);
    } else {
      PORTC &= static_cast<uint8_t>(~_BV(PORTC3));
    }
    PORTC |= _BV(PORTC2);
    PORTC &= static_cast<uint8_t>(~_BV(PORTC2));
    outputs >>= 1;
  }
  PORTC |= _BV(PORTC1);

  // Capture the 74HC165, read its first QH bit while CLK is already high,
  // then use each following rising edge to advance MSB first.
  PORTD |= _BV(PORTD7);
  PORTD &= static_cast<uint8_t>(~_BV(PORTD4));
  _delay_us(5);
  PORTD |= _BV(PORTD4);
  PORTC |= _BV(PORTC2);
  PORTD &= static_cast<uint8_t>(~_BV(PORTD7));
  uint8_t inputs = 0;
  for (uint8_t bit = 0; bit < 8; ++bit) {
    inputs = static_cast<uint8_t>((inputs << 1) |
                                  ((PINC & _BV(PINC0)) != 0 ? 1U : 0U));
    PORTC &= static_cast<uint8_t>(~_BV(PORTC2));
    if (bit != 7) {
      PORTC |= _BV(PORTC2);
    }
  }
  inputRegister_ = inputs;
  PORTD |= _BV(PORTD7);
#else
  // Update the active-low 74HC595 outputs.
  digitalWrite(BoardPins::ShiftClock, LOW);
  digitalWrite(BoardPins::ShiftOutputLatch, LOW);
  shiftOut(BoardPins::ShiftDataOut, BoardPins::ShiftClock, LSBFIRST,
           outputRegister_);
  digitalWrite(BoardPins::ShiftOutputLatch, HIGH);
  digitalWrite(BoardPins::ShiftClock, LOW);

  // Capture and read the active-low 74HC165 inputs.
  digitalWrite(BoardPins::ShiftClockEnable, HIGH);
  digitalWrite(BoardPins::ShiftInputLoad, LOW);
  delayMicroseconds(5);
  digitalWrite(BoardPins::ShiftInputLoad, HIGH);
  digitalWrite(BoardPins::ShiftClock, HIGH);
  digitalWrite(BoardPins::ShiftClockEnable, LOW);
  inputRegister_ =
      shiftIn(BoardPins::ShiftDataIn, BoardPins::ShiftClock, MSBFIRST);
  digitalWrite(BoardPins::ShiftClockEnable, HIGH);
#endif
}

bool ShiftRegisters::inputActive(uint8_t bit) const {
  if (bit > 7) {
    return false;
  }
  return (activeInputs() & _BV(bit)) != 0;
}

uint8_t ShiftRegisters::activeInputs() const {
  return static_cast<uint8_t>(~(inputRegister_ & virtualInputs_));
}

uint8_t ShiftRegisters::rawInputs() const { return inputRegister_; }

void ShiftRegisters::setOutput(uint8_t bit, bool active) {
  if (bit > 7) {
    return;
  }
  if (active) {
    outputRegister_ &= static_cast<uint8_t>(~_BV(bit));
  } else {
    outputRegister_ |= _BV(bit);
  }
}

void ShiftRegisters::setActiveOutputs(uint8_t activeMask) {
  outputRegister_ = static_cast<uint8_t>(~activeMask);
}

void ShiftRegisters::allOutputsOff() { outputRegister_ = 0xFF; }

uint8_t ShiftRegisters::activeOutputs() const {
  return static_cast<uint8_t>(~outputRegister_);
}

void ShiftRegisters::setVirtualInput(uint8_t bit, bool active) {
  if (bit > 7) {
    return;
  }
  if (active) {
    virtualInputs_ &= static_cast<uint8_t>(~_BV(bit));
  } else {
    virtualInputs_ |= _BV(bit);
  }
}

void ShiftRegisters::clearVirtualInputs() { virtualInputs_ = 0xFF; }
