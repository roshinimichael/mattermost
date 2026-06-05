# syntax=docker/dockerfile:1
# Multi-stage build: compiles Mattermost server from PR source code.
# Stage 1 builds the Go binaries; Stage 2 creates a minimal runtime image.

# Stage 1: Build mattermost + mmctl from source
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
        make git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build
# Copy only the server source — no webapp needed for API testing
COPY server/ server/

# Set up Go workspace (mirrors `make setup-go-work` without enterprise dir)
# Then build the mattermost server binary and mmctl binary
RUN cd server \
    && go work init \
    && go work use . \
    && go work use ./public \
    && env GOOS=linux GOARCH=amd64 GOFLAGS="-buildvcs=false" \
        go build -trimpath -tags production -o /dist/mattermost ./cmd/mattermost/ \
    && env GOOS=linux GOARCH=amd64 GOFLAGS="-buildvcs=false" \
        go build -trimpath -tags production -o /dist/mmctl ./cmd/mmctl/

# Stage 2: Minimal runtime — only what Mattermost needs to serve the API
FROM ubuntu:24.04

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --gid 2000 mattermost \
    && useradd --uid 2000 --gid 2000 --home-dir /mattermost mattermost \
    && mkdir -p \
        /mattermost/bin \
        /mattermost/data \
        /mattermost/config \
        /mattermost/logs \
        /mattermost/plugins \
        /mattermost/client/plugins \
    && chown -R 2000:2000 /mattermost

COPY --from=builder --chown=2000:2000 /dist/mattermost /mattermost/bin/mattermost
COPY --from=builder --chown=2000:2000 /dist/mmctl     /mattermost/bin/mmctl
# i18n translation files and email templates are required by the server at runtime
COPY --chown=2000:2000 server/i18n/      /mattermost/i18n/
COPY --chown=2000:2000 server/templates/ /mattermost/templates/

ENV PATH="/mattermost/bin:${PATH}"
ENV MM_SERVICESETTINGS_ENABLELOCALMODE="true"
ENV MM_INSTALL_TYPE="docker"

EXPOSE 8065
USER mattermost
WORKDIR /mattermost
CMD ["mattermost", "server"]
