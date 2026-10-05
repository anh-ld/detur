# Caveats

Decisions and their sources: [DECISIONS.md](DECISIONS.md).

Known limits and trust boundaries. Read before running in production.

## Security

- Portal has no authentication. Whoever reaches it controls every app and
  link. Only protection: loopback or a zero-trust gateway.
- Log out (`LOGOUT_URL`) only links to the gateway's sign-out. The identity
  provider may still be signed in and log the user right back in.
  Tailscale-style device trust has no session to end.
- No TLS enforcement. Over plain http, the SDK's bearer key and device
  fingerprints travel in the clear. HTTPS is the operator's job.
- API keys hashed with plain, unsalted SHA-256, not a slow KDF. Safe only
  because keys are ~131-bit random values.
- `TRUST_PROXY=1`: trusts rightmost `X-Forwarded-For` entry (the peer the
  proxy saw), else `X-Real-IP`. Set only behind a real proxy; otherwise a
  direct client spoofs the IP match signal (500 points).

## Matching and attribution

- Identical devices share a fingerprint. Their organic installs collapse into
  one row, so organic installs are undercounted.
- Deterministic matches limited to `RETENTION_HOURS` (default 24h). Install
  later than that loses the deferred link.
- Threshold and window are per app (Detour). Upgrade copies the old global
  values into every app; per-link overrides are dropped.
- A matched click is used up (Detour). Two devices behind one IP can't both
  claim one click; the second counts organic.
- Same device reinstalling within `RETENTION_HOURS` gets its earlier matched
  link back, even after tapping a newer link.
- Window measured from the SDK's capture timestamp when it is within 180 min
  of server time.
- One-hop interstitial captures screen, timezone, device model + OS version
  (client hints). iOS matching reaches 1350 (IP, OS, timezone, screen,
  language).
- iOS links with an App Store target get a tap-to-copy page (one extra tap).
  Copies the short link so the pasteboard signal (350/175) can fire. Lost if
  the user denies iOS's paste prompt. Unverified on device that the SDK sends
  `pastedLink`.
- Bot filter: Dub's UA_BOTS list (HEAD + `?bot=` count as bots), with the
  "Google/google" webview exception.
- Clicks deduped per link + device (IP + user agent) within an hour, like
  Dub's click cache. Re-tap refreshes the click (time, signals) and re-enters
  the match window.
- Stored click destination = link URL + forwarded params (what match-link
  returns), not the store URL it redirected to. Same as Dub.
- Short-link keys case-insensitive.

## Operations and scale

- SQLite: one writer at a time. Fine for personal or team use, not high
  concurrency.
- No graceful shutdown. Hard exit kills in-flight requests; WAL keeps stored
  data safe.
- Schema not versioned in v1; `Open()` adds new columns in place. Back up
  before upgrading.
- No rate limiting. A click flood grows the clicks table; hourly purge trims
  it to the retention period.
- Analytics days are UTC. Clicks recorded before the analytics rollups
  existed are not counted; installs before then have no link or platform.
  Deleting a link drops its click counts. Event names in analytics are cut
  to 64 characters and kept forever: don't put user or device ids in them.
- Installs table never purged. Attribution history grows without limit, by
  design.
- Single operator, no organizations or teams. All apps share one instance.

## Compatibility

- Platform API, webhooks, billing: not implemented. godetour users who need
  them get nothing here.
- Single-segment short links only. Multi-segment `/app-hash/slug` returns
  404; universal-link-click records no click for it.
- `effectiveLimit: -1`: this server's own encoding for "no limit". Harmless
  with SDK 2.3.1, not a documented godetour value.
- SDK contract has no stability guarantee. Client patch is per-version.
- Example app never run on a real device. Patch verified to apply, not
  exercised on a device.
