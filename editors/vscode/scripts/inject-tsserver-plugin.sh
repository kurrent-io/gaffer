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

# Resolve via cd/pwd rather than realpath - macOS ships realpath only
# in recent versions and we want this script portable.
VSIX_ABS="$(cd "$(dirname "$VSIX")" && pwd)/$(basename "$VSIX")"
( cd "$STAGE" && zip -qr "$VSIX_ABS" extension/node_modules )
echo "inject-tsserver-plugin: added $PLUGIN_SRC to $VSIX"
