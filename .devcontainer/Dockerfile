# Base image is left on a floating tag deliberately: it's an official,
# registry-signed image, and letting it float is what lets the weekly
# prebuild absorb Ubuntu/apt security updates unattended. Digest-pinning it
# here would freeze the OS userland until someone bumped it by hand - the
# opposite of what we want. We pin only what we fetch-and-extract ourselves.
FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

SHELL ["/bin/bash", "-euo", "pipefail", "-c"]

# Toolchains are pinned to an exact version and verified against a committed
# checksum rather than installed via pipe-to-shell or a floating "latest"
# tarball. The weekly prebuild pushes ghcr.io/.../gaffer-devcontainer:latest
# unattended, so without this a compromise of any upstream endpoint would
# propagate straight to every developer. Bump these in a reviewed PR.
ARG TARGETARCH
ARG JUST_VERSION=1.52.0
ARG DOTNET_SDK_VERSION=10.0.301
ARG GO_VERSION=1.26.4
ARG NODE_VERSION=22.22.3

# just (task runner)
RUN case "${TARGETARCH}" in \
      amd64) ARCH=x86_64;  SHA256=e0a712614574d45625332529c864754e26782422b7c7db931466180a618f092d ;; \
      arm64) ARCH=aarch64; SHA256=41af45372c49c404ca7b9a318ae9de5564636e39c6520cc4d42bf678f3bfb1bc ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL "https://github.com/casey/just/releases/download/${JUST_VERSION}/just-${JUST_VERSION}-${ARCH}-unknown-linux-musl.tar.gz" -o /tmp/just.tar.gz \
 && echo "${SHA256}  /tmp/just.tar.gz" | sha256sum -c - \
 && tar -C /usr/local/bin -xzf /tmp/just.tar.gz just \
 && rm /tmp/just.tar.gz

# .NET SDK
RUN case "${TARGETARCH}" in \
      amd64) ARCH=x64;   SHA512=cfbeec3a3a1d3ad3e168e37a77c4cc26c23125acd84a86d014047da3ecffce4c368a9acac4d7c950a047fa3d98989ce8aea69f8e5842cb6d330e8911e1c335a7 ;; \
      arm64) ARCH=arm64; SHA512=4ee438b363cb8468930d50b6bdc738a375e2f33b25bfd0c8dcb55853dda7f0fb187693e0f49dfc31556e68320b961a50dcf3b74c1f25abe3a5bd916db607db99 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL "https://builds.dotnet.microsoft.com/dotnet/Sdk/${DOTNET_SDK_VERSION}/dotnet-sdk-${DOTNET_SDK_VERSION}-linux-${ARCH}.tar.gz" -o /tmp/dotnet.tar.gz \
 && echo "${SHA512}  /tmp/dotnet.tar.gz" | sha512sum -c - \
 && mkdir -p /usr/share/dotnet \
 && tar -C /usr/share/dotnet -xzf /tmp/dotnet.tar.gz \
 && ln -sf /usr/share/dotnet/dotnet /usr/local/bin/dotnet \
 && rm /tmp/dotnet.tar.gz

# Go
RUN case "${TARGETARCH}" in \
      amd64) ARCH=amd64; SHA256=1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f ;; \
      arm64) ARCH=arm64; SHA256=ef758ae7c6cf9267c9c0ef080b8965f453d89ab2d25d9eb22de4405925238768 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz \
 && echo "${SHA256}  /tmp/go.tar.gz" | sha256sum -c - \
 && tar -C /usr/local -xzf /tmp/go.tar.gz \
 && ln -sf /usr/local/go/bin/go /usr/local/bin/go \
 && ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt \
 && rm /tmp/go.tar.gz

# Node (+ pnpm via the bundled corepack)
RUN case "${TARGETARCH}" in \
      amd64) ARCH=x64;   SHA256=c7a10d6816da8eaaa7534dd73c71c6e2b2c391dbbf845e364902d156615dd1b8 ;; \
      arm64) ARCH=arm64; SHA256=cc8bc82b2dd0b595c3b95a4c3c9c8c350907cff011afbdee3d1379e812e1e3e3 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${ARCH}.tar.gz" -o /tmp/node.tar.gz \
 && echo "${SHA256}  /tmp/node.tar.gz" | sha256sum -c - \
 && tar -C /usr/local --strip-components=1 -xzf /tmp/node.tar.gz \
 && rm /tmp/node.tar.gz \
 && corepack enable

# Shared GOPATH so any user's `go install` (cue, golangci-lint, ad-hoc
# tools) lands in /go/bin. PATH addition covers devpod ssh sessions
# (which run as root and keep ENV); postCreate.sh re-exports it because
# sudo strips PATH when switching to vscode.
ENV GOPATH=/go \
    PATH=/go/bin:$PATH
RUN mkdir -p /go/bin && chmod -R 777 /go

# Docker CLI + Compose plugin. No daemon - the host's /var/run/docker.sock
# is bind-mounted at runtime (see devcontainer.json `mounts`). Left on the
# apt channel intentionally: the GPG-signed Docker repo gives package
# integrity, so this isn't pipe-to-shell or an unverified tarball and
# doesn't need a pinned hash like the toolchains above.
RUN install -m 0755 -d /etc/apt/keyrings \
 && curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc \
 && chmod a+r /etc/apt/keyrings/docker.asc \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends docker-ce-cli docker-buildx-plugin docker-compose-plugin \
 && rm -rf /var/lib/apt/lists/*
