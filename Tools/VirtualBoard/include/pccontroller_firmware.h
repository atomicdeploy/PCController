#pragma once
#include <stddef.h>
#include <stdint.h>
#ifdef _WIN32
#ifdef PCCONTROLLER_FIRMWARE_EXPORTS
#define PCC_FW_API __declspec(dllexport)
#else
#define PCC_FW_API __declspec(dllimport)
#endif
#else
#define PCC_FW_API __attribute__((visibility("default")))
#endif
#ifdef __cplusplus
extern "C" {
#endif
typedef void *pcc_firmware_handle;
// One MCU per process/module lifetime. The library does not pretend setup()
// resets C++ globals. Multiple instances require isolated worker processes.
enum { PCC_FW_OK=0, PCC_FW_RESET_REQUIRED=1, PCC_FW_ERROR=-1 };
// Null/empty EEPROM path selects in-memory EEPROM; deterministic != 0 enables
// caller-driven virtual time. Diagnostics never contaminate the binary stream.
PCC_FW_API int pcc_firmware_create(const char *eeprom_path,int deterministic,pcc_firmware_handle *out);
PCC_FW_API int pcc_firmware_write(pcc_firmware_handle,const uint8_t *,size_t);
// Partial drain is lossless; NUL bytes are length-delimited. Call frequently
// for autonomous services. At RESET_REQUIRED, restart the owning process.
PCC_FW_API int pcc_firmware_poll(pcc_firmware_handle,uint64_t advance_us,uint8_t *out,size_t capacity,size_t *written);
PCC_FW_API int pcc_firmware_close(pcc_firmware_handle);
PCC_FW_API const char *pcc_firmware_error(void);
#ifdef __cplusplus
}
#endif
