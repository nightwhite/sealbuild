FROM --platform=linux/amd64 golang:1.26.1-bookworm@sha256:09fb8a652cf7a990b714c46a9f0f5fd2d5bc2222d995166b91907c1c05b7d0e8

ARG DEV_PROXY

RUN if [ -n "${DEV_PROXY}" ]; then \
      export http_proxy="${DEV_PROXY}" https_proxy="${DEV_PROXY}"; \
    fi; \
    apt-get update && \
    apt-get install --yes --no-install-recommends \
      bc build-essential ca-certificates cpio curl e2fsprogs file git jq \
      libelf-dev libglib2.0-dev libpixman-1-dev libslirp-dev ninja-build \
      openssl perl pkg-config python3-venv python3-wheel rsync tar unzip wget xz-utils zstd && \
    rm -rf /var/lib/apt/lists/*
