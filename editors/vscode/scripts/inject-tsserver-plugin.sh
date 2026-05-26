#!/usr/bin/env bash
# vsce hard-codes node_modules/** as ignored when invoked with
# --no-dependencies (which we need because vsce's npm-list dep walk
# breaks on pnpm's symlinked store). The tsserver plugin must live
# in node_modules/ at runtime so VS Code's tsserver can require()
# it. So we package without it and zip it in here.
#
# Note: appending to the .vsix doesn't refresh extension.vsixmanifest
# or [Content_Types].xml. VS Code installs accept this; marketplace
# signing would not. If we ever ship via the marketplace, sign AFTER
# this step.

set -euo pipefail
cd "$(dirname "$0")/.."

# Target the .vsix vsce just produced by name (not "newest in cwd"),
# so a stale .vsix from a prior run can't hijack the inject step.
NAME=$(node -p "require('./package.json').name")
VERSION=$(node -p "require('./package.json').version")
VSIX="${NAME}-${VERSION}.vsix"
if [[ ! -f "$VSIX" ]]; then
	echo "inject-tsserver-plugin: $VSIX not found in $(pwd) - did vsce package run?" >&2
	exit 1
fi

# Read the plugin from its source workspace package, not the injected
# copy at `node_modules/gaffer-tsserver-plugin/`. pnpm hard-links the
# injected copy at install time and doesn't refresh it after the
# plugin's `dist/` is built, so the injected dir is unreliable. Source
# and injected content are identical when both are fresh, but source
# is always there after `pnpm --filter gaffer-tsserver-plugin run build`.
PLUGIN_DIST_SRC=tsserver-plugin
# Path the plugin must live at inside the .vsix so VS Code's tsserver
# can require() it at runtime.
PLUGIN_RUNTIME_PATH=node_modules/gaffer-tsserver-plugin
if [[ ! -f "$PLUGIN_DIST_SRC/dist/index.js" ]]; then
	echo "inject-tsserver-plugin: $PLUGIN_DIST_SRC/dist/index.js missing - build the plugin first" >&2
	exit 1
fi

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

DEST="$STAGE/extension/$PLUGIN_RUNTIME_PATH"
mkdir -p "$DEST/dist"
cp "$PLUGIN_DIST_SRC/dist/index.js" "$DEST/dist/index.js"
cp "$PLUGIN_DIST_SRC/package.json" "$DEST/package.json"

# Resolve via cd/pwd rather than realpath - macOS ships realpath only
# in recent versions and we want this script portable.
VSIX_ABS="$(cd "$(dirname "$VSIX")" && pwd)/$(basename "$VSIX")"
( cd "$STAGE" && zip -qr "$VSIX_ABS" extension/node_modules )
echo "inject-tsserver-plugin: added $PLUGIN_RUNTIME_PATH to $VSIX"
