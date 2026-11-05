//go:build cgo && libavrdude

/* SPDX-License-Identifier: GPL-2.0-or-later */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#include <windows.h>
#else
#include <dlfcn.h>
#endif
#include "../../../AvrdudeAdapter/bridge.h"

/* Return a malloc-owned UTF-8 path to this code's loaded image. This is the
 * controller executable for CLI use and the library for C ABI embedding. */
char *pcc_go_avr_module_path(void) {
#ifdef _WIN32
  HMODULE module = NULL;
  if (!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS |
                         GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
                         (LPCWSTR)&pcc_go_avr_module_path, &module)) return NULL;
  WCHAR *wide = calloc(32768, sizeof(WCHAR));
  if (!wide) return NULL;
  DWORD length = GetModuleFileNameW(module, wide, 32768);
  if (!length || length >= 32768) { free(wide); return NULL; }
  int size = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wide, -1, NULL, 0, NULL, NULL);
  char *path = size > 0 ? malloc((size_t)size) : NULL;
  if (path && !WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wide, -1, path, size, NULL, NULL)) {
    free(path); path = NULL;
  }
  free(wide);
  return path;
#else
  Dl_info info;
  if (!dladdr((void *)&pcc_go_avr_module_path, &info) || !info.dli_fname) return NULL;
  return realpath(info.dli_fname, NULL);
#endif
}
extern void pccGoAvrEvent(uintptr_t user, char *type, int value, char *text);
extern int pccGoAvrCancelled(uintptr_t user);
static void forward_event(uintptr_t user, const char *type, int value, const char *text) {
  pccGoAvrEvent(user, (char *)type, value, (char *)text);
}
int pcc_go_avr_run(int argc, char **argv, uintptr_t user) {
  return pcc_avr_run(argc, argv, user, forward_event, pccGoAvrCancelled);
}
