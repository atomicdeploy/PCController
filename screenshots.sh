#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
if ! command -v node >/dev/null 2>&1; then
    printf '%s\n' '[ERROR] Node.js was not found in PATH.' >&2
    exit 1
fi
if command -v cygpath >/dev/null 2>&1 && [[ "$(node -p 'process.platform')" == "win32" ]]; then
    repo_root="$(cygpath -w "${repo_root}")"
fi
exec node "${repo_root}/Tools/Build/update-readme-previews.mjs" "$@"
