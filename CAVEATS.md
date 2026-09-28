# Caveats

Known limits and trust boundaries. Read before production.

## Security

- Portal has NO auth — whoever reaches it controls everything; loopback /
  zero-trust boundary is the only gate
- TLS not enforced by the server — plain http leaks the SDK bearer key +
  fingerprints; HTTPS is the operator's job
- API keys hashed with plain SHA-256 (unsalted) — safe only because keys are
  128-bit random; no slow KDF
- `TRUST_PROXY=1` is a trust flag — set without a real proxy in front and
  the IP match signal (500 weight) is spoofable

## Matching & attribution

- Identical devices share a fingerprint — a homogeneous device cohort
  collapses organic installs into one row (undercounts)
- Deterministic match capped at 24h retention — installs >24h after the tap
  lose the deferred link
- No iOS copy-link/interstitial page in v1 — the pasteboard signal
  (350/175 weight) rarely fires; iOS matching rests on IP/OS/timezone/
  screen/language (max 1350, still ≥ 850 default)
- Bot filter is a cheap UA-marker list — the `preview` marker can drop
  real-looking traffic

## Ops & scale

- SQLite single-writer — fine for personal/team scale, not high concurrency
- No graceful shutdown — hard exit kills in-flight requests (WAL keeps data
  safe)
- Schema not versioned in v1 — back up before upgrades
- No rate limiting — an unauthenticated click flood grows the clicks table
  (hourly purge bounds it at retention)
- Installs table never purged — attribution history grows forever (by
  design)
- Single operator model — no orgs/teams; all apps share one instance
- Docker image not published yet — build from source

## Compatibility

- Platform API, webhooks, billing deferred — godetour users on those
  features get nothing here
- Single-segment short links only — `/app-hash/slug` multi-segment form
  404s; universal-link-click doesn't record for it
- `effectiveLimit: -1` is our own no-limit encoding — verified harmless
  against SDK 2.3.1, not a documented godetour value
- SDK contract has no stability statement — the client patch is per-version
- Example app never run on a real device — the patch is apply-verified, not
  device-exercised