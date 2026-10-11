cmake_minimum_required(VERSION 3.20)

if(CMAKE_HOST_WIN32 AND NOT DEFINED CMAKE_CXX_COMPILER)
  find_program(PCCONTROLLER_CLANG clang++)
  if(PCCONTROLLER_CLANG)
    set(CMAKE_CXX_COMPILER "${PCCONTROLLER_CLANG}" CACHE FILEPATH "Native firmware compiler")
  endif()
endif()
project(PCControllerNativeFirmware VERSION 1.0.0 LANGUAGES CXX)
if(MINGW)
  add_link_options(-static)
endif()
set(CMAKE_MSVC_RUNTIME_LIBRARY "MultiThreaded$<$<CONFIG:Debug>:Debug>")

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/bin")

find_package(Threads REQUIRED)

set(PCCONTROLLER_FEATURE_PROFILE "0" CACHE STRING "0=constrained, 1=expanded panel, 2=expanded autonomous")
if(NOT PCCONTROLLER_FEATURE_PROFILE MATCHES "^[012]$")
  message(FATAL_ERROR "Unknown firmware feature profile")
endif()
file(GLOB_RECURSE PCCONTROLLER_FIRMWARE_FILES CONFIGURE_DEPENDS
  "${CMAKE_CURRENT_SOURCE_DIR}/../../Project/*.h"
  "${CMAKE_CURRENT_SOURCE_DIR}/../../Project/*.cpp"
  "${CMAKE_CURRENT_SOURCE_DIR}/../../LocalLib/*.h"
  "${CMAKE_CURRENT_SOURCE_DIR}/../../LocalLib/*.cpp")
list(APPEND PCCONTROLLER_FIRMWARE_FILES
  "${CMAKE_CURRENT_SOURCE_DIR}/../../PCController.ino"
  "${CMAKE_CURRENT_SOURCE_DIR}/../../ProjectConfig.h"
  "${CMAKE_CURRENT_SOURCE_DIR}/../../BoardSupportSources.cpp"
  "${CMAKE_CURRENT_SOURCE_DIR}/../../ControllerDomainSources.cpp")
list(SORT PCCONTROLLER_FIRMWARE_FILES)
set(PCCONTROLLER_SOURCE_DIGEST "")
foreach(source IN LISTS PCCONTROLLER_FIRMWARE_FILES)
  set_property(DIRECTORY APPEND PROPERTY CMAKE_CONFIGURE_DEPENDS "${source}")
  file(SHA256 "${source}" digest)
  file(RELATIVE_PATH source_name "${CMAKE_CURRENT_SOURCE_DIR}/../.." "${source}")
  string(APPEND PCCONTROLLER_SOURCE_DIGEST "${source_name}:${digest}\n")
endforeach()
string(SHA256 PCCONTROLLER_SOURCE_SHA256 "${PCCONTROLLER_SOURCE_DIGEST}")
file(GLOB_RECURSE PCCONTROLLER_NATIVE_FILES CONFIGURE_DEPENDS
  "${CMAKE_CURRENT_SOURCE_DIR}/platform/*.h"
  "${CMAKE_CURRENT_SOURCE_DIR}/include/*.h"
  "${CMAKE_CURRENT_SOURCE_DIR}/include/*.hpp"
  "${CMAKE_CURRENT_SOURCE_DIR}/src/*.cpp")
list(SORT PCCONTROLLER_NATIVE_FILES)
set(PCCONTROLLER_NATIVE_DIGEST "")
foreach(source IN LISTS PCCONTROLLER_NATIVE_FILES)
  set_property(DIRECTORY APPEND PROPERTY CMAKE_CONFIGURE_DEPENDS "${source}")
  file(SHA256 "${source}" digest)
  file(RELATIVE_PATH source_name "${CMAKE_CURRENT_SOURCE_DIR}/../.." "${source}")
  string(APPEND PCCONTROLLER_NATIVE_DIGEST "${source_name}:${digest}\n")
