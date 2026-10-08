# Caveats

Read before prod. Sources: [DECISIONS.md](DECISIONS.md).

## Security

- Portal: no auth. Reach = links + stats; `ADMIN_PASSWORD` unset = full control. Protect: loopback or zero-trust gateway.
- Admin cookie = password-equivalent (offline-crackable). Long random password + gateway TLS. Rotate password = sign everyone out.
- Wrong-password delay serialized per peer. Behind one gateway IP: a guess burst queues every login.
- `LOGOUT_URL`: link only. IdP may re-login instantly. Tailscale: nothing to end.
- No TLS enforcement. Plain http leaks API key + fingerprints. HTTPS = operator.
- API keys: unsalted SHA-256. OK only because keys ~131-bit random.
- `TRUST_PROXY=1` without real proxy → client spoofs IP signal (500 pts).

## Matching

- Identical devices = one fingerprint → organic installs undercounted.
- Click ID match: `CLICK_ID_DAYS` max (30 default). Fingerprint gone at `RETENTION_HOURS`;
  late click-ID installs get no platform from the click.
- Threshold/window per app. Upgrade copies old global values; per-link overrides dropped.
- Matched click consumed. 2 devices, 1 IP, 1 click → second = organic.
- Reinstall within `RETENTION_HOURS` → earlier link back, even after newer tap.
- Window from SDK timestamp if within 180 min of server time.
- Interstitial (1 hop): screen, timezone, model + OS (client hints). iOS max 1350.
- iOS + App Store target: tap-to-copy page, +1 tap. Feeds pasteboard (350/175).
  Lost if paste denied. SDK sending `pastedLink`: unverified on device.
- Bots: Dub `UA_BOTS`, HEAD + `?bot=`, Google webview exempt.
- Dedup: link + IP + UA per hour. Re-tap refreshes time/signals, re-enters window.
- Stored destination = link URL + params, not store URL. Like Dub.
- Keys case-insensitive.

## Fraud

- Signals tagged by default. Active: per app, per signal (app Fraud tab). Saves on press, no confirm.
- Active excludes click → user gets organic 404, no deferred deep link.
- IP ranges bundled per release. Pinned version → stale ranges.
- CGNAT / office NAT: many users, one IP → velocity may flag. Watch flagged installs before going active.
- Private/loopback IPs exempt. `TRUST_PROXY=0` behind proxy → IP signals blind.
- UA-suspect clicks left out of click counts, even when tagged.
- New thresholds apply to future matches. Past installs not re-scored.
- Repeated device: same profile within `RETENTION_HOURS` reuses the earlier match, so it counts only after that click expires.
- No click injection, device reset, emulator, or receipt checks.

## Ops

- SQLite: one writer. Personal/team scale.
- No graceful shutdown. WAL keeps data safe.
- No schema versions. `Open()` adds columns. Back up before upgrade.
- No rate limit. Click flood → table grows; unmatched rows kept `CLICK_ID_DAYS` (no PII).
- Analytics: UTC days. Pre-rollup clicks uncounted; pre-rollup installs lack
  link/platform. Link delete drops its counts.
- Event names: cut to 64 chars, kept forever. No user/device ids in them.
- Installs never purged, by design.
- Single operator. No orgs/teams.
- Health proxy check: counts since start, resets on restart.

## Webhooks

- Outbound calls: Detur makes outbound HTTP calls only when webhooks are explicitly configured and enabled.
- SSRF dial-time defense: Outbound HTTP requests block private networks (RFC 1918, RFC 4193), link-local addresses, and cloud instance metadata (169.254.169.254); redirects are never followed.
- Secrets at rest: Webhook signing secrets are stored retrievably in SQLite (`detur.db`) so admins can view and rotate them in the portal. Ensure database file permissions are restricted (`chmod 600 detur.db`).
- Replay & retention reality: Only `installs` are permanent. Raw `clicks` and `events` are purged after `RETENTION_HOURS` (24h default); a replay reaching past that window starts at the oldest retained click/event. If a purge empties a table, new rows reuse low rowids; the worker detects this and rewinds the stream cursor to 0, so the receiver may see duplicates (never silent loss); dedupe on record `id`.
- Consumer signature verification: Consumers must use constant-time comparisons (`crypto/subtle.ConstantTimeCompare`) and enforce a timestamp tolerance window (e.g. 5 minutes) to protect against replay attacks.

## Compatibility

- No Platform API, billing.
- Single-segment links only. `/app-hash/slug` → 404, no click recorded.
- `effectiveLimit: -1` = "no limit", ours. Fine with SDK 2.3.1.
- SDK contract unstable. Patch per version.
- Example app: never run on device. Patch: applies, untested on device.
