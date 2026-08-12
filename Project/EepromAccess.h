#pragma once

#if defined(__AVR__)
#include <avr/eeprom.h>
#elif defined(PCCONTROLLER_NATIVE_TEST)
#include <EEPROM.h>
#endif

// AVR-libc EEPROM writes complete asynchronously. Cooperative owners must
// observe readiness before starting the next byte and before publishing their
// persisted/not-busy state. Native tests expose the same gate via the mock.
inline bool controllerEepromReady() {
#if defined(__AVR__)
  return eeprom_is_ready();
#elif defined(PCCONTROLLER_NATIVE_TEST)
  return EEPROM.ready();
#else
  return true;
#endif
}
