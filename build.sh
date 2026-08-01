#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
if ! command -v node >/dev/null 2>&1; then
	printf '%s\n' '[ERROR] Node.js 22.12 or newer was not found in PATH.' >&2
	exit 1
fi

if [[ ! -f "${repo_root}/Tools/Build/node_modules/chalk/package.json" ||
	! -f "${repo_root}/Tools/Build/node_modules/cli-table3/package.json" ]]; then
	if ! command -v npm >/dev/null 2>&1; then
		printf '%s\n' '[ERROR] npm is required to install the locked build UI dependencies.' >&2
		exit 1
	fi
	printf '%s\n' '[SETUP] Installing locked build UI dependencies...'
	npm --prefix "${repo_root}/Tools/Build" ci --ignore-scripts --no-audit --no-fund
fi

exec node "${repo_root}/Tools/Build/build.mjs" "$@"
