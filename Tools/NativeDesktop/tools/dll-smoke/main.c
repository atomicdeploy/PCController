#define WIN32_LEAN_AND_MEAN

#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <windows.h>

#include "PCControllerDesktop.h"

typedef uint32_t (WINAPI *abi_version_fn)(void);
typedef int32_t (WINAPI *is_running_fn)(uint32_t);
typedef int32_t (WINAPI *ensure_primary_fn)(
    const wchar_t *,
    size_t,
    uint32_t
);
typedef int32_t (WINAPI *execute_utf8_fn)(
    const char *,
    size_t,
    const char *,
    size_t,
    uint32_t,
    PCCONTROLLER_DESKTOP_BUFFER *
);
typedef int32_t (WINAPI *execute_w_fn)(
    const wchar_t *,
    size_t,
    const wchar_t *,
    size_t,
    uint32_t,
    PCCONTROLLER_DESKTOP_WBUFFER *
);
typedef void (WINAPI *free_fn)(void *);

static FARPROC required_export(HMODULE module, const char *name) {
    FARPROC address = GetProcAddress(module, name);
    if (address == NULL) {
        fprintf(stderr, "missing export: %s\n", name);
    }
    return address;
}

int wmain(int argc, wchar_t **argv) {
    static const char *other_exports[] = {
        "PCControllerDesktop_On",
        "PCControllerDesktop_Off",
        "PCControllerDesktop_Toggle",
        "PCControllerDesktop_GetState"
    };
    HMODULE module;
    abi_version_fn abi_version;
    is_running_fn is_running;
    ensure_primary_fn ensure_primary;
    execute_utf8_fn execute_utf8;
    execute_w_fn execute_w;
    free_fn release;
    PCCONTROLLER_DESKTOP_BUFFER output = {0};
    PCCONTROLLER_DESKTOP_WBUFFER wide_output = {0};
    wchar_t oversized_method[65];
    int32_t status;
    size_t index;

    if (argc != 2) {
        fputs("usage: dll-smoke.exe PATH_TO_PCCONTROLLER_DESKTOP_DLL\n", stderr);
        return 2;
    }
    module = LoadLibraryW(argv[1]);
    if (module == NULL) {
        fprintf(stderr, "LoadLibraryW failed: %lu\n", GetLastError());
        return 3;
    }

    abi_version = (abi_version_fn)required_export(
        module,
        "PCControllerDesktop_ABIVersion"
    );
    is_running = (is_running_fn)required_export(
        module,
        "PCControllerDesktop_IsRunning"
    );
    ensure_primary = (ensure_primary_fn)required_export(
        module,
        "PCControllerDesktop_EnsurePrimaryW"
    );
    execute_utf8 = (execute_utf8_fn)required_export(
        module,
        "PCControllerDesktop_ExecuteUtf8"
    );
    execute_w = (execute_w_fn)required_export(
        module,
        "PCControllerDesktop_ExecuteW"
    );
    release = (free_fn)required_export(module, "PCControllerDesktop_Free");
    for (index = 0;
         index < sizeof(other_exports) / sizeof(other_exports[0]);
         ++index) {
        if (required_export(module, other_exports[index]) == NULL) {
            FreeLibrary(module);
            return 4;
        }
    }
    if (abi_version == NULL ||
        is_running == NULL ||
        ensure_primary == NULL ||
        execute_utf8 == NULL ||
        execute_w == NULL ||
        release == NULL) {
        FreeLibrary(module);
        return 4;
    }
    if (abi_version() != PCCONTROLLER_DESKTOP_ABI_VERSION) {
        fputs("ABI version mismatch\n", stderr);
        FreeLibrary(module);
        return 5;
    }
    if (is_running(99) != PCCONTROLLER_DESKTOP_STATUS_INVALID_ARGUMENT ||
        GetLastError() != ERROR_INVALID_PARAMETER) {
        fputs("invalid timeout contract failed\n", stderr);
        FreeLibrary(module);
        return 6;
    }

    status = execute_utf8(NULL, 0, NULL, 0, 0, &output);
    if (status != PCCONTROLLER_DESKTOP_STATUS_INVALID_ARGUMENT ||
        output.data == NULL ||
        output.length == 0 ||
        output.data[output.length] != '\0' ||
        strstr(output.data, "\"ok\":false") == NULL ||
        GetLastError() != ERROR_INVALID_PARAMETER) {
        fputs("UTF-8 invalid-input contract failed\n", stderr);
        release(output.data);
        FreeLibrary(module);
        return 7;
    }
    release(output.data);
    output.data = NULL;

    for (index = 0; index < 65; ++index) {
        oversized_method[index] = L'x';
    }
    status = execute_w(
        oversized_method,
        65,
        NULL,
        0,
        100,
        &wide_output
    );
    if (status != PCCONTROLLER_DESKTOP_STATUS_INVALID_ARGUMENT ||
        wide_output.data != NULL ||
        GetLastError() != ERROR_INVALID_PARAMETER) {
        fputs("UTF-16 method bound failed\n", stderr);
        release(wide_output.data);
        FreeLibrary(module);
        return 8;
    }
    status = execute_w(
        L"ping",
        4,
        L"{}",
        PCCONTROLLER_DESKTOP_MAX_INPUT_BYTES + 1,
        100,
        &wide_output
    );
    if (status != PCCONTROLLER_DESKTOP_STATUS_INVALID_ARGUMENT ||
        wide_output.data != NULL ||
        GetLastError() != ERROR_INVALID_PARAMETER) {
        fputs("UTF-16 parameter bound failed\n", stderr);
        release(wide_output.data);
        FreeLibrary(module);
        return 9;
    }

    status = execute_w(L"ping", 4, NULL, 0, 100, &wide_output);
    if ((status != PCCONTROLLER_DESKTOP_STATUS_OK &&
         status != PCCONTROLLER_DESKTOP_STATUS_UNAVAILABLE &&
         status != PCCONTROLLER_DESKTOP_STATUS_TIMEOUT) ||
        wide_output.data == NULL ||
        wide_output.length == 0 ||
        wide_output.data[wide_output.length] != L'\0') {
        fputs("UTF-16 execution contract failed\n", stderr);
        release(wide_output.data);
        FreeLibrary(module);
        return 10;
    }
    release(wide_output.data);
    release(NULL);
    FreeLibrary(module);
    puts("PCControllerDesktop SDK DLL smoke test passed.");
    return 0;
}
