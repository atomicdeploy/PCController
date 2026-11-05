#define WIN32_LEAN_AND_MEAN
#define _WIN32_WINNT 0x0601
#define RAYANLAMP_BUILD_DLL

#include "RayanLamp.h"
#include "RayanLampGo.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

static INIT_ONCE module_pin_once = INIT_ONCE_STATIC_INIT;
static BOOL module_pin_succeeded = FALSE;

static BOOL CALLBACK pin_module_once(
    PINIT_ONCE init_once,
    PVOID parameter,
    PVOID *context
) {
    HMODULE module = NULL;
    (void)init_once;
    (void)parameter;
    (void)context;
    module_pin_succeeded = GetModuleHandleExW(
        GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS |
            GET_MODULE_HANDLE_EX_FLAG_PIN,
        (LPCWSTR)(uintptr_t)&RayanLamp_ABIVersion,
        &module
    );
    return TRUE;
}

static int ensure_module_pinned(void) {
    if (!InitOnceExecuteOnce(
        &module_pin_once,
        pin_module_once,
        NULL,
        NULL
    )) {
        return 0;
    }
    return module_pin_succeeded ? 1 : 0;
}

static void clear_buffer(RAYANLAMP_BUFFER *buffer) {
    if (buffer != NULL) {
        buffer->data = NULL;
        buffer->length = 0;
    }
}

static void clear_wbuffer(RAYANLAMP_WBUFFER *buffer) {
    if (buffer != NULL) {
        buffer->data = NULL;
        buffer->length = 0;
    }
}

static void set_status_last_error(int32_t status) {
    switch (status) {
        case RAYANLAMP_STATUS_OK:
            SetLastError(ERROR_SUCCESS);
            break;
        case RAYANLAMP_STATUS_INVALID_ARGUMENT:
            SetLastError(ERROR_INVALID_PARAMETER);
            break;
        case RAYANLAMP_STATUS_UNAVAILABLE:
            SetLastError(ERROR_PIPE_NOT_CONNECTED);
            break;
        case RAYANLAMP_STATUS_TIMEOUT:
            SetLastError(ERROR_TIMEOUT);
            break;
        case RAYANLAMP_STATUS_UNSUPPORTED:
            SetLastError(ERROR_NOT_SUPPORTED);
            break;
        default:
            SetLastError(ERROR_GEN_FAILURE);
            break;
    }
}

static int utf16_to_utf8(
    const wchar_t *input,
    size_t input_length,
    size_t maximum_output_length,
    char **output,
    size_t *output_length
) {
    int required;
    char *converted;

    if (output == NULL || output_length == NULL) {
        return 0;
    }
    *output = NULL;
    *output_length = 0;
    if (input_length == 0) {
        converted = (char *)malloc(1);
        if (converted == NULL) {
            return 0;
        }
        converted[0] = '\0';
        *output = converted;
        return 1;
    }
    if (input == NULL || input_length > INT_MAX) {
        return 0;
    }
    required = WideCharToMultiByte(
        CP_UTF8,
        WC_ERR_INVALID_CHARS,
        input,
        (int)input_length,
        NULL,
        0,
        NULL,
        NULL
    );
    if (required <= 0 ||
        (size_t)required > maximum_output_length ||
        (size_t)required >= SIZE_MAX) {
        return 0;
    }
    converted = (char *)malloc((size_t)required + 1);
    if (converted == NULL) {
        return 0;
    }
    if (WideCharToMultiByte(
        CP_UTF8,
        WC_ERR_INVALID_CHARS,
        input,
        (int)input_length,
        converted,
        required,
        NULL,
        NULL
    ) != required) {
        free(converted);
        return 0;
    }
    converted[required] = '\0';
    *output = converted;
    *output_length = (size_t)required;
    return 1;
}

static int utf8_to_utf16(
    const char *input,
    size_t input_length,
    wchar_t **output,
    size_t *output_length
) {
    int required;
    wchar_t *converted;

    if (output == NULL || output_length == NULL) {
        return 0;
    }
    *output = NULL;
    *output_length = 0;
    if (input_length == 0) {
        converted = (wchar_t *)calloc(1, sizeof(wchar_t));
        if (converted == NULL) {
            return 0;
        }
        *output = converted;
        return 1;
    }
    if (input == NULL || input_length > INT_MAX) {
        return 0;
    }
    required = MultiByteToWideChar(
        CP_UTF8,
        MB_ERR_INVALID_CHARS,
        input,
        (int)input_length,
        NULL,
        0
    );
    if (required <= 0 ||
        (size_t)required > (SIZE_MAX / sizeof(wchar_t)) - 1) {
        return 0;
    }
    converted = (wchar_t *)calloc(
        (size_t)required + 1,
        sizeof(wchar_t)
    );
    if (converted == NULL) {
        return 0;
    }
    if (MultiByteToWideChar(
        CP_UTF8,
        MB_ERR_INVALID_CHARS,
        input,
        (int)input_length,
        converted,
        required
    ) != required) {
        free(converted);
        return 0;
    }
    *output = converted;
    *output_length = (size_t)required;
    return 1;
}