endforeach()
string(SHA256 PCCONTROLLER_NATIVE_SHA256 "${PCCONTROLLER_NATIVE_DIGEST}")
string(SHA256 PCCONTROLLER_TARGET_SHA256 "${PCCONTROLLER_SOURCE_SHA256}:${PCCONTROLLER_NATIVE_SHA256}:${PCCONTROLLER_FEATURE_PROFILE}")
string(SUBSTRING "${PCCONTROLLER_TARGET_SHA256}" 0 8 PCCONTROLLER_BUILD_HASH)
file(GENERATE OUTPUT "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/firmware-manifest.json" CONTENT
  "{\"implementation\":\"PCController.ino + BoardSupportSources.cpp + ControllerDomainSources.cpp\",\"target\":\"${CMAKE_SYSTEM_NAME}-${CMAKE_SYSTEM_PROCESSOR}\",\"featureProfile\":${PCCONTROLLER_FEATURE_PROFILE},\"sourceSha256\":\"${PCCONTROLLER_SOURCE_SHA256}\",\"adapterSha256\":\"${PCCONTROLLER_NATIVE_SHA256}\",\"targetSha256\":\"${PCCONTROLLER_TARGET_SHA256}\",\"buildHash\":\"${PCCONTROLLER_BUILD_HASH}\",\"physicalAcceptance\":false}\n")
file(GENERATE OUTPUT "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/firmware-sources.sha256" CONTENT "${PCCONTROLLER_SOURCE_DIGEST}")
file(GENERATE OUTPUT "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/firmware-adapters.sha256" CONTENT "${PCCONTROLLER_NATIVE_DIGEST}")

add_library(native_firmware_objects OBJECT
  src/native_hal.cpp
  src/native_firmware.cpp
  ../../BoardSupportSources.cpp
  ../../ControllerDomainSources.cpp
)
set_target_properties(native_firmware_objects PROPERTIES POSITION_INDEPENDENT_CODE ON CXX_VISIBILITY_PRESET hidden VISIBILITY_INLINES_HIDDEN ON)
target_include_directories(native_firmware_objects PRIVATE include platform ../..)
target_compile_definitions(native_firmware_objects PRIVATE PCCONTROLLER_NATIVE=1
  PCCONTROLLER_FEATURE_PROFILE=${PCCONTROLLER_FEATURE_PROFILE}
  PCCONTROLLER_BUILD_HASH=0x${PCCONTROLLER_BUILD_HASH}UL)
add_library(virtual_board_core STATIC src/protocol.cpp $<TARGET_OBJECTS:native_firmware_objects>)
target_include_directories(virtual_board_core PUBLIC include)
target_link_libraries(virtual_board_core PUBLIC Threads::Threads)
add_library(pccontroller_firmware SHARED src/firmware_c_api.cpp $<TARGET_OBJECTS:native_firmware_objects>)
target_include_directories(pccontroller_firmware PUBLIC $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include> $<INSTALL_INTERFACE:include>)
target_compile_definitions(pccontroller_firmware PRIVATE PCCONTROLLER_FIRMWARE_EXPORTS=1)
target_link_libraries(pccontroller_firmware PRIVATE Threads::Threads)

add_executable(virtual_board
  src/main.cpp
  src/tcp_server.cpp
)
target_link_libraries(virtual_board PRIVATE virtual_board_core)

add_executable(virtual_board_smoke tests/smoke_client.cpp)
target_link_libraries(virtual_board_smoke PRIVATE virtual_board_core)

execute_process(
  COMMAND "${CMAKE_CXX_COMPILER}" -dumpmachine
  OUTPUT_VARIABLE VIRTUAL_BOARD_COMPILER_TARGET
  OUTPUT_STRIP_TRAILING_WHITESPACE
  ERROR_QUIET
)
if(WIN32 AND NOT VIRTUAL_BOARD_COMPILER_TARGET MATCHES "msys")
  target_link_libraries(virtual_board PRIVATE ws2_32)
  target_link_libraries(virtual_board_smoke PRIVATE ws2_32)
endif()

include(GNUInstallDirs)
install(TARGETS virtual_board RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})
install(TARGETS pccontroller_firmware EXPORT PCControllerFirmwareTargets
  RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
  LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
  ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR})
