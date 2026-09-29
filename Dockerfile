# detur: self-hosted Detour-compatible deferred deep-link server.
# Multi-stage: static Go binary (pure-Go SQLite, CGO_ENABLED=0) + kinu portal
# dist -> slim alpine runtime as non-root user (uid 1000).

# --- Stage 1: Go server ---
FROM golang:alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/cmd ./cmd
COPY server/internal ./internal
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/detur ./cmd/detur
# Pre-create data dir owned by runtime uid: fresh named volume inherits writable ownership (Docker copies image dir metadata into new volumes).
RUN mkdir -p /out/data && chown -R 1000:1000 /out/data

# --- Stage 2: portal (kinu, vite) ---
FROM node:lts-alpine AS portal
WORKDIR /src
COPY portal/package.json portal/package-lock.json ./
RUN npm ci
COPY portal/ .
RUN npm run build   # vite outDir=dist -> /src/dist

# --- Stage 3: runtime ---
FROM alpine:3.21
RUN adduser -D -u 1000 -h /app detur
COPY --from=build --chown=detur:detur /out/detur /app/detur
COPY --from=build --chown=detur:detur /out/data /data
COPY --from=portal --chown=detur:detur /src/dist /app/portal/dist
ENV DB_PATH=/data/detur.db
WORKDIR /app
USER detur
EXPOSE 8080 8081
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://localhost:8080/health || exit 1
# Portal on 0.0.0.0 inside container; publish on host loopback only.
ENTRYPOINT ["/app/detur", "-portal-addr=0.0.0.0:8081"]