uint32_t WINAPI RayanLamp_ABIVersion(void) {
    if (!ensure_module_pinned()) {
        SetLastError(ERROR_DLL_INIT_FAILED);
        return 0;
    }
    SetLastError(ERROR_SUCCESS);
    return RayanLampGoABIVersion();
}

int32_t WINAPI RayanLamp_IsRunning(uint32_t timeout_milliseconds) {
    int32_t status;
    if (!ensure_module_pinned()) {
        SetLastError(ERROR_DLL_INIT_FAILED);
        return RAYANLAMP_STATUS_INTERNAL;
    }
    status = RayanLampGoIsRunning(timeout_milliseconds);
    if (status == 1) {
        SetLastError(ERROR_SUCCESS);
    } else if (status == 0) {
        SetLastError(ERROR_PIPE_NOT_CONNECTED);
    } else {
        set_status_last_error(status);
    }
    return status;
}

int32_t WINAPI RayanLamp_EnsurePrimaryW(
    const wchar_t *executable_path,
    size_t executable_path_length,
    uint32_t timeout_milliseconds
) {
    static const wchar_t arguments[] = L" /background";
    STARTUPINFOW startup = {0};
    PROCESS_INFORMATION process = {0};
    wchar_t *path = NULL;
    wchar_t *command_line = NULL;
    size_t command_characters;
    uint64_t deadline;
    uint32_t effective_timeout;
    DWORD attributes;
    DWORD create_error;
    size_t index;
    int32_t running;

    if (timeout_milliseconds == 0) {
        effective_timeout = 12000;
    } else if (timeout_milliseconds < 100 ||
               timeout_milliseconds > 600000) {
        set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
        return RAYANLAMP_STATUS_INVALID_ARGUMENT;
    } else {
        effective_timeout = timeout_milliseconds;
    }

    running = RayanLamp_IsRunning(100);
    if (running == 1) {
        return RAYANLAMP_STATUS_OK;
    }
    if (running < 0 && running != RAYANLAMP_STATUS_TIMEOUT) {
        set_status_last_error(running);
        return running;
    }
    if (executable_path == NULL ||
        executable_path_length == 0 ||
        executable_path_length > 32700) {
        set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
        return RAYANLAMP_STATUS_INVALID_ARGUMENT;
    }
    for (index = 0; index < executable_path_length; ++index) {
        if (executable_path[index] == L'\0' ||
            executable_path[index] == L'"') {
            set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
            return RAYANLAMP_STATUS_INVALID_ARGUMENT;
        }
    }
    if (executable_path_length > SIZE_MAX / sizeof(wchar_t) - 1) {
        set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
        return RAYANLAMP_STATUS_INVALID_ARGUMENT;
    }
    path = (wchar_t *)calloc(
        executable_path_length + 1,
        sizeof(wchar_t)
    );
    if (path == NULL) {
        set_status_last_error(RAYANLAMP_STATUS_INTERNAL);
        return RAYANLAMP_STATUS_INTERNAL;
    }
    memcpy(
        path,
        executable_path,
        executable_path_length * sizeof(wchar_t)
    );
    attributes = GetFileAttributesW(path);
    if (attributes == INVALID_FILE_ATTRIBUTES ||
        (attributes & FILE_ATTRIBUTE_DIRECTORY) != 0) {
        create_error = GetLastError();
        free(path);
        SetLastError(
            create_error == ERROR_SUCCESS ? ERROR_FILE_NOT_FOUND : create_error
        );
        return RAYANLAMP_STATUS_UNAVAILABLE;
    }

    command_characters = executable_path_length +
        (sizeof(arguments) / sizeof(arguments[0])) + 2;
    if (command_characters > 32767 ||
        command_characters > SIZE_MAX / sizeof(wchar_t)) {
        free(path);
        set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
        return RAYANLAMP_STATUS_INVALID_ARGUMENT;
    }
    command_line = (wchar_t *)calloc(
        command_characters,
        sizeof(wchar_t)
    );
    if (command_line == NULL) {
        free(path);
        set_status_last_error(RAYANLAMP_STATUS_INTERNAL);
        return RAYANLAMP_STATUS_INTERNAL;
    }
    command_line[0] = L'"';
    memcpy(
        command_line + 1,
        path,
        executable_path_length * sizeof(wchar_t)
    );
    command_line[executable_path_length + 1] = L'"';
    memcpy(
        command_line + executable_path_length + 2,
        arguments,
        sizeof(arguments)
    );

    startup.cb = sizeof(startup);
    if (!CreateProcessW(
        path,
        command_line,
        NULL,
        NULL,
        FALSE,
        CREATE_NO_WINDOW,
        NULL,
        NULL,
        &startup,
        &process
    )) {
        create_error = GetLastError();
        free(command_line);
        free(path);
        SetLastError(create_error);
        return RAYANLAMP_STATUS_UNAVAILABLE;
    }
    free(command_line);
    free(path);
    CloseHandle(process.hThread);

    deadline = GetTickCount64() + effective_timeout;
    for (;;) {
        running = RayanLamp_IsRunning(100);
        if (running == 1) {
            CloseHandle(process.hProcess);
            SetLastError(ERROR_SUCCESS);
            return RAYANLAMP_STATUS_OK;
        }
        if (WaitForSingleObject(process.hProcess, 0) == WAIT_OBJECT_0) {
            CloseHandle(process.hProcess);
            SetLastError(ERROR_PROCESS_ABORTED);
            return RAYANLAMP_STATUS_UNAVAILABLE;
        }
        if (GetTickCount64() >= deadline) {
            CloseHandle(process.hProcess);
            set_status_last_error(RAYANLAMP_STATUS_TIMEOUT);
            return RAYANLAMP_STATUS_TIMEOUT;
        }
        Sleep(50);
    }
}

