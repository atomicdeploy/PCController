#pragma once

#include <Arduino.h>

// ControllerBoardMini / ATmega328P pin ownership.
//
// The aliases use MiniCore's physical-port names so the mapping remains
// unambiguous even when a different Arduino pin-number variant is selected.
namespace BoardPins {

// 74HC165 input + 74HC595 output chain.
constexpr uint8_t ShiftDataIn = PIN_PC0;       // A0, 74HC165 QH
constexpr uint8_t ShiftOutputLatch = PIN_PC1;  // A1, 74HC595 RCLK
constexpr uint8_t ShiftClock = PIN_PC2;        // A2, shared clock
constexpr uint8_t ShiftDataOut = PIN_PC3;      // A3, 74HC595 SER
constexpr uint8_t I2cSda = PIN_PC4;            // A4 / SDA
constexpr uint8_t I2cScl = PIN_PC5;            // A5 / SCL
constexpr uint8_t ShiftInputLoad = PIN_PD4;    // D4, 74HC165 /PL
constexpr uint8_t ShiftMasterReset = PIN_PD5;  // D5, 74HC595 /SRCLR
constexpr uint8_t ShiftClockEnable = PIN_PD7;  // D7, 74HC165 /CE
constexpr uint8_t ShiftOutputEnable = PIN_PB0; // D8, 74HC595 /OE

constexpr uint8_t Buzzer = PIN_PB1;            // D9
constexpr uint8_t OneWireData = PIN_PB2;       // D10/CS; DS18B20 bus needs a 4.7k pull-up to VCC
constexpr uint8_t Tm1637Data = PIN_PB3;        // D11 / MOSI
constexpr uint8_t RcTransmit = PIN_PD3;         // D3 / INT1, 433 MHz transmitter
constexpr uint8_t Tm1637Clock = PIN_PB5;       // D13 / SCK
constexpr uint8_t RcReceive = PIN_PD2;          // D2 / INT0, 433 MHz receiver

// D12 remains available for future expansion. The inherited addressable LED
// output owns D6 (PD6).
constexpr uint8_t SparePin = PIN_PB4;
constexpr uint8_t AddressableLed = PIN_PD6;     // D6, WS2811 / WS2812B data

// The first four active-low 74HC165 bits drive the menu. Inputs 4 and 5 are
// reserved system-sense positions. The final two positions are monitored
// inputs and must never be treated as keys.
constexpr uint8_t KeyPrevious = 0;
constexpr uint8_t KeyNext = 1;
constexpr uint8_t KeyDecrease = 2;
constexpr uint8_t KeyIncrease = 3;
constexpr uint8_t SystemSense1 = 4;
constexpr uint8_t SystemSense2 = 5;
constexpr uint8_t BluetoothLedSense = 6;
constexpr uint8_t DoorReedSense = 7;

// PWM is strapped to 0x41 so it cannot collide with the INA219 at 0x40.
constexpr uint8_t Ina219Address = 0x40;
constexpr uint8_t PwmAddress = 0x41;
constexpr float PwmFrequencyHz = 1000.0F;

} // namespace BoardPins
