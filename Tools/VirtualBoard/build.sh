#!/usr/bin/env bash
set -euo pipefail

source_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
configuration=Release
clean=0
skip_tests=0

while (($#)); do
    case "$1" in
        --configuration)
            (($# >= 2)) || { printf 'Missing value for --configuration\n' >&2; exit 2; }
            configuration=$2
            shift 2
            ;;
        --clean)
            clean=1
            shift
            ;;
        --skip-tests)
            skip_tests=1
            shift
            ;;
        --help|-h)
            cat <<'EOF'
Usage: ./build.sh [--configuration Debug|Release|RelWithDebInfo] [--clean] [--skip-tests]

Uses CMake, Ninja, and the native GCC C++ compiler discovered from CXX/PATH.
EOF
            exit 0
            ;;
        *)
            printf 'Unknown option: %s\n' "$1" >&2
            exit 2
            ;;
    esac
done

case "$configuration" in
    Debug|Release|RelWithDebInfo) ;;
    *) printf 'Unsupported configuration: %s\n' "$configuration" >&2; exit 2 ;;
esac

case "$configuration" in
    Debug) preset=debug ;;
    Release) preset=release ;;
    RelWithDebInfo) preset=relwithdebinfo ;;
esac
build_root="${source_root}/.build/${preset}"

# Preset discovery is rooted in the current directory on some CMake builds.
# Enter the script directory so this command behaves identically from any caller.
cd -- "$source_root"

command -v cmake >/dev/null || { printf 'cmake is required in PATH\n' >&2; exit 1; }
command -v ninja >/dev/null || { printf 'ninja is required in PATH\n' >&2; exit 1; }
cxx=${CXX:-g++}
command -v "$cxx" >/dev/null || { printf '%s is required in PATH\n' "$cxx" >&2; exit 1; }
triplet=$("$cxx" -dumpmachine)
compiler_display=$(command -v "$cxx")
compiler_path=$compiler_display
cmake_compiler_args=()
if [[ -n "${MSYSTEM:-}" ]]; then
    # The preset already selects the GNU compiler from the native Windows PATH.
    # Do not pass Git Bash's virtual path through to native CMake.
    :
else
    if [[ "$triplet" == *msys* ]] && command -v cygpath >/dev/null; then
        [[ -x "${compiler_path}.exe" ]] && compiler_path="${compiler_path}.exe"
        compiler_path=$(cygpath -m "$compiler_path")
    fi
    cmake_compiler_args+=("-DCMAKE_CXX_COMPILER=${compiler_path}")
fi

cyan=$'\033[36m'
green=$'\033[32m'
magenta=$'\033[1;95m'
yellow=$'\033[33m'
dim=$'\033[90m'
reset=$'\033[0m'

printf '%s╔══════════════════════════════════════╗%s\n' "$magenta" "$reset"
printf '%s║  🧪 PCController Virtual Board       ║%s\n' "$magenta" "$reset"
printf '%s╚══════════════════════════════════════╝%s\n' "$magenta" "$reset"
printf '%sCompiler: %s (%s)%s\n' "$dim" "$compiler_display" "$triplet" "$reset"

if ((clean)) && [[ -d "$build_root" ]]; then
    printf '%s🧹 Removing %s%s\n' "$yellow" "$build_root" "$reset"
    rm -rf -- "$build_root"
fi

printf '%s⚙️  Configuring CMake%s\n' "$cyan" "$reset"
cmake --preset "$preset" -S "$source_root" "${cmake_compiler_args[@]}"

printf '%s🔨 Building C++17 virtual hardware%s\n' "$cyan" "$reset"
cmake --build --preset "$preset" --parallel

if ((skip_tests == 0)); then
    printf '%s🧪 Running protocol and EEPROM tests%s\n' "$cyan" "$reset"
    # The test preset already points at the matching generated build tree.
    # Overriding it with the source directory makes CTest discover zero tests.
    ctest --preset "$preset" --no-tests=error
fi

printf '%s✅ Virtual board build passed.%s\n' "$green" "$reset"
for artifact in "$build_root"/bin/*; do
    [[ -f "$artifact" ]] && printf '%s\n' "$artifact"
done
