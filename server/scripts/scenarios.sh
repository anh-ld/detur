#!/usr/bin/env bash
# Scores the real-world usage scenarios (internal/scenarios) and guards the existing suite.
# Prints one JSON object: scenario_pass_rate (primary), gates, per-case results, worst cases.
set -uo pipefail
cd "$(dirname "$0")/.."

build_ok=1
go vet ./... >/dev/null 2>&1 || build_ok=0

existing_ok=1
pkgs=$(go list ./internal/... | grep -v /internal/scenarios)
go test -count=1 $pkgs >/tmp/detur-existing.log 2>&1 || existing_ok=0

go test -count=1 -json ./internal/scenarios 2>/dev/null >/tmp/detur-scenarios.json

python3 -I - "$build_ok" "$existing_ok" /tmp/detur-scenarios.json <<'PY'
import json, sys
build_ok, existing_ok, path = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
cases, out = {}, {}
for line in open(path):
    try:
        ev = json.loads(line)
    except ValueError:
        continue
    t = ev.get("Test")
    if not t or "/" in t or not t.startswith("TestS_"):
        continue
    if ev.get("Action") == "output":
        out.setdefault(t, []).append(ev.get("Output", ""))
    if ev.get("Action") in ("pass", "fail", "skip"):
        cases[t] = ev["Action"] == "pass"
total = len(cases)
passed = sum(cases.values())
worst = []
for t, ok in sorted(cases.items()):
    if not ok:
        msg = "".join(l for l in out.get(t, []) if not l.startswith(("=== ", "--- ")) and l.strip())
        worst.append({"case": t, "why": msg.strip()[-400:]})
print(json.dumps({
    "scenario_pass_rate": round(passed / total, 4) if total else 0,
    "scenarios_passed": passed,
    "scenarios_failed": total - passed,
    "scenarios_total": total,
    "build_ok": build_ok,
    "existing_tests_pass": existing_ok,
    "cases": cases,
    "worst_cases": worst[:15],
}))
PY
