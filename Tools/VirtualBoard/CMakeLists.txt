cmake_minimum_required(VERSION 3.20)

project(PCControllerVirtualBoard VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/bin")

find_package(Threads REQUIRED)

add_library(virtual_board_core
  src/hardware.cpp
  src/protocol.cpp
  src/virtual_board.cpp
)
target_include_directories(virtual_board_core PUBLIC include)
target_link_libraries(virtual_board_core PUBLIC Threads::Threads)

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
    ../../Project/Core/MacroRing.cpp
    ../../Project/MacroQueue.cpp
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

  add_executable(virtual_board_tests tests/test_main.cpp)
  target_link_libraries(virtual_board_tests PRIVATE virtual_board_core)
  if(MSVC)
    target_compile_options(virtual_board_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(virtual_board_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME virtual_board_unit COMMAND virtual_board_tests)

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
  )
  target_include_directories(firmware_controls_tests PRIVATE
    tests/arduino_mock
    ../..
  )
  if(MSVC)
    target_compile_options(firmware_controls_tests PRIVATE /W4 /permissive-)
  else()
    target_compile_options(firmware_controls_tests PRIVATE ${VIRTUAL_BOARD_WARNINGS})
  endif()
  add_test(NAME firmware_controls_unit COMMAND firmware_controls_tests)

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
endif()
