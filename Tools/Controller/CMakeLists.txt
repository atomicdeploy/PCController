cmake_minimum_required(VERSION 3.20)

# This developer-only project is a consumer-facing bridge to the one Go C ABI
# implementation. It does not reimplement package, compiler, manifest, or
# deployment policy from Tools/Build; it only makes a local c-shared build and
# C consumer verification available through CMake.
set(PCCONTROLLER_CABI_CC "" CACHE FILEPATH
  "Native C compiler used by both CMake and Go CGO (defaults to CMAKE_C_COMPILER)")
if(PCCONTROLLER_CABI_CC AND NOT DEFINED CMAKE_C_COMPILER)
  set(CMAKE_C_COMPILER "${PCCONTROLLER_CABI_CC}" CACHE FILEPATH
    "C compiler selected for the PCController C ABI" FORCE)
endif()

# Deliberately omit VERSION: this verification project is not a host release
# package and must not imply a product version separate from the canonical
# packager's frozen identity.
project(PCControllerCABI LANGUAGES C)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_C_EXTENSIONS OFF)

find_program(PCCONTROLLER_CABI_GO_EXECUTABLE NAMES go REQUIRED)
include(GNUInstallDirs)

if(NOT PCCONTROLLER_CABI_CC)
  set(PCCONTROLLER_CABI_CC "${CMAKE_C_COMPILER}" CACHE FILEPATH
    "Native C compiler used by both CMake and Go CGO" FORCE)
endif()

get_filename_component(_pccontroller_cabi_cc_real "${PCCONTROLLER_CABI_CC}" REALPATH)
get_filename_component(_pccontroller_cmake_cc_real "${CMAKE_C_COMPILER}" REALPATH)
if(NOT _pccontroller_cabi_cc_real STREQUAL _pccontroller_cmake_cc_real)
  message(FATAL_ERROR
    "PCCONTROLLER_CABI_CC and CMAKE_C_COMPILER must name the same compiler. "
    "Configure with -DPCCONTROLLER_CABI_CC=<native compiler> on a fresh build directory.")
endif()

if(WIN32)
  # Go's Windows c-shared target needs native MinGW-w64/Windows-GNU. A Git
  # MSYS/Cygwin gcc can compile simple C but produces an incompatible CGO
  # runtime, so reject it at configure time rather than leaving a late linker
  # failure for the developer.
  execute_process(
    COMMAND "${PCCONTROLLER_CABI_CC}" -dumpmachine
    RESULT_VARIABLE _pccontroller_cabi_target_result
    OUTPUT_VARIABLE PCCONTROLLER_CABI_COMPILER_TARGET
    ERROR_VARIABLE _pccontroller_cabi_target_error
    OUTPUT_STRIP_TRAILING_WHITESPACE
  )
  file(WRITE "${CMAKE_BINARY_DIR}/pccontroller-cabi-compiler-probe.c" "")
  execute_process(
    COMMAND "${PCCONTROLLER_CABI_CC}" -dM -E -x c -
    INPUT_FILE "${CMAKE_BINARY_DIR}/pccontroller-cabi-compiler-probe.c"
    RESULT_VARIABLE _pccontroller_cabi_macro_result
    OUTPUT_VARIABLE _pccontroller_cabi_macros
    ERROR_VARIABLE _pccontroller_cabi_macro_error
  )
  execute_process(
    COMMAND "${PCCONTROLLER_CABI_GO_EXECUTABLE}" env GOARCH
    RESULT_VARIABLE _pccontroller_cabi_goarch_result
    OUTPUT_VARIABLE _pccontroller_cabi_goarch
    OUTPUT_STRIP_TRAILING_WHITESPACE
  )
  if(NOT _pccontroller_cabi_target_result EQUAL 0 OR NOT _pccontroller_cabi_macro_result EQUAL 0
      OR NOT _pccontroller_cabi_goarch_result EQUAL 0)
    message(FATAL_ERROR
      "Could not probe PCCONTROLLER_CABI_CC=${PCCONTROLLER_CABI_CC}. "
      "Use a native MinGW-w64 compiler compatible with go env GOARCH.\n"
      "-dumpmachine: ${_pccontroller_cabi_target_error}\n"
      "macros: ${_pccontroller_cabi_macro_error}")
  endif()
  if(_pccontroller_cabi_goarch STREQUAL "amd64")
    set(_pccontroller_cabi_target_pattern "^x86_64-.*(mingw|windows-gnu)")
  elseif(_pccontroller_cabi_goarch STREQUAL "386")
    set(_pccontroller_cabi_target_pattern "^i[3-6]86-.*(mingw|windows-gnu)")
  elseif(_pccontroller_cabi_goarch STREQUAL "arm64")
    set(_pccontroller_cabi_target_pattern "^(aarch64|arm64)-.*(mingw|windows-gnu)")
  else()
    set(_pccontroller_cabi_target_pattern "(mingw|windows-gnu)")
  endif()
  string(TOLOWER "${PCCONTROLLER_CABI_COMPILER_TARGET}" _pccontroller_cabi_target_lower)
  string(FIND "${_pccontroller_cabi_macros}" "__MSYS__" _pccontroller_cabi_msys)
  string(FIND "${_pccontroller_cabi_macros}" "__CYGWIN__" _pccontroller_cabi_cygwin)
  string(FIND "${_pccontroller_cabi_macros}" "__MINGW32__" _pccontroller_cabi_mingw32)
  string(FIND "${_pccontroller_cabi_macros}" "__MINGW64__" _pccontroller_cabi_mingw64)
  if(NOT _pccontroller_cabi_target_lower MATCHES "${_pccontroller_cabi_target_pattern}"
      OR NOT _pccontroller_cabi_msys EQUAL -1 OR NOT _pccontroller_cabi_cygwin EQUAL -1
      OR (_pccontroller_cabi_mingw32 EQUAL -1 AND _pccontroller_cabi_mingw64 EQUAL -1))
    message(FATAL_ERROR
      "PCCONTROLLER_CABI_CC=${PCCONTROLLER_CABI_CC} is not a native Windows "
      "GNU compiler for GOARCH=${_pccontroller_cabi_goarch}. Target was "
      "${PCCONTROLLER_CABI_COMPILER_TARGET}. Configure with a UCRT/MinGW-w64 "
      "compiler; MSYS and Cygwin targets are unsupported.")
  endif()
