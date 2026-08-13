#include <stdio.h>
#include <string.h>

#if defined(_WIN32)
#include <windows.h>
typedef char *(__cdecl *invoke_fn)(char *);
typedef void(__cdecl *free_fn)(char *);
#define PCCONTROLLER_LIBRARY "pccontroller.dll"
#else
#include <dlfcn.h>
typedef char *(*invoke_fn)(char *);
typedef void (*free_fn)(char *);
#if defined(__APPLE__)
#define PCCONTROLLER_LIBRARY "pccontroller.dylib"
#else
#define PCCONTROLLER_LIBRARY "pccontroller.so"
#endif
#endif

int main(void) {
  // Load dynamically so this stays an external C ABI consumer rather than a
  // second link-time owner of Go's process-lifetime runtime.
#if defined(_WIN32)
  HMODULE library = LoadLibraryA(PCCONTROLLER_LIBRARY);
  if (library == NULL) {
    fprintf(stderr, "LoadLibrary failed: %lu\n", GetLastError());
    return 1;
  }

  FARPROC invoke_symbol = GetProcAddress(library, "PCControllerInvoke");
  FARPROC release_symbol = GetProcAddress(library, "PCControllerFree");
  _Static_assert(sizeof(invoke_fn) == sizeof(invoke_symbol),
                 "Windows function pointers must have a uniform representation");
  _Static_assert(sizeof(free_fn) == sizeof(release_symbol),
                 "Windows function pointers must have a uniform representation");
  invoke_fn invoke;
  free_fn release;
  memcpy(&invoke, &invoke_symbol, sizeof(invoke));
  memcpy(&release, &release_symbol, sizeof(release));
#else
  void *library = dlopen("./" PCCONTROLLER_LIBRARY, RTLD_NOW | RTLD_LOCAL);
  if (library == NULL) {
    fprintf(stderr, "dlopen failed: %s\n", dlerror());
    return 1;
  }
  void *invoke_symbol = dlsym(library, "PCControllerInvoke");
  void *release_symbol = dlsym(library, "PCControllerFree");
  _Static_assert(sizeof(invoke_fn) == sizeof(invoke_symbol),
                 "POSIX function pointers must have a uniform representation");
  _Static_assert(sizeof(free_fn) == sizeof(release_symbol),
                 "POSIX function pointers must have a uniform representation");
  invoke_fn invoke;
  free_fn release;
  memcpy(&invoke, &invoke_symbol, sizeof(invoke));
  memcpy(&release, &release_symbol, sizeof(release));
#endif
  if (invoke == NULL || release == NULL) {
    fprintf(stderr, "required C ABI exports are missing\n");
    return 2;
  }

  char request[] = "{\"operation\":\"ports\"}";
  char *response = invoke(request);
  if (response == NULL) {
    fprintf(stderr, "PCControllerInvoke returned NULL\n");
    return 3;
  }
  puts(response);
  const int valid = strstr(response, "\"ok\":true") != NULL;
  release(response);

  // A Go c-shared runtime is process-lifetime state. Do not FreeLibrary here;
  // normal consumers should unload it only as part of process shutdown.
  return valid ? 0 : 4;
}
