#include <stdio.h>
#include <string.h>
#include <windows.h>

typedef char *(__cdecl *invoke_fn)(char *);
typedef void(__cdecl *free_fn)(char *);

int main(void) {
  HMODULE library = LoadLibraryA("pccontroller.dll");
  if (library == NULL) {
    fprintf(stderr, "LoadLibrary failed: %lu\n", GetLastError());
    return 1;
  }

  invoke_fn invoke =
      (invoke_fn)(void *)GetProcAddress(library, "PCControllerInvoke");
  free_fn release =
      (free_fn)(void *)GetProcAddress(library, "PCControllerFree");
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
