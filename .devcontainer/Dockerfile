FROM golang:1.24-bookworm

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    git make bash curl ca-certificates pkg-config build-essential \
    && rm -rf /var/lib/apt/lists/*

# Dev user
RUN useradd -m -s /bin/bash goland

# Use per-user GOPATH (overrides the base image's GOPATH=/go)
ENV GOPATH=/home/goland/go
ENV PATH=$GOPATH/bin:$PATH

# Prepare workspace
RUN mkdir -p /workspaces/mdai-gateway && chown -R goland:goland /workspaces /home/goland

USER goland
WORKDIR /workspaces/mdai-gateway