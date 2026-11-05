#!/usr/bin/env bash

set -Eeuo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
source "$project_root/tools/terminal.sh"
cd -- "$project_root"
dist_root="$project_root/dist"
build_info_path="$project_root/internal/buildinfo/buildinfo.go"
web_root="$project_root/webui"
web_dist_root="$project_root/internal/webui/dist"
resource_tool="github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0"

architecture="amd64"
edition=""
version=""
build_number="0"
commit=""
build_date=""
upx_path=""
compress=false
skip_tests=false
skip_vet=false
skip_web_build=false
force=false

info() {
    rl_info "$*"
}

step() {
    rl_step "$*"
}

success() {
    rl_success "$*"
}

warn() {
    rl_warn "$*"
}

die() {
    rl_error "$*"
    exit 1
}

usage() {
    cat <<'EOF'
Cross-build the single-file Windows RayanLamp executable from Linux.

Usage:
  tools/build-linux.sh [options]

Options:
  --architecture amd64|386|arm64  Windows architecture (default: amd64)
  --edition develop|production    Override build-edition.txt
  --version VERSION               Must match internal/buildinfo.Version
  --build-number NUMBER           Fourth Windows file-version component
  --commit COMMIT                 Build metadata (default: current Git commit)
  --build-date ISO-8601           Build metadata (default: current UTC time)
  --compress                      Compress amd64/386 with UPX when available
  --upx PATH                      Use this UPX executable and enable compression
  --skip-tests                    Skip frontend/Go tests and test compilation
  --skip-vet                      Skip Windows-targeted go vet
  --skip-web-build                Reuse the checked-in web distribution
  --force                         Replace an existing dist/RayanLamp.exe
  -h, --help                      Show this help

Color follows terminal capability. Set NO_COLOR to disable ANSI styling or
FORCE_COLOR=1 to retain VT-100 styling in a capable CI log. Set NO_EMOJI=1
for ASCII labels, or RAYANLAMP_EMOJI=always to force emoji in a UTF-8 CI log.
EOF
}

require_command() {
    command -v "$1" >/dev/null 2>&1 ||
        die "$1 is required and must be available on PATH."
}

require_value() {
    local option="$1"
    local value="${2:-}"
    [[ -n "$value" ]] || die "$option requires a value."
}

while (($# > 0)); do
    case "$1" in
        --architecture)
            require_value "$1" "${2:-}"
            architecture="$2"
            shift 2
            ;;
        --architecture=*)
            architecture="${1#*=}"
            shift
            ;;
        --edition)
            require_value "$1" "${2:-}"
            edition="$2"
            shift 2
            ;;
        --edition=*)
            edition="${1#*=}"
            shift
            ;;
        --version)
            require_value "$1" "${2:-}"
            version="$2"
            shift 2
            ;;
        --version=*)
            version="${1#*=}"
            shift
            ;;
        --build-number)
            require_value "$1" "${2:-}"
            build_number="$2"
            shift 2
            ;;
        --build-number=*)
            build_number="${1#*=}"
            shift
            ;;
        --commit)
            require_value "$1" "${2:-}"
            commit="$2"
            shift 2
            ;;
        --commit=*)
            commit="${1#*=}"
            shift
            ;;
        --build-date)
            require_value "$1" "${2:-}"
            build_date="$2"
            shift 2
            ;;
        --build-date=*)
            build_date="${1#*=}"
            shift
            ;;
        --upx)
            require_value "$1" "${2:-}"
            upx_path="$2"
            compress=true
            shift 2
            ;;
        --upx=*)
            upx_path="${1#*=}"
            compress=true
            shift
            ;;
        --compress)
            compress=true
            shift
            ;;
        --skip-tests)
            skip_tests=true
            shift
            ;;
        --skip-vet)
            skip_vet=true
            shift
            ;;
        --skip-web-build)
            skip_web_build=true
            shift
            ;;
        --force)
            force=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            die "Unknown option: $1"
            ;;
    esac
done

case "$architecture" in
    amd64|386|arm64) ;;
    *) die "Unsupported Windows architecture: $architecture" ;;
