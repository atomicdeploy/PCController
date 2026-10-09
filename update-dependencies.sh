#!/usr/bin/env sh
set -eu
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ ! -f "$SCRIPT_DIR/Tools/Build/node_modules/chalk/package.json" ] ||
   [ ! -f "$SCRIPT_DIR/Tools/Build/node_modules/cli-table3/package.json" ]; then
  npm --prefix "$SCRIPT_DIR/Tools/Build" ci --ignore-scripts --no-audit --no-fund
fi
exec node "$SCRIPT_DIR/Tools/Dependencies/update.mjs" "$@"
