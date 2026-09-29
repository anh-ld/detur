# Caveats

Known limits and trust boundaries. Read them before running detur in
production.

## Security

- The portal has no authentication. Anyone who reaches it controls every app
  and link. Only protection: loopback or a zero-trust gateway.
- No TLS enforcement. Over plain http, the SDK's bearer key and the device
  fingerprints travel in the clear. HTTPS is the operator's job.
- API keys are hashed with plain, unsalted SHA-256, not a slow KDF. Safe only
  because the keys are 128-bit random values.
- `TRUST_PROXY=1`: trusts `X-Real-IP`, else the rightmost `X-Forwarded-For`
  entry (the peer the proxy saw). Set only with a real proxy in front — a
  direct client can still spoof the IP match signal (500 points).

## Matching and attribution

- Identical devices share a fingerprint. A group of identical devices
  collapses its organic installs into one row, so organic installs are
  undercounted.
- Deterministic matches are limited to the 24h retention. An install more than
  24h after the tap loses the deferred link.
- One-hop interstitial captures screen, timezone, device model + OS version
  (client hints): iOS matching reaches 1350 (IP, OS, timezone, screen,
  language). No iOS copy-link page yet — pasteboard signal (350/175) rarely
  fires.
- Bot filter: Dub's UA_BOTS list (HEAD + `?bot=` count as bots), with the
  "Google/google" webview exception. Clicks deduped per link + device (IP +
  user agent) within an hour, like Dub's click cache.
- Stored click's destination = final redirect target (platform URL +
  forwarded params; Play targets add the clickId referrer), not the link's
  base URL. Short-link keys case-insensitive.

## Operations and scale

- SQLite allows one writer at a time. Fine for personal or team use, not for
  high concurrency.
- No graceful shutdown. A hard exit kills in-flight requests; WAL keeps the
  stored data safe.
- Schema not versioned in v1; `Open()` adds new columns in place. Back up
  before upgrading.
- No rate limiting. A flood of unauthenticated clicks grows the clicks table;
  the hourly purge keeps it to the retention period.
- The installs table is never purged. Attribution history grows without
  limit. By design.
- Single operator, no organizations or teams. All apps share one instance.
- The Docker image is not published yet — build from source.

## Compatibility

- Platform API, webhooks, billing: not implemented. godetour users who depend
  on them get nothing here.
- Only single-segment short links work. The multi-segment `/app-hash/slug`
  form returns 404, and universal-link-click records no click for it.
- `effectiveLimit: -1` is this server's own encoding for "no limit". Checked
  against SDK 2.3.1, harmless there, but not a documented godetour value.
- The SDK contract has no stability guarantee. The client patch is
  per-version.
- The example app has never run on a real device. The patch was verified to
  apply, not exercised on a device.