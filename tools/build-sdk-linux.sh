#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
project_root="$(cd -- "$script_dir/.." && pwd -P)"
sdk_root="$project_root/dist-sdk"
build_info="$project_root/internal/buildinfo/buildinfo.go"
resource_template="$project_root/resources/RayanLamp.dll.rc.in"
native_root="$project_root/sdk/native"

architecture="amd64"
build_number="0"
cc_path=""
skip_tests=0
force=0

usage() {
    cat <<'EOF'
Cross-build the optional attach-only RayanLamp Windows SDK DLL.

Usage:
  tools/build-sdk-linux.sh [options]

Options:
  --architecture amd64|386|all  Target architecture (default: amd64)
  --build-number NUMBER         Fourth Windows file-version component
  --cc PATH                     Explicit matching MinGW-w64 gcc (single target)
  --skip-tests                  Skip focused host-side Go tests
  --force                       Replace the selected dist-sdk architecture
  --help                        Show this help

ARM64 is intentionally unsupported until a reproducible LLVM-MinGW toolchain
is pinned. The ordinary CGO-free ARM64 RayanLamp.exe build is unaffected.
EOF
}

die() {
    printf 'build-sdk-linux: %s\n' "$*" >&2
    exit 1
}

while (($#)); do
    case "$1" in
        --architecture)
            (($# >= 2)) || die "--architecture requires a value"
            architecture="$2"
            shift 2
            ;;
        --architecture=*)
            architecture="${1#*=}"
            shift
            ;;
        --build-number)
            (($# >= 2)) || die "--build-number requires a value"
            build_number="$2"
            shift 2
            ;;
        --build-number=*)
            build_number="${1#*=}"
            shift
            ;;
        --cc)
            (($# >= 2)) || die "--cc requires a value"
            cc_path="$2"
            shift 2
            ;;
        --cc=*)
            cc_path="${1#*=}"
            shift
            ;;
        --skip-tests)
            skip_tests=1
            shift
            ;;
        --force)
            force=1
            shift
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            die "unknown option: $1"
            ;;
    esac
done

case "$architecture" in
    amd64|386|all) ;;
    arm64)
        die "ARM64 SDK requires pinned LLVM-MinGW and is not yet supported; use amd64 or 386"
        ;;
    *)
        die "unsupported SDK architecture: $architecture"
        ;;
esac
[[ "$build_number" =~ ^[0-9]+$ ]] ||
    die "build number must be an integer"
((build_number >= 0 && build_number <= 65535)) ||
    die "build number must be between 0 and 65535"
if [[ "$architecture" == "all" && -n "$cc_path" ]]; then
    die "--cc may select only one architecture; invoke once per target"
fi

for required in "$build_info" "$resource_template" \
    "$native_root/RayanLamp.h" "$native_root/RayanLamp.c"; do
    [[ -f "$required" ]] || die "missing SDK source: $required"
done

product_version="$(
    sed -nE 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"([0-9]+\.[0-9]+\.[0-9]+)".*/\1/p' \
        "$build_info" | head -n 1
)"
[[ -n "$product_version" ]] ||
    die "could not read the product version from $build_info"
file_version="$product_version.$build_number"

if ((skip_tests == 0)); then
    (
        cd -- "$project_root"
        env CGO_ENABLED=0 go test -count=1 \
            ./sdk ./internal/dllapi ./tools/verify-dll
    )
fi

resolve_toolchain() {
    local target="$1"
    local requested="$2"
    local prefix compiler windres machine package_arch
    if [[ "$target" == "amd64" ]]; then
        prefix="x86_64-w64-mingw32"
        package_arch="x86-64"
    else
        prefix="i686-w64-mingw32"
        package_arch="i686"
    fi
    if [[ -n "$requested" ]]; then
        compiler="$requested"
    else
        compiler="$(command -v "$prefix-gcc" || true)"
    fi
    [[ -n "$compiler" && -x "$compiler" ]] || die \
        "missing $prefix-gcc. Install MinGW-w64 (Debian/Ubuntu: gcc-mingw-w64-$package_arch binutils-mingw-w64-$package_arch) or pass --cc"
    compiler="$(cd -- "$(dirname -- "$compiler")" && pwd -P)/$(basename -- "$compiler")"
    machine="$("$compiler" -dumpmachine | head -n 1)"
    [[ "$machine" == "$prefix" ]] ||
        die "$compiler targets $machine, expected $prefix"

    windres="$(dirname -- "$compiler")/$prefix-windres"
    if [[ ! -x "$windres" ]]; then
        windres="$(command -v "$prefix-windres" || true)"
    fi
    [[ -n "$windres" && -x "$windres" ]] ||
        die "missing $prefix-windres; install the matching binutils-mingw-w64 package"
    printf '%s\n%s\n%s\n' "$prefix" "$compiler" "$windres"
}

