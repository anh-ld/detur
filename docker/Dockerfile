# detur — self-hosted Detour-compatible deferred deep-link server.
# Multi-stage: static Go binary (pure-Go SQLite, CGO_ENABLED=0) + kinu portal
# dist -> slim alpine runtime as non-root user (uid 1000).
#
# Healthcheck lives in docker-compose.yml (busybox wget inside the container),
# not here — see docs/guide.md -> Ops for why.

# --- Stage 1: Go server ----------------------------------------------------
FROM golang:alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/cmd ./cmd
COPY server/internal ./internal
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/detur ./cmd/detur
# Pre-create the data dir owned by the runtime uid so a fresh named volume
# inherits writable ownership (Docker copies image dir metadata into new
# volumes).
RUN mkdir -p /out/data && chown -R 1000:1000 /out/data

# --- Stage 2: portal (kinu, vite) -------------------------------------------
FROM node:lts-alpine AS portal
WORKDIR /src
COPY portal/package.json portal/package-lock.json ./
RUN npm ci
COPY portal/ .
RUN npm run build   # vite outDir=dist -> /src/dist

# --- Stage 3: runtime --------------------------------------------------------
FROM alpine:3.21
RUN adduser -D -u 1000 -h /app detur
COPY --from=build --chown=detur:detur /out/detur /app/detur
COPY --from=build --chown=detur:detur /out/data /data
COPY --from=portal --chown=detur:detur /src/dist /app/portal-dist
ENV DETUR_PORTAL_DIR=/app/portal-dist \
    DETUR_DB_PATH=/data/detur.db \
    DETUR_PORTAL_ADDR=0.0.0.0:8081
USER detur
EXPOSE 8080 8081
VOLUME ["/data"]
ENTRYPOINT ["/app/detur"]