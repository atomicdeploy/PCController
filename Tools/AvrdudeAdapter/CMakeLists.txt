cmake_minimum_required(VERSION 3.20)

project(PCControllerAvrdudeAdapter VERSION 1.0.0 LANGUAGES C)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_C_EXTENSIONS OFF)

include(FetchContent)
include(GNUInstallDirs)

# libavrdude does not promise a stable ABI. Fetching the reviewed source and
# compiling the adapter and library together is therefore part of the safety
# boundary, not merely build convenience.
file(READ "${CMAKE_CURRENT_SOURCE_DIR}/dependency-lock.json"
  _pccontroller_avrdude_lock)
string(JSON PCCONTROLLER_AVRDUDE_VERSION GET "${_pccontroller_avrdude_lock}" version)
string(JSON PCCONTROLLER_AVRDUDE_SOURCE_URL GET "${_pccontroller_avrdude_lock}" source_url)
string(JSON PCCONTROLLER_AVRDUDE_SOURCE_SHA256 GET "${_pccontroller_avrdude_lock}" source_sha256)

set(BUILD_DOC OFF CACHE BOOL "" FORCE)
set(BUILD_SHARED_LIBS OFF CACHE BOOL "" FORCE)
set(FORCE_DISABLE_PYTHON_SUPPORT ON CACHE BOOL "" FORCE)
if(WIN32)
  # Upstream's reviewed dependency set enables USBasp and other USB-backed
  # programmers without relying on an arbitrary DLL already on the host.
  set(USE_EXTERNAL_LIBS ON CACHE BOOL "" FORCE)
endif()

FetchContent_Declare(pccontroller_avrdude
  URL "${PCCONTROLLER_AVRDUDE_SOURCE_URL}"
  URL_HASH "SHA256=${PCCONTROLLER_AVRDUDE_SOURCE_SHA256}"
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE
  EXCLUDE_FROM_ALL)
FetchContent_MakeAvailable(pccontroller_avrdude)

# AVRDUDE 8.3's pinned libusb-win32 dependency predates GCC 16's rejection of
# an empty trailing variadic macro argument. Apply the narrow upstream-source
# compatibility spelling only to that fetched dependency and only for MinGW.
# Fail closed if the dependency no longer has the reviewed line.
if(MINGW AND DEFINED libusb_SOURCE_DIR)
  set(_pccontroller_libusb_error_header "${libusb_SOURCE_DIR}/src/error.h")
  file(READ "${_pccontroller_libusb_error_header}" _pccontroller_libusb_error_text)
  set(_pccontroller_libusb_old
    "#define USBERR(format,...) usb_err(__FUNCTION__,format,__VA_ARGS__)")
  set(_pccontroller_libusb_new
    "#define USBERR(format,...) usb_err(__FUNCTION__,format, ##__VA_ARGS__)")
  string(FIND "${_pccontroller_libusb_error_text}" "${_pccontroller_libusb_old}"
    _pccontroller_libusb_patch_offset)
  if(_pccontroller_libusb_patch_offset EQUAL -1)
    string(FIND "${_pccontroller_libusb_error_text}" "${_pccontroller_libusb_new}"
      _pccontroller_libusb_patched_offset)
    if(_pccontroller_libusb_patched_offset EQUAL -1)
      message(FATAL_ERROR "Pinned libusb compatibility line changed; review before updating")
    endif()
  else()
    string(REPLACE "${_pccontroller_libusb_old}" "${_pccontroller_libusb_new}"
      _pccontroller_libusb_error_text "${_pccontroller_libusb_error_text}")
    file(WRITE "${_pccontroller_libusb_error_header}" "${_pccontroller_libusb_error_text}")
  endif()
endif()

if(WIN32)
  # libavrdude's public header uses WIN32 (rather than the compiler-provided
  # _WIN32) to select its Windows serial declarations.
  target_compile_definitions(libavrdude PUBLIC WIN32)
endif()

add_executable(pccontroller-avrdude adapter.c)
add_dependencies(pccontroller-avrdude avrdude conf)
target_link_libraries(pccontroller-avrdude PRIVATE libavrdude)
if(MINGW)
  # The pinned Windows USB/FTDI compatibility libraries contain C++ objects
  # although libavrdude and the adapter expose C APIs. Repeat those static
  # targets here so their C++/WinUSB dependencies occur after the archives in
  # the one-pass MinGW link order.
  target_link_libraries(pccontroller-avrdude PRIVATE libusb libftdi stdc++ winusb)
  # Apply the same static archive ordering to the matching upstream CLI. It is
  # packaged only for extension queries that libavrdude does not expose.
  target_link_libraries(avrdude PRIVATE libusb libftdi stdc++ winusb)
endif()
target_compile_definitions(pccontroller-avrdude PRIVATE
  PCCONTROLLER_AVRDUDE_VERSION="${PCCONTROLLER_AVRDUDE_VERSION}")
if(MSVC)
  target_compile_options(pccontroller-avrdude PRIVATE /W4 /permissive-)
else()
  target_compile_options(pccontroller-avrdude PRIVATE
    -Wall -Wextra -Wpedantic -Wconversion -Wshadow)
endif()

add_custom_command(TARGET pccontroller-avrdude POST_BUILD
  COMMAND "${CMAKE_COMMAND}" -E copy_if_different
    "$<TARGET_FILE:avrdude>"
    "$<TARGET_FILE_DIR:pccontroller-avrdude>/$<TARGET_FILE_NAME:avrdude>"
  COMMAND "${CMAKE_COMMAND}" -E copy_if_different
    "${pccontroller_avrdude_BINARY_DIR}/src/avrdude.conf"
    "$<TARGET_FILE_DIR:pccontroller-avrdude>/avrdude.conf"
  COMMENT "Staging the configuration compiled with libavrdude")

install(TARGETS pccontroller-avrdude avrdude RUNTIME DESTINATION "${CMAKE_INSTALL_BINDIR}")
install(FILES "${pccontroller_avrdude_BINARY_DIR}/src/avrdude.conf"
  DESTINATION "${CMAKE_INSTALL_SYSCONFDIR}")
