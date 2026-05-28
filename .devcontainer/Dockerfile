FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

SHELL ["/bin/bash", "-euo", "pipefail", "-c"]

# just (task runner)
RUN curl -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin

# .NET 10 SDK
RUN curl -fsSL https://dot.net/v1/dotnet-install.sh -o /tmp/dotnet-install.sh \
 && bash /tmp/dotnet-install.sh --channel 10.0 --install-dir /usr/share/dotnet \
 && ln -sf /usr/share/dotnet/dotnet /usr/local/bin/dotnet \
 && rm /tmp/dotnet-install.sh

# Go (latest stable - refreshed each weekly prebuild)
ARG TARGETARCH
RUN ARCH=${TARGETARCH:-amd64} \
 && GO_VERSION=$(curl -fsSL "https://go.dev/VERSION?m=text" | head -1) \
 && curl -fsSL "https://go.dev/dl/${GO_VERSION}.linux-${ARCH}.tar.gz" | tar -C /usr/local -xz \
 && ln -sf /usr/local/go/bin/go /usr/local/bin/go \
 && ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

# Node 22 (+ pnpm via corepack)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && corepack enable \
 && rm -rf /var/lib/apt/lists/*

# Shared GOPATH so any user's `go install` (cue, golangci-lint, ad-hoc
# tools) lands in /go/bin. PATH addition covers devpod ssh sessions
# (which run as root and keep ENV); postCreate.sh re-exports it because
# sudo strips PATH when switching to vscode.
ENV GOPATH=/go \
    PATH=/go/bin:$PATH
RUN mkdir -p /go/bin && chmod -R 777 /go

# Docker CLI + Compose plugin. No daemon - the host's /var/run/docker.sock
# is bind-mounted at runtime (see devcontainer.json `mounts`).
RUN install -m 0755 -d /etc/apt/keyrings \
 && curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc \
 && chmod a+r /etc/apt/keyrings/docker.asc \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends docker-ce-cli docker-buildx-plugin docker-compose-plugin \
 && rm -rf /var/lib/apt/lists/*
