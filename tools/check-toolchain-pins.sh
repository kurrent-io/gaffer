#!/usr/bin/env bash
# Checks that every toolchain pinned in more than one place agrees across
# its copies. Each toolchain has one source of truth - go.work's toolchain
# directive (Go), global.json (dotnet SDK), .nvmrc (Node) - which the
# devcontainer Dockerfile repeats alongside its download checksums. The
# just version lives in the Dockerfile and is repeated by every setup-just
# step in the workflows (the action reads no version file). CI runs this
# on every PR so a bump that misses a copy fails fast instead of
# surfacing as behavior drift between CI and the devcontainer.
set -euo pipefail

cd "$(dirname "$0")/.."

status=0
mismatch() {
	echo "toolchain pin mismatch: $1" >&2
	status=1
}

dockerfile_arg() {
	sed -n "s/^ARG $1=//p" .devcontainer/Dockerfile
}

go_source=$(sed -n 's/^toolchain go//p' go.work)
go_container=$(dockerfile_arg GO_VERSION)
if [[ -z "$go_source" ]]; then
	mismatch "go.work is missing its toolchain directive"
elif [[ "$go_source" != "$go_container" ]]; then
	mismatch "Go is $go_source in go.work but $go_container in the devcontainer Dockerfile"
fi

dotnet_source=$(grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' global.json | head -1 | grep -o '[0-9][^"]*' || true)
dotnet_container=$(dockerfile_arg DOTNET_SDK_VERSION)
if [[ "$dotnet_source" != "$dotnet_container" ]]; then
	mismatch "dotnet SDK is ${dotnet_source:-unset} in global.json but $dotnet_container in the devcontainer Dockerfile"
fi

node_source=$(tr -d '[:space:]' <.nvmrc)
node_container=$(dockerfile_arg NODE_VERSION)
if [[ "$node_source" != "$node_container" ]]; then
	mismatch "Node is ${node_source:-unset} in .nvmrc but $node_container in the devcontainer Dockerfile"
fi

# Every setup-just step must carry a just-version pin (an unpinned step
# floats to the latest release), and every pin must match the Dockerfile.
just_container=$(dockerfile_arg JUST_VERSION)
setup_count=$(grep -hc 'extractions/setup-just@' .github/workflows/*.yml | awk '{n += $1} END {print n + 0}' || true)
pin_count=$(grep -hc 'just-version:' .github/workflows/*.yml | awk '{n += $1} END {print n + 0}' || true)
if [[ "$setup_count" != "$pin_count" ]]; then
	mismatch "$setup_count setup-just steps but $pin_count just-version pins in the workflows"
fi
while read -r version; do
	if [[ "$version" != "$just_container" ]]; then
		mismatch "a workflow pins just $version but the devcontainer Dockerfile has $just_container"
	fi
done < <(sed -n 's/.*just-version:[[:space:]]*//p' .github/workflows/*.yml)

exit "$status"
