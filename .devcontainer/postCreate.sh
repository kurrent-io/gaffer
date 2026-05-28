#!/bin/bash
set -euo pipefail

# Devcontainer feature installers (node via nvm, go, dotnet) add tools to
# paths that aren't in the default shell PATH during lifecycle commands.
# Source them explicitly so `just init` can find everything.
export PATH="/usr/local/share/nvm/current/bin:/usr/local/go/bin:/go/bin:$PATH:/usr/share/dotnet"

just init
