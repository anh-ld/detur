#!/bin/sh
# CI gate. Plain run covers everything; -race only where goroutines are tested.
# -race slows the pure-Go SQLite driver ~16x (store: 4s -> 65s), so the full suite under it takes ~3 min.
set -eu
cd "$(dirname "$0")/.."
go vet ./...
go test ./... &
plain=$!
go test -race \
	-run 'TestElevateLockPerPeer|TestS_INFRA_2[345]_|TestS_PORTAL_30_' ./internal/api ./internal/scenarios &
race_sqlite=$!
go test -race ./internal/webhook ./internal/httpx ./internal/ua ./internal/page ./internal/fraud/... ./internal/config &
race_pure=$!
fail=0
for p in $plain $race_sqlite $race_pure; do wait "$p" || fail=1; done
exit $fail
