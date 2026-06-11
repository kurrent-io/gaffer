#!/bin/bash
set -euo pipefail

# Grant access to the host's bind-mounted docker socket. The container's
# `docker` group GID won't match the host's, so add the user to a group
# whose GID matches the socket's owner.
#
# SECURITY: access to the host docker socket is equivalent to host root.
# This runs `just init` (pnpm install etc.) on the checked-out branch, so
# only open branches you trust. See devcontainer.json for the trust note.
if [ -S /var/run/docker.sock ]; then
  HOST_DOCKER_GID=$(stat -c '%g' /var/run/docker.sock)
  if ! getent group "$HOST_DOCKER_GID" >/dev/null; then
    sudo groupadd -g "$HOST_DOCKER_GID" docker-host
  fi
  sudo usermod -aG "$HOST_DOCKER_GID" "$(whoami)"
fi

# Skip pnpm's interactive purge confirmation when re-using a workspace that
# was previously initialised outside the container.
export CI=true

# Surface the shared GOPATH bin so binaries installed by `just init`
# (cue, golangci-lint) are findable. Dockerfile-level ENV PATH doesn't
# survive sudo's user switch into vscode; setting it here does.
export GOPATH=/go
export PATH="/go/bin:$PATH"

just init
