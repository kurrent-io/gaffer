#!/usr/bin/env bash
# vsce hard-codes node_modules/** as ignored when invoked with
# --no-dependencies (which we need because vsce's npm-list dep walk
# breaks on pnpm's symlinked store). The tsserver plugin must live
# in node_modules/ at runtime so VS Code's tsserver can require()
# it. So we package without it and zip it in here.

set -euo pipefail
cd "$(dirname "$0")/.."

VSIX=$(ls -t -- *.vsix 2>/dev/null | head -1)
if [[ -z "${VSIX:-}" ]]; then
	echo "inject-tsserver-plugin: no .vsix found in $(pwd)" >&2
	exit 1
fi

PLUGIN_SRC=node_modules/gaffer-tsserver-plugin
if [[ ! -f "$PLUGIN_SRC/dist/index.js" ]]; then
	echo "inject-tsserver-plugin: $PLUGIN_SRC/dist/index.js missing - build the plugin first" >&2
	exit 1
fi

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

DEST="$STAGE/extension/$PLUGIN_SRC"
mkdir -p "$DEST/dist"
cp "$PLUGIN_SRC/dist/index.js" "$DEST/dist/index.js"
cp "$PLUGIN_SRC/package.json" "$DEST/package.json"

VSIX_ABS=$(realpath "$VSIX")
( cd "$STAGE" && zip -qr "$VSIX_ABS" extension/node_modules )
echo "inject-tsserver-plugin: added $PLUGIN_SRC to $VSIX"