endif()

execute_process(
  COMMAND "${PCCONTROLLER_CABI_CC}" --version
  OUTPUT_VARIABLE _pccontroller_cabi_compiler_version
  ERROR_QUIET
  OUTPUT_STRIP_TRAILING_WHITESPACE
)
string(REPLACE "\n" " " _pccontroller_cabi_compiler_version "${_pccontroller_cabi_compiler_version}")
message(STATUS "PCController C ABI compiler: ${PCCONTROLLER_CABI_CC} (${_pccontroller_cabi_compiler_version})")
if(WIN32)
  message(STATUS "PCController C ABI compiler target: ${PCCONTROLLER_CABI_COMPILER_TARGET}")
endif()

set(PCCONTROLLER_CABI_ARTIFACT_DIR "${CMAKE_BINARY_DIR}/artifacts" CACHE PATH
  "Output directory for the generated PCController C ABI library and header")
set(PCCONTROLLER_CABI_ENABLE_SMOKE ON CACHE BOOL "Build and register the external C ABI smoke consumer")
set(PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX "${CMAKE_BINARY_DIR}/install-smoke" CACHE PATH
  "Private install prefix used only by the CTest installed-consumer smoke")

if(WIN32)
  set(_pccontroller_cabi_library "${PCCONTROLLER_CABI_ARTIFACT_DIR}/pccontroller.dll")
elseif(APPLE)
  set(_pccontroller_cabi_library "${PCCONTROLLER_CABI_ARTIFACT_DIR}/pccontroller.dylib")
else()
  set(_pccontroller_cabi_library "${PCCONTROLLER_CABI_ARTIFACT_DIR}/pccontroller.so")
endif()
set(_pccontroller_cabi_header "${PCCONTROLLER_CABI_ARTIFACT_DIR}/pccontroller.h")
set(_pccontroller_cabi_installed_header
  "${PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX}/${CMAKE_INSTALL_INCLUDEDIR}/pccontroller.h")

file(GLOB_RECURSE _pccontroller_cabi_go_sources CONFIGURE_DEPENDS
  "${CMAKE_CURRENT_SOURCE_DIR}/*.go")
list(FILTER _pccontroller_cabi_go_sources EXCLUDE REGEX "/(node_modules|\\.cache)/")

add_custom_target(pccontroller_cabi
  COMMAND "${CMAKE_COMMAND}" -E make_directory "${PCCONTROLLER_CABI_ARTIFACT_DIR}"
  COMMAND "${CMAKE_COMMAND}" -E env
    "CGO_ENABLED=1"
    "CC=${PCCONTROLLER_CABI_CC}"
    "${PCCONTROLLER_CABI_GO_EXECUTABLE}" build
    -buildvcs=false -trimpath -tags controllerlib -buildmode=c-shared
    -o "${_pccontroller_cabi_library}" ./cmd/controller-cabi
  BYPRODUCTS "${_pccontroller_cabi_library}" "${_pccontroller_cabi_header}"
  DEPENDS ${_pccontroller_cabi_go_sources}
    "${CMAKE_CURRENT_SOURCE_DIR}/go.mod"
    "${CMAKE_CURRENT_SOURCE_DIR}/go.sum"
  WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
  VERBATIM
  COMMENT "Building the PCController Go C ABI"
)

