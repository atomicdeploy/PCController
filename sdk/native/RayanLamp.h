#ifndef YEKTAYAR_RAYANLAMP_SDK_H
#define YEKTAYAR_RAYANLAMP_SDK_H

#include <stddef.h>
#include <stdint.h>
#include <windows.h>

#define RAYANLAMP_ABI_VERSION UINT32_C(0x00010000)
#define RAYANLAMP_MAX_INPUT_BYTES (UINT32_C(1) << 20)

#if defined(RAYANLAMP_BUILD_DLL)
#define RAYANLAMP_API
#else
#define RAYANLAMP_API __declspec(dllimport)
#endif

#ifdef __cplusplus
extern "C" {
#endif

typedef enum RAYANLAMP_STATUS {
    RAYANLAMP_STATUS_OK = 0,
    RAYANLAMP_STATUS_INVALID_ARGUMENT = -1,
    RAYANLAMP_STATUS_UNAVAILABLE = -2,
    RAYANLAMP_STATUS_TIMEOUT = -3,
    RAYANLAMP_STATUS_UNSUPPORTED = -4,
    RAYANLAMP_STATUS_INTERNAL = -5
} RAYANLAMP_STATUS;

/*
 * Returned buffers are NUL terminated for convenience. length excludes that
 * terminator and may include embedded NUL bytes in future protocol values.
 * Release data only with RayanLamp_Free.
 */
typedef struct RAYANLAMP_BUFFER {
    char *data;
    size_t length;
} RAYANLAMP_BUFFER;

typedef struct RAYANLAMP_WBUFFER {
    wchar_t *data;
    size_t length;
} RAYANLAMP_WBUFFER;

/*
 * The first SDK call pins the Go-backed module for process lifetime. Hosts may
 * balance their own LoadLibrary with FreeLibrary, but must treat the SDK as a
 * process-lifetime runtime and must not attempt to force-unmap it.
 */

/*
 * Returns the binary ABI as (major << 16) | minor. The ABI is distinct from
 * the living, versionless command protocol carried in JSON.
 */
RAYANLAMP_API uint32_t WINAPI RayanLamp_ABIVersion(void);

/*
 * Returns 1 when the current user's authoritative primary answers, 0 when it
 * does not, or a negative RAYANLAMP_STATUS for invalid ABI input.
 */
RAYANLAMP_API int32_t WINAPI RayanLamp_IsRunning(
    uint32_t timeout_milliseconds
);

/*
 * Explicitly launches the exact supplied unified RayanLamp.exe with
 * /background when no primary is running, then waits for named-pipe readiness.
 * executable_path is never inferred and is not searched through PATH.
 *
 * This is opt-in bootstrap only: Execute calls never launch a process and the
 * DLL never turns its host into an embedded/invisible primary.
 */
RAYANLAMP_API int32_t WINAPI RayanLamp_EnsurePrimaryW(
    const wchar_t *executable_path,
    size_t executable_path_length,
    uint32_t timeout_milliseconds
);

/*
 * Executes one dispatcher method. params_json may be NULL only when
 * params_length is zero. timeout_milliseconds=0 selects the 12-second default;
 * otherwise the accepted range is 100..600000.
 *
 * A return value of RAYANLAMP_STATUS_OK means the transport completed. Inspect
 * the returned {"ok":...} JSON because the dispatcher may still reject a
 * command. On ABI/transport failure, a structured JSON error is returned when
 * allocation succeeds. Free every non-NULL output->data regardless of status.
 */
RAYANLAMP_API int32_t WINAPI RayanLamp_ExecuteUtf8(
    const char *method,
    size_t method_length,
    const char *params_json,
    size_t params_length,
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
);

/*
 * UTF-16 counterpart to RayanLamp_ExecuteUtf8. Input lengths are UTF-16 code
 * units and output.length excludes its trailing UTF-16 NUL.
 */
RAYANLAMP_API int32_t WINAPI RayanLamp_ExecuteW(
    const wchar_t *method,
    size_t method_length,
    const wchar_t *params_json,
    size_t params_length,
    uint32_t timeout_milliseconds,
    RAYANLAMP_WBUFFER *output
);

RAYANLAMP_API int32_t WINAPI RayanLamp_On(
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
);
RAYANLAMP_API int32_t WINAPI RayanLamp_Off(
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
);
RAYANLAMP_API int32_t WINAPI RayanLamp_Toggle(
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
);
RAYANLAMP_API int32_t WINAPI RayanLamp_GetState(
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
);

RAYANLAMP_API void WINAPI RayanLamp_Free(void *memory);

#ifdef __cplusplus
}
#endif

#endif
