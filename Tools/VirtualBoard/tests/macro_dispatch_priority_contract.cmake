file(READ
  "${PCCONTROLLER_ROOT}/Project/Runtime/LifecycleRuntime.inc.h"
  lifecycle_source
)

string(FIND "${lifecycle_source}" "void serviceController()" service_start)
if(service_start LESS 0)
  message(FATAL_ERROR "serviceController() not found")
endif()
string(SUBSTRING "${lifecycle_source}" ${service_start} -1 service_source)

string(FIND "${service_source}"
  "if (macroPlayback.dequeueDue(queuedMacroFrame))"
  macro_position)
string(FIND "${service_source}" "appProtocol.service();" uart_position)
string(FIND "${service_source}"
  "serviceShiftRegisterAndKeys(loopNow);"
  key_position)

if(macro_position LESS 0 OR uart_position LESS 0 OR key_position LESS 0)
  message(FATAL_ERROR
    "macro, UART, or physical-input service marker is missing")
endif()
if(NOT macro_position LESS uart_position)
  message(FATAL_ERROR
    "a due MCU macro action must run before ordinary UART backlog")
endif()
if(NOT uart_position LESS key_position)
  message(FATAL_ERROR
    "physical input service must retain its bounded turn after UART")
endif()

string(REGEX MATCH
  "while[ \t\r\n]*\\(macroPlayback\\.dequeueDue"
  drains_same_due_actions
  "${service_source}")
if(drains_same_due_actions)
  message(FATAL_ERROR
    "macro playback must not drain multiple due actions in one loop")
endif()

message(STATUS
  "macro dispatch contract: one due MCU action precedes ordinary UART")
