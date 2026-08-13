#pragma once

#include <cstdint>

#ifndef PROGMEM
#define PROGMEM
#endif

inline std::uint16_t pgm_read_word(const void *address) {
  return *static_cast<const std::uint16_t *>(address);
}
