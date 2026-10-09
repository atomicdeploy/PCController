#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
if ! command -v node >/dev/null 2>&1; then
	printf '%s\n' '[ERROR] Node.js 22.12 or newer was not found in PATH.' >&2
	printf '%s\n' '        Install Node.js, then run this command again.' >&2
	exit 1
fi

# MSYS/Cygwin expose POSIX paths even when PATH resolves native Windows Node.
# Convert only for that combination; passing /mnt/c/... or /c/... to node.exe
# otherwise turns it into an invalid C:\mnt\c\... or C:\c\... path.
if command -v cygpath >/dev/null 2>&1 && [[ "$(node -p 'process.platform')" == "win32" ]]; then
	repo_root="$(cygpath -w "${repo_root}")"
fi

if [[ ! -f "${repo_root}/Tools/Build/node_modules/chalk/package.json" ||
	! -f "${repo_root}/Tools/Build/node_modules/cli-table3/package.json" ]]; then
	printf '%s\n' '[SETUP] Installing locked build UI dependencies...'
	node "${repo_root}/Tools/Build/env-bootstrap.mjs" install-build-dependencies
fi

exec node "${repo_root}/Tools/Build/build.mjs" "$@"
