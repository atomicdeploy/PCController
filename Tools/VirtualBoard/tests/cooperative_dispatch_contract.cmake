file(READ
  "${PCCONTROLLER_ROOT}/Project/Firmware/LifecycleRuntime.inc.h"
  lifecycle_source
)

string(FIND "${lifecycle_source}" "void serviceController()" service_start)
if(service_start LESS 0)
  message(FATAL_ERROR "serviceController() not found")
endif()
string(SUBSTRING "${lifecycle_source}" ${service_start} -1 service_source)

string(FIND "${service_source}" "appProtocol.service();" uart_position)
string(FIND "${service_source}" "const bool i2cReserved = i2cLeaseActive(loopNow);" lease_position)
string(FIND "${service_source}" "serviceRadio();" rf_position)
string(FIND "${service_source}" "serviceShiftRegisterAndKeys(loopNow);" key_position)
string(FIND "${service_source}" "if (macroPlayback.dequeueDue(queuedMacroFrame))" macro_position)

if(uart_position LESS 0 OR lease_position LESS 0 OR rf_position LESS 0 OR key_position LESS 0 OR
   macro_position LESS 0)
  message(FATAL_ERROR
    "cooperative input/macro service marker is missing from serviceController()")
endif()
if(NOT uart_position LESS lease_position)
  message(FATAL_ERROR
    "I2C lease snapshot must occur after same-turn UART/I2cTransfer dispatch")
endif()
if(NOT (uart_position LESS rf_position AND rf_position LESS key_position AND
        key_position LESS macro_position))
  message(FATAL_ERROR
    "virtual, RF, and physical input must each run before one macro dispatch")
endif()

string(FIND "${service_source}"
  "(hostLcdFlags & HOST_LCD_OFFLINE) == 0 &&\n      !i2cReserved"
  offline_lcd_lease_guard)
if(offline_lcd_lease_guard LESS 0)
  message(FATAL_ERROR
    "host-offline LCD shift/marker is not deferred for an active I2C lease")
endif()

file(READ
  "${PCCONTROLLER_ROOT}/Project/Firmware/ProtocolRuntime.inc.h"
  protocol_source
)
string(REGEX MATCHALL
  "if[ \t\r\n]*\\(i2cLeaseActive\\(frameNow\\)\\)[ \t\r\n]*\\{[ \t\r\n]*goto busy;"
  lease_busy_guards
  "${protocol_source}"
)
list(LENGTH lease_busy_guards lease_busy_guard_count)
if(lease_busy_guard_count LESS 3)
  message(FATAL_ERROR
    "PwmSet/PwmAllOff/StatusRgb must reject while generic I2C is leased")
endif()

file(READ
  "${PCCONTROLLER_ROOT}/Project/Firmware/ControllerUtilities.inc.h"
  utility_source
)
string(REGEX MATCH
  "if[ \t\r\n]*\\(learningActive[^)]*\\)[ \t\r\n]*\\{[ \t\r\n]*return;"
  learning_blocks_persistence
  "${utility_source}"
)
if(learning_blocks_persistence)
  message(FATAL_ERROR
    "RF learning must not deadlock its own cooperative EEPROM transaction")
endif()
foreach(required_stop
    "relays.allOff(now);"
    "pwm.tryAllOff();"
    "pwm.setPowerSignal(true);"
    "buzzer.stop();"
    "AddressableLeds::clear();"
    "AddressableLeds::show();"
    "statusLeds.cancelEffect();"
    "clearHostSegmentText();")
  string(FIND "${utility_source}" "${required_stop}" stop_position)
  if(stop_position LESS 0)
    message(FATAL_ERROR
      "macro cancel/failure safe stop lost required domain: ${required_stop}")
  endif()
endforeach()
string(FIND "${utility_source}"
  "if (i2cLeaseActive(now))"
  safe_stop_lease_guard)
string(FIND "${service_source}"
  "serviceDeferredMacroPwmSafeStop(i2cReserved);"
  safe_stop_retry)
if(safe_stop_lease_guard LESS 0 OR safe_stop_retry LESS 0)
  message(FATAL_ERROR
    "macro PCA safe-stop is not deferred/retried across a host I2C lease")
endif()

string(REGEX MATCH
  "while[ \t\r\n]*\\(macroPlayback\\.dequeueDue"
  drains_same_due_actions
  "${service_source}"
)
if(drains_same_due_actions)
  message(FATAL_ERROR
    "macro playback drains multiple due actions in one controller loop")
endif()

message(STATUS
  "cooperative dispatch contract: UART -> RF -> physical -> one macro action")
