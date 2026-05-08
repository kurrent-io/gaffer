#!/bin/bash
set -euo pipefail

# Install just (task runner)
curl -sSf https://just.systems/install.sh | sudo bash -s -- --to /usr/local/bin

# Install jq (used by the telemetry codegen pipeline). The base Ubuntu image
# usually has it, but install explicitly so `just init` doesn't depend on a
# host-image quirk.
sudo apt-get update -qq
sudo apt-get install -y -qq jq

# Devcontainer feature installers (node via nvm, go, dotnet) add tools to
# paths that aren't in the default shell PATH during lifecycle commands.
# Source them explicitly so `just init` can find everything.
export PATH="/usr/local/share/nvm/current/bin:/usr/local/go/bin:/go/bin:$PATH:/usr/share/dotnet"

just init
