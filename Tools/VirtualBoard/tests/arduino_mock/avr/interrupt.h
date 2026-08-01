#pragma once

#include <cstdint>

inline std::uint8_t SREG = 0;
inline std::uint8_t TCCR1A = 0;
inline std::uint8_t TCCR1B = 0;
inline std::uint8_t TIMSK1 = 0;
inline std::uint8_t TIFR1 = 0;
inline std::uint16_t TCNT1 = 0;
inline std::uint16_t OCR1A = 0;

constexpr std::uint8_t COM1A0 = 6;
constexpr std::uint8_t WGM12 = 3;
constexpr std::uint8_t CS10 = 0;
constexpr std::uint8_t CS11 = 1;
constexpr std::uint8_t CS12 = 2;
constexpr std::uint8_t OCIE1A = 1;
constexpr std::uint8_t OCF1A = 1;

inline void cli() {}
