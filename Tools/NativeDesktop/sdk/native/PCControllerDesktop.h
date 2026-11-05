#ifndef ATOMICDEPLOY_PCCONTROLLER_DESKTOP_SDK_H
#define ATOMICDEPLOY_PCCONTROLLER_DESKTOP_SDK_H

#include <stddef.h>
#include <stdint.h>
#include <windows.h>

#define PCCONTROLLER_DESKTOP_ABI_VERSION UINT32_C(0x00010000)
#define PCCONTROLLER_DESKTOP_MAX_INPUT_BYTES (UINT32_C(1) << 20)

#if defined(PCCONTROLLER_DESKTOP_BUILD_DLL)
#define PCCONTROLLER_DESKTOP_API
#else
#define PCCONTROLLER_DESKTOP_API __declspec(dllimport)
#endif

#ifdef __cplusplus
extern "C" {
#endif

typedef enum PCCONTROLLER_DESKTOP_STATUS {
    PCCONTROLLER_DESKTOP_STATUS_OK = 0,
    PCCONTROLLER_DESKTOP_STATUS_INVALID_ARGUMENT = -1,
    PCCONTROLLER_DESKTOP_STATUS_UNAVAILABLE = -2,
    PCCONTROLLER_DESKTOP_STATUS_TIMEOUT = -3,
    PCCONTROLLER_DESKTOP_STATUS_UNSUPPORTED = -4,
    PCCONTROLLER_DESKTOP_STATUS_INTERNAL = -5
} PCCONTROLLER_DESKTOP_STATUS;

/*
 * Returned buffers are NUL terminated for convenience. length excludes that
 * terminator and may include embedded NUL bytes in future protocol values.
 * Release data only with PCControllerDesktop_Free.
 */
typedef struct PCCONTROLLER_DESKTOP_BUFFER {
    char *data;
    size_t length;
} PCCONTROLLER_DESKTOP_BUFFER;

typedef struct PCCONTROLLER_DESKTOP_WBUFFER {
    wchar_t *data;
    size_t length;
} PCCONTROLLER_DESKTOP_WBUFFER;

/*
 * The first SDK call pins the Go-backed module for process lifetime. Hosts may
 * balance their own LoadLibrary with FreeLibrary, but must treat the SDK as a
 * process-lifetime runtime and must not attempt to force-unmap it.
 */

/*
 * Returns the binary ABI as (major << 16) | minor. The ABI is distinct from
 * the living, versionless command protocol carried in JSON.
 */
PCCONTROLLER_DESKTOP_API uint32_t WINAPI PCControllerDesktop_ABIVersion(void);

/*
 * Returns 1 when the current user's authoritative primary answers, 0 when it
 * does not, or a negative PCCONTROLLER_DESKTOP_STATUS for invalid ABI input.
 */
PCCONTROLLER_DESKTOP_API int32_t WINAPI PCControllerDesktop_IsRunning(
    uint32_t timeout_milliseconds
);

/*
 * Explicitly launches the exact supplied unified PCControllerDesktop.exe with
 * /background when no primary is running, then waits for named-pipe readiness.
 * executable_path is never inferred and is not searched through PATH.
 *
 * This is opt-in bootstrap only: Execute calls never launch a process and the
 * DLL never turns its host into an embedded/invisible primary.
 */
PCCONTROLLER_DESKTOP_API int32_t WINAPI PCControllerDesktop_EnsurePrimaryW(
    const wchar_t *executable_path,
    size_t executable_path_length,
    uint32_t timeout_milliseconds
);

/*
 * Executes one dispatcher method. params_json may be NULL only when
 * params_length is zero. timeout_milliseconds=0 selects the 12-second default;
 * otherwise the accepted range is 100..600000.
 *
 * A return value of PCCONTROLLER_DESKTOP_STATUS_OK means the transport completed. Inspect
 * the returned {"ok":...} JSON because the dispatcher may still reject a
 * command. On ABI/transport failure, a structured JSON error is returned when
 * allocation succeeds. Free every non-NULL output->data regardless of status.
 */
PCCONTROLLER_DESKTOP_API int32_t WINAPI PCControllerDesktop_ExecuteUtf8(
    const char *method,
    size_t method_length,
    const char *params_json,
    size_t params_length,
    uint32_t timeout_milliseconds,
    PCCONTROLLER_DESKTOP_BUFFER *output
);

/*
 * UTF-16 counterpart to PCControllerDesktop_ExecuteUtf8. Input lengths are UTF-16 code
 * units and output.length excludes its trailing UTF-16 NUL.
 */
PCCONTROLLER_DESKTOP_API int32_t WINAPI PCControllerDesktop_ExecuteW(
    const wchar_t *method,
    size_t method_length,
    const wchar_t *params_json,
    size_t params_length,
    uint32_t timeout_milliseconds,
    PCCONTROLLER_DESKTOP_WBUFFER *output
);

PCCONTROLLER_DESKTOP_API int32_t WINAPI PCControllerDesktop_On(
    uint32_t timeout_milliseconds,
    PCCONTROLLER_DESKTOP_BUFFER *output
);
PCCONTROLLER_DESKTOP_API int32_t WINAPI PCControllerDesktop_Off(
    uint32_t timeout_milliseconds,
    PCCONTROLLER_DESKTOP_BUFFER *output
);
PCCONTROLLER_DESKTOP_API int32_t WINAPI PCControllerDesktop_Toggle(
    uint32_t timeout_milliseconds,
    PCCONTROLLER_DESKTOP_BUFFER *output
);
PCCONTROLLER_DESKTOP_API int32_t WINAPI PCControllerDesktop_GetState(
    uint32_t timeout_milliseconds,
    PCCONTROLLER_DESKTOP_BUFFER *output
);

PCCONTROLLER_DESKTOP_API void WINAPI PCControllerDesktop_Free(void *memory);

#ifdef __cplusplus
}
#endif

#endif
