#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"

if ! command -v node >/dev/null 2>&1; then
    printf '%s\n' '[ERROR] Node.js 22.12 or newer was not found in PATH.' >&2
    printf '%s\n' '        Install Node.js, then run this command again.' >&2
    exit 1
fi

exec node "${repo_root}/Tools/Firmware/firmware.mjs" "$@"