if(PCCONTROLLER_CABI_ENABLE_SMOKE)
  add_executable(pccontroller_cabi_smoke examples/c_abi_smoke.c)
  add_dependencies(pccontroller_cabi_smoke pccontroller_cabi)
  target_include_directories(pccontroller_cabi_smoke PRIVATE "${PCCONTROLLER_CABI_ARTIFACT_DIR}")
  set_target_properties(pccontroller_cabi_smoke PROPERTIES
    RUNTIME_OUTPUT_DIRECTORY "${PCCONTROLLER_CABI_ARTIFACT_DIR}")
  if(MSVC)
    target_compile_options(pccontroller_cabi_smoke PRIVATE /W4 /permissive-)
  else()
    target_compile_options(pccontroller_cabi_smoke PRIVATE -Wall -Wextra -Wpedantic -Wconversion -Wshadow)
  endif()
  target_link_libraries(pccontroller_cabi_smoke PRIVATE ${CMAKE_DL_LIBS})

  # Stage a private prefix through CMake's own install machinery, after the
  # Go target has generated its DLL/shared-object and header. A second C
  # consumer then compiles against that installed header and dynamically loads
  # the installed library, proving the documented install boundary.
  if(WIN32)
    set(_pccontroller_cabi_installed_library
      "${PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX}/${CMAKE_INSTALL_BINDIR}/pccontroller.dll")
  elseif(APPLE)
    set(_pccontroller_cabi_installed_library
      "${PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX}/${CMAKE_INSTALL_LIBDIR}/pccontroller.dylib")
  else()
    set(_pccontroller_cabi_installed_library
      "${PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX}/${CMAKE_INSTALL_LIBDIR}/pccontroller.so")
  endif()
  add_custom_command(
    OUTPUT "${_pccontroller_cabi_installed_library}" "${_pccontroller_cabi_installed_header}"
    COMMAND "${CMAKE_COMMAND}" --install "${CMAKE_BINARY_DIR}"
      --prefix "${PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX}"
    DEPENDS pccontroller_cabi
    VERBATIM
    COMMENT "Staging the PCController C ABI install tree for its external C consumer"
  )
  add_custom_target(pccontroller_cabi_install
    DEPENDS "${_pccontroller_cabi_installed_library}" "${_pccontroller_cabi_installed_header}")

  add_executable(pccontroller_cabi_installed_smoke examples/c_abi_smoke.c)
  # Building the installed-consumer target also builds the direct consumer so
  # its paired CTest entry can never be registered without an executable.
  add_dependencies(pccontroller_cabi_installed_smoke
    pccontroller_cabi_install
    pccontroller_cabi_smoke)
  target_include_directories(pccontroller_cabi_installed_smoke PRIVATE
    "${PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX}/${CMAKE_INSTALL_INCLUDEDIR}")
  set_target_properties(pccontroller_cabi_installed_smoke PROPERTIES
    RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/installed-smoke/bin")
  if(MSVC)
    target_compile_options(pccontroller_cabi_installed_smoke PRIVATE /W4 /permissive-)
  else()
    target_compile_options(pccontroller_cabi_installed_smoke PRIVATE
      -Wall -Wextra -Wpedantic -Wconversion -Wshadow)
  endif()
  target_link_libraries(pccontroller_cabi_installed_smoke PRIVATE ${CMAKE_DL_LIBS})

  include(CTest)
  if(BUILD_TESTING)
    add_test(NAME pccontroller_cabi_smoke COMMAND pccontroller_cabi_smoke)
    set_tests_properties(pccontroller_cabi_smoke PROPERTIES
      WORKING_DIRECTORY "${PCCONTROLLER_CABI_ARTIFACT_DIR}")
    add_test(NAME pccontroller_cabi_installed_smoke COMMAND pccontroller_cabi_installed_smoke)
    if(WIN32)
      set(_pccontroller_cabi_installed_working_directory
        "${PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX}/${CMAKE_INSTALL_BINDIR}")
    else()
      set(_pccontroller_cabi_installed_working_directory
        "${PCCONTROLLER_CABI_SMOKE_INSTALL_PREFIX}/${CMAKE_INSTALL_LIBDIR}")
    endif()
    set_tests_properties(pccontroller_cabi_installed_smoke PROPERTIES
      WORKING_DIRECTORY "${_pccontroller_cabi_installed_working_directory}")
  endif()
endif()

if(WIN32)
  install(FILES "${_pccontroller_cabi_library}"
    DESTINATION "${CMAKE_INSTALL_BINDIR}")
else()
  install(FILES "${_pccontroller_cabi_library}"
    DESTINATION "${CMAKE_INSTALL_LIBDIR}")
endif()
install(FILES "${_pccontroller_cabi_header}"
  DESTINATION "${CMAKE_INSTALL_INCLUDEDIR}")
