#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
if ! command -v node >/dev/null 2>&1; then
	printf '%s\n' '[ERROR] Node.js 20.19 or newer was not found in PATH.' >&2
	exit 1
fi

exec node "${repo_root}/Tools/Build/build.mjs" "$@"
