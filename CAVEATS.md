# Caveats

These are the known limits and trust boundaries. Read them before running
detur in production.

## Security

- The portal has no authentication. Anyone who can reach it controls every
  app and link, so keeping it on loopback or behind a zero-trust gateway is
  the only protection.
- The server does not enforce TLS. Over plain http, the SDK's bearer key and
  the device fingerprints travel in the clear. Serving HTTPS is up to the
  operator.
- API keys are hashed with plain, unsalted SHA-256 rather than a slow KDF.
  This is safe only because the keys are 128-bit random values.
- `TRUST_PROXY=1` makes the server trust `X-Forwarded-For`. If it is set
  without a real proxy in front, clients can spoof the IP match signal, which
  is worth 500 points.

## Matching and attribution

- Identical devices share a fingerprint. A group of identical devices
  collapses its organic installs into one row, so organic installs are
  undercounted.
- Deterministic matches are limited to the 24h retention. An install more than
  24h after the tap loses the deferred link.
- v1 has no iOS copy-link or interstitial page, so the pasteboard signal
  (350/175 points) rarely fires. iOS matching relies on IP, OS, timezone,
  screen, and language, which add up to at most 1350. That is still above the
  default threshold of 850.
- The bot filter is a short list of user-agent markers. The `preview` marker
  can drop traffic that looks real.

## Operations and scale

- SQLite allows one writer at a time. That is enough for personal or team
  use, but not for high concurrency.
- There is no graceful shutdown. A hard exit kills in-flight requests; WAL
  keeps the stored data safe.
- The schema is not versioned in v1. Back up the database before upgrading.
- There is no rate limiting. A flood of unauthenticated clicks grows the
  clicks table, and the hourly purge limits it to the retention period.
- The installs table is never purged, so attribution history grows without
  limit. This is by design.
- There is a single operator and no organizations or teams. All apps share
  one instance.
- The Docker image is not published yet, so build it from source.

## Compatibility

- The platform API, webhooks, and billing are not implemented. godetour users
  who depend on those features get nothing here.
- Only single-segment short links work. The multi-segment `/app-hash/slug`
  form returns 404, and universal-link-click does not record a click for it.
- `effectiveLimit: -1` is this server's own encoding for "no limit". It was
  checked against SDK 2.3.1 and is harmless there, but it is not a documented
  godetour value.
- The SDK contract has no stability guarantee, so the client patch is
  per-version.
- The example app has never run on a real device. The patch was verified to
  apply, not exercised on a device.
