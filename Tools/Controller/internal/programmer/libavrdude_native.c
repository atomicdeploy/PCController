//go:build cgo && libavrdude

/* SPDX-License-Identifier: GPL-2.0-or-later */
#include "../../../AvrdudeAdapter/bridge.h"
extern void pccGoAvrEvent(uintptr_t user, char *type, int value, char *text);
extern int pccGoAvrCancelled(uintptr_t user);
static void forward_event(uintptr_t user, const char *type, int value, const char *text) {
  pccGoAvrEvent(user, (char *)type, value, (char *)text);
}
int pcc_go_avr_run(int argc, char **argv, uintptr_t user) {
  return pcc_avr_run(argc, argv, user, forward_event, pccGoAvrCancelled);
}