int32_t WINAPI RayanLamp_ExecuteUtf8(
    const char *method,
    size_t method_length,
    const char *params_json,
    size_t params_length,
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
) {
    char *response = NULL;
    size_t response_length = 0;
    int32_t status;

    if (output == NULL) {
        set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
        return RAYANLAMP_STATUS_INVALID_ARGUMENT;
    }
    clear_buffer(output);
    if (!ensure_module_pinned()) {
        set_status_last_error(RAYANLAMP_STATUS_INTERNAL);
        return RAYANLAMP_STATUS_INTERNAL;
    }
    status = RayanLampGoExecute(
        (char *)method,
        method_length,
        (char *)params_json,
        params_length,
        timeout_milliseconds,
        &response,
        &response_length
    );
    output->data = response;
    output->length = response_length;
    set_status_last_error(status);
    return status;
}

int32_t WINAPI RayanLamp_ExecuteW(
    const wchar_t *method,
    size_t method_length,
    const wchar_t *params_json,
    size_t params_length,
    uint32_t timeout_milliseconds,
    RAYANLAMP_WBUFFER *output
) {
    char *method_utf8 = NULL;
    size_t method_bytes = 0;
    char *params_utf8 = NULL;
    size_t params_bytes = 0;
    RAYANLAMP_BUFFER response = {0};
    int32_t status;

    if (output == NULL) {
        set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
        return RAYANLAMP_STATUS_INVALID_ARGUMENT;
    }
    clear_wbuffer(output);
    if (method == NULL ||
        method_length == 0 ||
        method_length > 64 ||
        params_length > RAYANLAMP_MAX_INPUT_BYTES ||
        method_length + params_length > RAYANLAMP_MAX_INPUT_BYTES) {
        set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
        return RAYANLAMP_STATUS_INVALID_ARGUMENT;
    }
    if (!utf16_to_utf8(
        method,
        method_length,
        64,
        &method_utf8,
        &method_bytes
    )) {
        set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
        return RAYANLAMP_STATUS_INVALID_ARGUMENT;
    }
    if (params_json != NULL || params_length != 0) {
        if (!utf16_to_utf8(
            params_json,
            params_length,
            RAYANLAMP_MAX_INPUT_BYTES - method_bytes,
            &params_utf8,
            &params_bytes
        )) {
            free(method_utf8);
            set_status_last_error(RAYANLAMP_STATUS_INVALID_ARGUMENT);
            return RAYANLAMP_STATUS_INVALID_ARGUMENT;
        }
    }
    status = RayanLamp_ExecuteUtf8(
        method_utf8,
        method_bytes,
        params_utf8,
        params_bytes,
        timeout_milliseconds,
        &response
    );
    free(method_utf8);
    free(params_utf8);

    if (response.data != NULL) {
        if (!utf8_to_utf16(
            response.data,
            response.length,
            &output->data,
            &output->length
        )) {
            RayanLamp_Free(response.data);
            clear_wbuffer(output);
            set_status_last_error(RAYANLAMP_STATUS_INTERNAL);
            return RAYANLAMP_STATUS_INTERNAL;
        }
        RayanLamp_Free(response.data);
    }
    set_status_last_error(status);
    return status;
}

int32_t WINAPI RayanLamp_On(
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
) {
    static const char method[] = "lamp.on";
    return RayanLamp_ExecuteUtf8(
        method,
        sizeof(method) - 1,
        NULL,
        0,
        timeout_milliseconds,
        output
    );
}

int32_t WINAPI RayanLamp_Off(
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
) {
    static const char method[] = "lamp.off";
    return RayanLamp_ExecuteUtf8(
        method,
        sizeof(method) - 1,
        NULL,
        0,
        timeout_milliseconds,
        output
    );
}

int32_t WINAPI RayanLamp_Toggle(
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
) {
    static const char method[] = "lamp.toggle";
    return RayanLamp_ExecuteUtf8(
        method,
        sizeof(method) - 1,
        NULL,
        0,
        timeout_milliseconds,
        output
    );
}

int32_t WINAPI RayanLamp_GetState(
    uint32_t timeout_milliseconds,
    RAYANLAMP_BUFFER *output
) {
    static const char method[] = "status";
    return RayanLamp_ExecuteUtf8(
        method,
        sizeof(method) - 1,
        NULL,
        0,
        timeout_milliseconds,
        output
    );
}

void WINAPI RayanLamp_Free(void *memory) {
    free(memory);
}
