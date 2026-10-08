# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27.1
ARG NODE_VERSION=24
FROM node:${NODE_VERSION}-bookworm-slim AS node

FROM golang:${GO_VERSION}-bookworm AS dev
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
    && apt-get update \
    && apt-get install -y --no-install-recommends sqlite3 ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /workspace
CMD ["sleep", "infinity"]

FROM dev AS build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY web/package.json web/package-lock.json ./web/
RUN --mount=type=cache,target=/root/.npm npm ci --prefix web
COPY . .
RUN npm --prefix web run build
ARG VERSION=dev
ARG AGENT_VERSION=v0.8.1
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.agentVersion=${AGENT_VERSION}" \
    -o /out/bfs ./cmd/bfs

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 boxfleet \
    && useradd --uid 10001 --gid 10001 --no-create-home boxfleet \
    && mkdir /data && chown boxfleet:boxfleet /data
COPY --from=build /out/bfs /usr/local/bin/bfs
USER 10001:10001
EXPOSE 18081
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD curl -fsS http://127.0.0.1:18081/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/bfs"]
CMD ["--addr", "0.0.0.0:18081", "--db", "/data/boxfleet.db"]