esac
if [[ -z "$edition" ]]; then
    [[ -f "$project_root/build-edition.txt" ]] ||
        die "canonical build edition is missing: $project_root/build-edition.txt"
    edition="$(<"$project_root/build-edition.txt")"
    edition="${edition#"${edition%%[![:space:]]*}"}"
    edition="${edition%"${edition##*[![:space:]]}"}"
fi
case "$edition" in
    develop)
        build_tag="rayanlamp_develop"
        developer_tools=true
        ;;
    production)
        build_tag="rayanlamp_production"
        developer_tools=false
        ;;
    *) die "Unsupported build edition: $edition" ;;
esac

[[ "$build_number" =~ ^[0-9]+$ ]] ||
    die "Build number must be an integer between 0 and 65535."
((10#$build_number <= 65535)) ||
    die "Build number must be an integer between 0 and 65535."
build_number="$((10#$build_number))"

for command_name in go awk sed grep date mktemp flock sha256sum; do
    require_command "$command_name"
done

source_version="$(
    sed -n \
        's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' \
        "$build_info_path"
)"
[[ -n "$source_version" && "$source_version" != *$'\n'* ]] ||
    die "Could not read one canonical version from $build_info_path."
if [[ -z "$version" ]]; then
    version="$source_version"
fi
[[ "$version" == "$source_version" ]] ||
    die "Requested version $version does not match buildinfo.Version $source_version."

semver_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?(\+([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$'
[[ "$version" =~ $semver_pattern ]] ||
    die "Version must be strict, filename-safe semantic versioning."
major="$((10#${BASH_REMATCH[1]}))"
minor="$((10#${BASH_REMATCH[2]}))"
patch="$((10#${BASH_REMATCH[3]}))"
for component in "$major" "$minor" "$patch"; do
    ((component <= 65535)) ||
        die "Windows version components must be between 0 and 65535."
done
file_version="$major.$minor.$patch.$build_number"

if [[ -z "$build_date" ]]; then
    build_date="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
fi
if [[ -z "$commit" ]]; then
    commit="dev"
    if command -v git >/dev/null 2>&1; then
        candidate="$(git -C "$project_root" rev-parse --short=12 HEAD 2>/dev/null || true)"
        if [[ -n "$candidate" ]]; then
            commit="$candidate"
        fi
    fi
fi
[[ "$commit" =~ ^[A-Za-z0-9._+-]{1,64}$ ]] ||
    die "Commit may contain only letters, numbers, dot, underscore, plus, and hyphen."
[[ "$build_date" =~ ^[0-9T:Z+.-]{1,64}$ ]] ||
    die "Build date must be compact ISO-8601 without spaces."

mkdir -p -- "$dist_root"
output_path="$dist_root/RayanLamp.exe"
build_lock_path="$dist_root/.rayanlamp-build.lock"
temporary_root=""
temporary_output=""
app_resource="$project_root/cmd/RayanLamp/zz_rayanlamp_windows_${architecture}.syso"
owns_resource=false
temporary_prefix="${TMPDIR:-/tmp}"
[[ "$temporary_prefix" == /* ]] ||
    die "TMPDIR must be an absolute path."
temporary_prefix="${temporary_prefix%/}/RayanLamp-build."

exec {build_lock_fd}> "$build_lock_path"
flock -n "$build_lock_fd" ||
    die "Another RayanLamp release build is active."

cleanup() {
    local status=$?
    if $owns_resource && [[ -f "$app_resource" ]]; then
        rm -f -- "$app_resource"
    fi
    if [[ -n "$temporary_output" && -f "$temporary_output" ]]; then
        rm -f -- "$temporary_output"
    fi
    if [[ -n "$temporary_root" &&
        "$temporary_root" == "$temporary_prefix"* &&
        -d "$temporary_root" ]]; then
        rm -rf -- "$temporary_root"
    fi
    flock -u "$build_lock_fd" >/dev/null 2>&1 || true
    eval "exec ${build_lock_fd}>&-"
    rm -f -- "$build_lock_path"
    return "$status"
}
trap cleanup EXIT

shopt -s nullglob dotglob
release_entries=("$dist_root"/*)
shopt -u nullglob dotglob
for release_entry in "${release_entries[@]}"; do
    if [[ "$release_entry" != "$output_path" &&
        "$release_entry" != "$build_lock_path" ]]; then
        die "The release directory must contain only RayanLamp.exe; found $release_entry."
    fi
done

if [[ -e "$output_path" ]] && ! $force; then
    die "Release executable already exists: $output_path. Pass --force to replace it."
fi
[[ ! -e "$app_resource" ]] ||
    die "A stale generated resource already exists: $app_resource"

temporary_root="$(mktemp -d "${temporary_prefix}XXXXXXXX")"
temporary_output="$(mktemp "$dist_root/.RayanLamp.exe.XXXXXXXX")"
manifest_path="$temporary_root/RayanLamp.exe.manifest"

awk -v version="$file_version" '
    /name="YektaYar\.RayanLamp"/ { in_app_identity = 1 }
    in_app_identity && /version="/ {
        sub(/version="[^"]+"/, "version=\"" version "\"")
        in_app_identity = 0
    }
    { print }
' "$project_root/resources/RayanLamp.exe.manifest" > "$manifest_path"
grep -Fq "version=\"$file_version\"" "$manifest_path" ||
    die "Failed to stamp the application manifest version."

info "Linux host -> Windows/$architecture | $edition edition | version $file_version | CGO disabled"

if $skip_web_build; then
    step "Validating the checked-in SPA/PWA distribution"
else
    for command_name in node npm; do
        require_command "$command_name"
    done
    step "Building the embedded SPA/PWA from its npm lockfile"
    (
        cd -- "$web_root"
        npm ci --no-audit --no-fund
        npm run typecheck
        if ! $skip_tests; then
            npm test
        fi
        VITE_RAYANLAMP_BUILD_EDITION="$edition" npm run build
    )
fi

for relative_web_file in index.html build-edition.json manifest.webmanifest sw.js theme-init.js; do
    [[ -f "$web_dist_root/$relative_web_file" ]] ||
        die "Bundled web dashboard is incomplete: $web_dist_root/$relative_web_file"
done
expected_web_marker="{\"edition\":\"$edition\",\"developer_tools\":$developer_tools}"
actual_web_marker="$(<"$web_dist_root/build-edition.json")"
[[ "$actual_web_marker" == "$expected_web_marker" ]] ||
    die "Embedded Web UI does not match the requested $edition edition."
shopt -s nullglob globstar
source_maps=("$web_dist_root"/**/*.map)
shopt -u nullglob globstar
if [[ "$edition" == "production" ]] && ((${#source_maps[@]} != 0)); then
    die "Production frontend must not contain source maps."
fi
if [[ "$edition" == "develop" ]] && ((${#source_maps[@]} == 0)); then
    die "Development frontend must retain source maps."
fi
shopt -s nullglob
xterm_chunks=("$web_dist_root"/assets/xterm-*.js)
webgl_chunks=("$web_dist_root"/assets/addon-webgl-*.js)
shopt -u nullglob
((${#xterm_chunks[@]} == 1)) ||
    die "frontend must retain one lazy xterm chunk"
((${#webgl_chunks[@]} == 1)) ||
    die "frontend must retain one lazy WebGL addon chunk"
if [[ "$edition" == "production" ]]; then
    for forbidden_marker in \
        sourceMappingURL \
        @preact/debug \
        __VITEST__ \
        test-fixtures; do
        if grep -R -F -q --include='*.js' \
            "$forbidden_marker" "$web_dist_root"; then
            die "production frontend contains developer marker: $forbidden_marker"
        fi
    done
fi
success "Embedded $edition web dashboard is ready"

host_os="$(go env GOHOSTOS)"
host_architecture="$(go env GOHOSTARCH)"
[[ -n "$host_os" && -n "$host_architecture" ]] ||
    die "Could not determine the native Go host platform."

if [[ "$edition" == "production" ]]; then
    step "Verifying that developer servers and SDK bridges are absent"
    dependency_graph="$(
        env GOOS=windows GOARCH="$architecture" CGO_ENABLED=0 \
            go list -tags "$build_tag" -deps ./cmd/RayanLamp
    )"
    for forbidden_dependency in \
        expvar \
        net/http/pprof \
        runtime/pprof \
        runtime/trace \
        testing \
        rayanlamp.local/app/internal/dllapi \
        rayanlamp.local/app/sdk; do
        if grep -Fxq "$forbidden_dependency" <<< "$dependency_graph"; then
            die "production dependency graph contains developer-only package: $forbidden_dependency"
        fi
    done
    success "Production dependency graph excludes developer-only packages"
fi

if ! $skip_vet; then
    step "Vetting the Windows/$architecture codebase"
    env GOOS=windows GOARCH="$architecture" CGO_ENABLED=0 \
        go vet -tags "$build_tag" ./...
    success "Windows-targeted go vet passed"
fi

if ! $skip_tests; then
    step "Running tests supported by the native $host_os/$host_architecture host"
    env GOOS="$host_os" GOARCH="$host_architecture" CGO_ENABLED=0 \
        go test -count=1 -tags "$build_tag" ./...
    success "Native-host test suites passed"

    step "Cross-compiling every Windows/$architecture test binary"
    env GOOS=windows GOARCH="$architecture" CGO_ENABLED=0 \
        go test -count=1 -tags "$build_tag" -run '^$' -exec true ./...
    success "Every Windows test package cross-compiled"
fi

resource_is_64=false
resource_is_arm=false
if [[ "$architecture" == "amd64" || "$architecture" == "arm64" ]]; then
    resource_is_64=true
fi
if [[ "$architecture" == "arm64" ]]; then
    resource_is_arm=true
fi

step "Generating the $architecture icon, version, and manifest resources"
owns_resource=true
env GOOS="$host_os" GOARCH="$host_architecture" CGO_ENABLED=0 \
    go run "$resource_tool" \
    "-64=$resource_is_64" \
    "-arm=$resource_is_arm" \
    "-icon=$project_root/assets/RayanLamp.ico" \
    "-manifest=$manifest_path" \
    "-file-version=$file_version" \
    "-product-version=$version" \
    "-ver-major=$major" \
    "-ver-minor=$minor" \
    "-ver-patch=$patch" \
    "-ver-build=$build_number" \
    "-product-ver-major=$major" \
    "-product-ver-minor=$minor" \
    "-product-ver-patch=$patch" \
    "-product-ver-build=$build_number" \
    "-o=$app_resource" \
    "$project_root/resources/versioninfo.json"
[[ -f "$app_resource" ]] ||
    die "Resource generation did not create $app_resource."
success "Architecture-correct COFF resources generated"

step "Linking the standalone Windows console-subsystem $edition executable"
linker_flags=""
if [[ "$edition" == "production" ]]; then
    linker_flags="-s -w"
fi
linker_flags+=" -X=rayanlamp.local/app/internal/buildinfo.Commit=$commit"
linker_flags+=" -X=rayanlamp.local/app/internal/buildinfo.Date=$build_date"
env GOOS=windows GOARCH="$architecture" CGO_ENABLED=0 \
    go build -trimpath -buildvcs=false -tags "$build_tag" \
    -ldflags "$linker_flags" \
    -o "$temporary_output" ./cmd/RayanLamp

step "Inspecting PE architecture, console subsystem, resources, and embedded UI"
env GOOS="$host_os" GOARCH="$host_architecture" CGO_ENABLED=0 \
    go run ./tools/verify-pe \
    --file "$temporary_output" \
    --arch "$architecture" \
    --file-version "$file_version" \
    --edition "$edition"
success "Unpacked executable passed structural verification"

if $compress; then
    if [[ "$edition" == "develop" ]]; then
        warn "UPX makes native debugger workflows less convenient; production edition is recommended for compression."
    fi
    if [[ "$architecture" == "arm64" ]]; then
        warn "UPX is intentionally skipped for ARM64; the executable remains uncompressed."
    else
        if [[ -n "$upx_path" ]]; then
            [[ -f "$upx_path" && -x "$upx_path" ]] ||
                die "The requested UPX executable is unavailable or not executable: $upx_path"
        else
            upx_path="$(command -v upx || true)"
        fi
        if [[ -z "$upx_path" ]]; then
            warn "UPX was requested but is unavailable; the executable remains uncompressed."
        else
            step "Compressing the executable with UPX"
            "$upx_path" --best --lzma --no-progress -- "$temporary_output"
            "$upx_path" -t --no-progress -- "$temporary_output"
            step "Inspecting the packed PE resources that will be published"
            env GOOS="$host_os" GOARCH="$host_architecture" CGO_ENABLED=0 \
                go run ./tools/verify-pe \
                --file "$temporary_output" \
                --arch "$architecture" \
                --file-version "$file_version" \
                --packed
            success "UPX compression and integrity verification passed"
        fi
    fi
fi

mv -f -- "$temporary_output" "$output_path"
temporary_output=""
output_hash="$(sha256sum "$output_path" | awk '{print $1}')"

success "Built one standalone $edition executable: $output_path"
rl_hash "$output_hash"
info "CGO_ENABLED=0; the $edition SPA/PWA, icon, manifest, and version resources are embedded"
printf '%s\n' "$output_path"
