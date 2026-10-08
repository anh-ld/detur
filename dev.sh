#!/usr/bin/env bash
# Dev: portal HMR on :8000 + server hot reload (air). Ctrl-C stops all.
# Usage: ./dev.sh [server|portal]...   (no args = both)
set -e
cd "$(dirname "$0")"

# .env vars exported to every process; admin gating on by default (password "admin")
load_env() {
  set -a
  [ ! -f .env ] || . ./.env
  : "${ADMIN_PASSWORD:=admin}"
  set +a
}

# air pinned: same version on every machine, no surprise upgrades from @latest
server() {
  cd server
  go run github.com/air-verse/air@v1.67.4 -tmp_dir /tmp/detur-air \
    -build.cmd "go build -o /tmp/detur-air/detur ./cmd/detur" -build.bin /tmp/detur-air/detur
}

# waits for the API (:8081) so its first proxied requests don't race the server build
portal() {
  cd portal
  [ -d node_modules ] || npm ci
  until (: >/dev/tcp/127.0.0.1/8081) 2>/dev/null; do sleep 0.3; done
  npm run dev -- --port 8000 --strictPort
}

[ $# -gt 0 ] || set -- server portal
for svc; do
  case $svc in
    server | portal) ;;
    *) echo "usage: $0 [server|portal]..." >&2; exit 1 ;;
  esac
done

load_env
trap 'kill 0' INT TERM EXIT
for svc; do "$svc" & done
wait
