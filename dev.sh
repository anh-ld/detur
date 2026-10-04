#!/usr/bin/env bash
# Dev: portal HMR on :8000 + server hot reload (air). Ctrl-C stops both.
set -e
cd "$(dirname "$0")"
trap 'kill 0' INT TERM EXIT
[ -d portal/node_modules ] || (cd portal && npm ci)
(cd server && go run github.com/air-verse/air@latest -tmp_dir /tmp/detur-air \
  -build.cmd "go build -o /tmp/detur-air/detur ./cmd/detur" -build.bin /tmp/detur-air/detur) &
# portal waits for the API (:8081) so its first proxied requests don't race the server build
(until (: >/dev/tcp/127.0.0.1/8081) 2>/dev/null; do sleep 0.3; done
  cd portal && npm run dev -- --port 8000 --strictPort) &
wait