build_target() {
    local target="$1"
    local requested="$2"
    local output_dir="$sdk_root/$target"
    local temp_root publish archive generated_header rc_file resource_object
    local dll import_library definition smoke prefix compiler windres cxx
    local toolchain_output
    local -a toolchain

    # Capture the resolver in a normal command substitution first. A failed
    # process substitution would otherwise leave mapfile successful and lose
    # the toolchain diagnostic under `set -e`.
    toolchain_output="$(resolve_toolchain "$target" "$requested")"
    mapfile -t toolchain <<< "$toolchain_output"
    ((${#toolchain[@]} == 3)) ||
        die "toolchain resolver returned an incomplete result for $target"
    prefix="${toolchain[0]}"
    compiler="${toolchain[1]}"
    windres="${toolchain[2]}"
    cxx="$(dirname -- "$compiler")/$prefix-g++"
    [[ -x "$cxx" ]] || cxx="$prefix-g++"

    if [[ -e "$output_dir" && "$force" -ne 1 ]]; then
        die "SDK output already exists: $output_dir (pass --force to replace it)"
    fi
    mkdir -p -- "$sdk_root"
    temp_root="$(mktemp -d "${TMPDIR:-/tmp}/RayanLamp-sdk.XXXXXXXX")"
    trap 'rm -rf -- "$temp_root"' RETURN
    publish="$temp_root/publish"
    archive="$temp_root/RayanLampGo.a"
    generated_header="$temp_root/RayanLampGo.h"
    rc_file="$temp_root/RayanLamp.dll.rc"
    resource_object="$temp_root/RayanLamp.dll.o"
    dll="$publish/RayanLamp.dll"
    import_library="$publish/libRayanLamp.dll.a"
    definition="$native_root/RayanLamp_${target}.def"
    smoke="$temp_root/RayanLamp-dll-smoke.exe"
    mkdir -p -- "$publish"

    sed \
        -e "s/@FILE_VERSION_COMMAS@/${file_version//./,}/g" \
        -e "s/@FILE_VERSION@/$file_version/g" \
        -e "s/@PRODUCT_VERSION@/$product_version/g" \
        "$resource_template" > "$rc_file"

    printf 'Building attach-only RayanLamp SDK for Windows/%s\n' "$target"
    (
        cd -- "$project_root"
        env GOOS=windows GOARCH="$target" CGO_ENABLED=1 \
            CC="$compiler" CXX="$cxx" \
            go build -trimpath -buildvcs=false -buildmode=c-archive \
                -tags rayanlamp_sdk,rayanlamp_production -ldflags '-s -w' \
                -o "$archive" ./cmd/RayanLampDLL
    )
    [[ -f "$generated_header" ]] ||
        die "Go did not create $generated_header"
    "$windres" --input "$rc_file" --output "$resource_object" \
        --output-format=coff
    "$compiler" \
        -std=c11 -Os -Wall -Wextra -Werror \
        -shared -static-libgcc \
        -Wl,--gc-sections -Wl,--exclude-all-symbols \
        "-Wl,--out-implib,$import_library" \
        -I "$native_root" -I "$temp_root" \
        -o "$dll" \
        "$native_root/RayanLamp.c" \
        "$resource_object" "$archive" "$definition" \
        -lwinmm -lws2_32 -lntdll
    cp -- "$native_root/RayanLamp.h" "$publish/RayanLamp.h"

    (
        cd -- "$project_root"
        env CGO_ENABLED=0 go run ./tools/verify-dll \
            --file "$dll" --arch "$target" --file-version "$file_version"
    )
    # Linux cannot execute the PE harness, but compiling it with the same
    # target compiler catches header and WINAPI signature drift.
    "$compiler" \
        -std=c11 -Os -Wall -Wextra -Werror -Wno-cast-function-type \
        -municode -I "$native_root" \
        -o "$smoke" "$project_root/tools/dll-smoke/main.c"

    if [[ -e "$output_dir" ]]; then
        case "$output_dir" in
            "$sdk_root"/amd64|"$sdk_root"/386)
                rm -rf -- "$output_dir"
                ;;
            *)
                die "refusing unsafe SDK replacement path: $output_dir"
                ;;
        esac
    fi
    mv -- "$publish" "$output_dir"
    trap - RETURN
    rm -rf -- "$temp_root"
    printf 'SDK artifact: %s\n' "$output_dir"
    printf 'SHA-256: '
    sha256sum -- "$output_dir/RayanLamp.dll" | awk '{print $1}'
}

if [[ "$architecture" == "all" ]]; then
    build_target amd64 ""
    build_target 386 ""
else
    build_target "$architecture" "$cc_path"
fi
