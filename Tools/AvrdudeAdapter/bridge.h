/* SPDX-License-Identifier: GPL-2.0-or-later */
#ifndef PCCONTROLLER_AVRDUDE_BRIDGE_H
#define PCCONTROLLER_AVRDUDE_BRIDGE_H
#include <stdint.h>
/* All calls, including callbacks and cleanup, must be serialized by the host.
 * No Go pointers are retained: user is a runtime/cgo.Handle integer. */
typedef void (*pcc_avr_event)(uintptr_t user, const char *type, int value, const char *text);
typedef int (*pcc_avr_cancelled)(uintptr_t user);
int pcc_avr_run(int argc, char **argv, uintptr_t user,
  pcc_avr_event event, pcc_avr_cancelled cancelled);
#endif