install(FILES include/pccontroller_firmware.h DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
install(EXPORT PCControllerFirmwareTargets NAMESPACE PCController:: DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/PCControllerFirmware)
install(FILES PCControllerFirmwareConfig.cmake DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/PCControllerFirmware)
install(FILES "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/firmware-manifest.json"
  "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/firmware-sources.sha256"
  "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/firmware-adapters.sha256"
  DESTINATION ${CMAKE_INSTALL_DATADIR}/PCController)

if(MSVC)
  target_compile_options(virtual_board_core PRIVATE /W4 /permissive-)
  target_compile_options(virtual_board PRIVATE /W4 /permissive-)
  target_compile_options(virtual_board_smoke PRIVATE /W4 /permissive-)
else()
  set(VIRTUAL_BOARD_WARNINGS
    -Wall
    -Wextra
    -Wpedantic
    -Wconversion
    -Wshadow
  )
  target_compile_options(virtual_board_core PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  target_compile_options(virtual_board PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  target_compile_options(virtual_board_smoke PRIVATE ${VIRTUAL_BOARD_WARNINGS})
endif()

include(CTest)
if(BUILD_TESTING)
  foreach(profile IN ITEMS default macro_strip)
    add_executable(firmware_${profile}_profile_tests tests/feature_profile_test.cpp)
    target_include_directories(firmware_${profile}_profile_tests PRIVATE ../..)
    add_test(NAME firmware_${profile}_profile_unit COMMAND firmware_${profile}_profile_tests)
  endforeach()
  target_compile_definitions(firmware_macro_strip_profile_tests PRIVATE PCCONTROLLER_MACRO_STRIP_TEST=1)

  add_executable(firmware_strip_tests
    tests/strip_test.cpp
    ../../Project/AddressableLeds.cpp
  )
  target_include_directories(firmware_strip_tests PRIVATE tests/arduino_mock ../..)
  add_test(NAME firmware_strip_unit COMMAND firmware_strip_tests)

  # The target-neutral wire vocabulary is compiled through both its portable
  # header and the production AVR UART adapter before native targets consume
  # it. This intentionally does not link or alter VirtualBoard behavior.
  add_executable(firmware_protocol_contract_tests
    tests/protocol_contract_test.cpp
  )
  target_include_directories(firmware_protocol_contract_tests PRIVATE ../..)
  set_property(TARGET firmware_protocol_contract_tests PROPERTY CXX_STANDARD 11)
  if(MSVC)
    target_compile_options(firmware_protocol_contract_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_protocol_contract_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_protocol_contract_unit COMMAND firmware_protocol_contract_tests)

  # Fixed-buffer codec vectors compile the same portable C++11 implementation
  # used by the AVR UART adapter, without changing VirtualBoard runtime code.
  add_executable(firmware_protocol_codec_tests
    tests/protocol_codec_test.cpp
  )
  target_include_directories(firmware_protocol_codec_tests PRIVATE ../..)
  set_property(TARGET firmware_protocol_codec_tests PROPERTY CXX_STANDARD 11)
  if(MSVC)
    target_compile_options(firmware_protocol_codec_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_protocol_codec_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_protocol_codec_unit COMMAND firmware_protocol_codec_tests)

  # Compile the persisted four-mode safety predicate as target-neutral C++11
  # before either AVR or VirtualBoard adapters consume the common contract.
  add_executable(firmware_motion_policy_contract_tests
    tests/motion_policy_contract_test.cpp
  )
  target_include_directories(firmware_motion_policy_contract_tests PRIVATE ../..)
  set_property(TARGET firmware_motion_policy_contract_tests PROPERTY CXX_STANDARD 11)
  if(MSVC)
    target_compile_options(firmware_motion_policy_contract_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_motion_policy_contract_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_motion_policy_contract_unit
    COMMAND firmware_motion_policy_contract_tests)

  # Exercise the target-neutral relay state machine with a deterministic sink
  # before the AVR adapter or a future VirtualBoard adapter consumes it.
  add_executable(firmware_relay_motion_machine_tests
    tests/relay_motion_machine_test.cpp
  )
  target_include_directories(firmware_relay_motion_machine_tests PRIVATE ../..)
  set_property(TARGET firmware_relay_motion_machine_tests PROPERTY CXX_STANDARD 11)
  if(MSVC)
    target_compile_options(firmware_relay_motion_machine_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_relay_motion_machine_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_relay_motion_machine_unit
    COMMAND firmware_relay_motion_machine_tests)

  # Compare the production ShiftRegisters/RelayController adapter against the
  # same portable trace so AVR-only glue cannot drift from the safety core.
  add_executable(firmware_relay_controller_adapter_tests
    tests/relay_controller_adapter_test.cpp
    ../../LocalLib/ShiftRegisters.cpp
    ../../Project/RelayController.cpp
  )
  target_include_directories(firmware_relay_controller_adapter_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  set_property(TARGET firmware_relay_controller_adapter_tests PROPERTY CXX_STANDARD 17)
  if(MSVC)
    target_compile_options(firmware_relay_controller_adapter_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_relay_controller_adapter_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_relay_controller_adapter_unit
    COMMAND firmware_relay_controller_adapter_tests)

  # The scheduler remains target-neutral: this C++11 trace suite compiles the
  # same bounded macro ring that the AVR/UART adapter uses, without linking or
  # altering the VirtualBoard runtime implementation.
  add_executable(firmware_macro_ring_tests
    tests/macro_ring_test.cpp
    ../../Project/Core/MacroRing.cpp
  )
  target_include_directories(firmware_macro_ring_tests PRIVATE ../..)
  set_property(TARGET firmware_macro_ring_tests PROPERTY CXX_STANDARD 11)
  if(MSVC)
    target_compile_options(firmware_macro_ring_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_macro_ring_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_macro_ring_unit COMMAND firmware_macro_ring_tests)

  # Compile the production UART adapter against the native serial double. Its
  # trace confirms that replay still reaches the ordinary protocol dispatcher.
  add_executable(firmware_macro_queue_adapter_tests
    tests/macro_queue_adapter_test.cpp
    ../../LocalLib/ShiftRegisters.cpp
    ../../Project/Core/MacroRing.cpp
    ../../Project/MacroQueue.cpp
    ../../Project/RelayController.cpp
    ../../Project/UartProtocol.cpp
  )
  target_include_directories(firmware_macro_queue_adapter_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  if(MSVC)
    target_compile_options(firmware_macro_queue_adapter_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_macro_queue_adapter_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_macro_queue_adapter_unit
           COMMAND firmware_macro_queue_adapter_tests)

  # Preserve the recovered scheduling invariant without copying the obsolete
  # firmware implementation: one due MCU action precedes ordinary UART backlog
  # and only one action is dispatched per controller loop.
  add_test(NAME firmware_macro_dispatch_priority_contract
    COMMAND ${CMAKE_COMMAND}
      -DPCCONTROLLER_ROOT=${CMAKE_CURRENT_SOURCE_DIR}/../..
      -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/macro_dispatch_priority_contract.cmake)

  add_executable(virtual_board_tests tests/native_firmware_test.cpp)
  target_compile_definitions(virtual_board_tests PRIVATE PCCONTROLLER_TEST_PROFILE=${PCCONTROLLER_FEATURE_PROFILE})
  target_link_libraries(virtual_board_tests PRIVATE virtual_board_core)
  if(MSVC)
    target_compile_options(virtual_board_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(virtual_board_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME virtual_board_unit COMMAND virtual_board_tests)
  add_test(NAME firmware_same_source_contract COMMAND ${CMAKE_COMMAND}
    -DPCCONTROLLER_ROOT=${CMAKE_CURRENT_SOURCE_DIR}/../..
    -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/same_source_contract.cmake)
  add_executable(firmware_c_api_tests tests/firmware_c_api_test.cpp src/protocol.cpp)
  target_link_libraries(firmware_c_api_tests PRIVATE pccontroller_firmware)
  add_test(NAME firmware_c_api_unit COMMAND firmware_c_api_tests)
  find_program(PCCONTROLLER_NODE node)
  if(PCCONTROLLER_NODE)
    add_test(NAME firmware_transport_integration COMMAND "${PCCONTROLLER_NODE}"
      "${CMAKE_CURRENT_SOURCE_DIR}/tests/transport_integration.test.mjs" $<TARGET_FILE:virtual_board>)
    set_tests_properties(firmware_transport_integration PROPERTIES TIMEOUT 45)
  endif()

  # Compile the production AVR framing implementation against a minimal serial
  # double so zero-copy/in-place RX behavior cannot silently drift.
  add_executable(firmware_uart_protocol_tests
    tests/uart_protocol_test.cpp
    ../../Project/UartProtocol.cpp
  )
  target_include_directories(firmware_uart_protocol_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  if(MSVC)
    target_compile_options(firmware_uart_protocol_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_uart_protocol_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_uart_protocol_unit COMMAND firmware_uart_protocol_tests)

  # Exercise production key, relay, 1-Wire, and transition primitives against
  # host-native GPIO doubles before risking a physical firmware upload.
  add_executable(firmware_controls_tests
    tests/firmware_controls_test.cpp
    ../../LocalLib/DallasTemperatureBus.cpp
    ../../LocalLib/Keys.cpp
    ../../LocalLib/SevenSegments.cpp
    ../../LocalLib/ShiftRegisters.cpp
    ../../LocalLib/TonePlayer.cpp
    ../../Project/RelayController.cpp
    ../../Project/SettingsStore.cpp
    ../../Project/UartProtocol.cpp
  )
  target_include_directories(firmware_controls_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  # Exercise the persisted page-rank compatibility path without linking or
  # changing the VirtualBoard runtime.
  target_compile_definitions(firmware_controls_tests PRIVATE
    PCCONTROLLER_MENU_VISIBILITY=1
    PCCONTROLLER_MENU_ORDERING=1
    PCCONTROLLER_SETTINGS_TEST_LAYOUT=1
  )
  if(MSVC)
    target_compile_options(firmware_controls_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_controls_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_controls_unit COMMAND firmware_controls_tests)

  # Compile the production AVR status LED compositor with deterministic
  # hardware/EEPROM doubles. This is the AVR-first same-source gate; it does
  # not alter or validate the separate VirtualBoard runtime implementation.
  add_executable(firmware_status_led_controller_tests
    tests/status_led_controller_test.cpp
    ../../Project/CompactI2c.cpp
    ../../Project/PwmController.cpp
    ../../Project/PwmExpanderDriver.cpp
    ../../Project/StatusLedController.cpp
    ../../Project/UartProtocol.cpp
  )
  target_include_directories(firmware_status_led_controller_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  set_property(TARGET firmware_status_led_controller_tests PROPERTY
    CXX_STANDARD 17)
  if(MSVC)
    target_compile_options(firmware_status_led_controller_tests PRIVATE
      /W4 /permissive-)
  else()
    target_compile_options(firmware_status_led_controller_tests PRIVATE
      ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_status_led_controller_unit
    COMMAND firmware_status_led_controller_tests)

  # Compile the production EEPROM reset journal against a byte-accurate mock.
  # This covers corrupt/torn slots and both physical/count rollover boundaries.
  add_executable(firmware_reset_telemetry_tests
    tests/reset_telemetry_test.cpp
    ../../Project/ResetTelemetry.cpp
    ../../Project/UartProtocol.cpp
  )
  target_include_directories(firmware_reset_telemetry_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  target_compile_definitions(firmware_reset_telemetry_tests PRIVATE
    PCCONTROLLER_NATIVE_TEST=1
  )
  if(MSVC)
    target_compile_options(firmware_reset_telemetry_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_reset_telemetry_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_reset_telemetry_unit COMMAND firmware_reset_telemetry_tests)

  # Validate the compile-gated EEPROM menu-label reader against a byte-accurate
  # 1 KiB EEPROM double. This is intentionally a production-AVR component test,
  # not a VirtualBoard runtime behavior fork.
  add_executable(firmware_eeprom_menu_labels_tests
    tests/eeprom_menu_labels_test.cpp
    ../../Project/EepromMenuLabels.cpp
  )
  target_include_directories(firmware_eeprom_menu_labels_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  target_compile_definitions(firmware_eeprom_menu_labels_tests PRIVATE
    PCCONTROLLER_ENABLE_EEPROM_MENU_LABELS=1
  )
  if(MSVC)
    target_compile_options(firmware_eeprom_menu_labels_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_eeprom_menu_labels_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_eeprom_menu_labels_unit COMMAND firmware_eeprom_menu_labels_tests)

  # Compile the production Timer1 engine and compact EEPROM-backed cue policy
  # together. This catches exact door/output tones, atomic CRC fallback,
  # specific-cue preemption, and the global Silent electrical invariant.
  add_executable(firmware_audio_cue_tests
    tests/audio_cue_test.cpp
    ../../LocalLib/TonePlayer.cpp
    ../../Project/AudioCues.cpp
    ../../Project/UartProtocol.cpp
  )
  target_include_directories(firmware_audio_cue_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  target_compile_definitions(firmware_audio_cue_tests PRIVATE
    PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES=1
    PCCONTROLLER_ENABLE_EEPROM_AUDIO_CUES=1
  )
  if(MSVC)
    target_compile_options(firmware_audio_cue_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_audio_cue_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_audio_cue_unit COMMAND firmware_audio_cue_tests)

  # The optional EEPROM boot-opcode runner is production AVR code compiled
  # against byte-accurate EEPROM/Arduino doubles. It does not link or alter
  # VirtualBoard runtime behavior.
  add_executable(firmware_boot_opcode_sequence_tests
    tests/boot_opcode_sequence_test.cpp
    ../../Project/BootOpcodeSequence.cpp
    ../../Project/UartProtocol.cpp
  )
  target_include_directories(firmware_boot_opcode_sequence_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  if(MSVC)
    target_compile_options(firmware_boot_opcode_sequence_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_boot_opcode_sequence_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_boot_opcode_sequence_unit COMMAND firmware_boot_opcode_sequence_tests)

  # Exercise the production dual-bank EEPROM automation table and its bounded,
  # deterministic executor against the byte-accurate EEPROM double.
  add_executable(firmware_automation_store_tests
    tests/automation_store_test.cpp
    ../../Project/AutomationStore.cpp
    ../../Project/UartProtocol.cpp
  )
  target_include_directories(firmware_automation_store_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  target_compile_definitions(firmware_automation_store_tests PRIVATE
    PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS=1
  )
  if(MSVC)
    target_compile_options(firmware_automation_store_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_automation_store_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_automation_store_unit COMMAND firmware_automation_store_tests)
endif()
