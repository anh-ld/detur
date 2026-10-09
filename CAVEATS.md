# Caveats

Operational gotchas, architectural limits, and security constraints to read before deploying to production. For design rationales, see [DECISIONS.md](DECISIONS.md).

## Security

- **Portal network access**: The portal has no user-level authentication. Anyone who can reach `:8081` can view links and analytics. If `ADMIN_PASSWORD` is unset, anyone can create, edit, or delete apps. Run on loopback or gate behind a zero-trust proxy (Cloudflare Access, Tailscale).
- **Admin session cookie**: The admin session cookie is directly derived from the password (offline-crackable if intercepted). Use a long, random password and enforce HTTPS. Changing `ADMIN_PASSWORD` immediately invalidates all active sessions.
- **Login brute-force delays**: Failed login attempts introduce a serialized delay per remote IP. If running behind a reverse proxy with `TRUST_PROXY=0`, all users share the gateway IP; a brute-force attempt will queue and delay logins for everyone.
- **Logout behavior (`LOGOUT_URL`)**: The "Log out" button only redirects to `LOGOUT_URL`. If your identity provider re-authenticates automatically (or if using Tailscale), sessions do not truly terminate on click.
- **TLS termination**: Detur does not enforce TLS internally. Running over plain HTTP leaks SDK API keys and device fingerprints. Always terminate HTTPS at a reverse proxy or load balancer.
- **API key storage**: Stored as unsalted SHA-256 hashes. Safe only because generated keys possess ~131 bits of entropy.
- **Proxy trust risk**: Setting `TRUST_PROXY=1` without an actual reverse proxy stripping upstream headers allows clients to spoof `X-Forwarded-For` and game the 500-point IP matching score.

## Matching

- **Identical hardware fingerprints**: Two identical devices (same model, OS version, screen resolution, timezone) on the same Wi-Fi share a single fingerprint; this can lead to undercounting non-organic installs or misattributing candidates.
- **Single-use clicks**: Matched clicks are consumed immediately. If two users install from the same Wi-Fi after a single shared link click, only the first user receives the deferred deep link; the second is recorded as organic.
- **Late Play referrer (`clickId`)**: `clickId` matches are supported for up to `CLICK_ID_DAYS` (default 30 days). However, the raw click fingerprint is purged after `RETENTION_HOURS` (default 24h), so installs matching late `clickId`s lose original click metadata (such as platform).
- **Matching thresholds & windows**: Configured per app (threshold default 850, window default 15 min). There are no per-link threshold overrides.
- **Reinstalls within retention**: A user reinstalling the app within `RETENTION_HOURS` receives their earlier matched link again, even if they tapped a newer link in between.
- **Device clock drift**: The matching window uses the SDK client timestamp only if it is within 180 minutes of server time; otherwise, it clamps to server time.
- **One-hop interstitial**: Mobile browsers reload through an interstitial to capture screen, timezone, and client hints (model + OS). Maximum probabilistic score on iOS is 1350 without pasteboard.
- **iOS App Store pasteboard**: Redirecting to the App Store shows a tap-to-copy page to feed pasteboard matching (350 points exact / 175 points domain). If the user denies pasteboard permission in iOS, the signal is lost. Note: The SDK's `pastedLink` is checked server-side against the clicked link to prevent tampering.
- **In-app browser escape (iOS)**: In-app webviews (Messenger, TikTok, Zalo) show an interstitial "Get the app" tap-page to escape sandbox restrictions. If the app is already installed on iOS, it can only open to the home screen rather than the deep link due to iOS webview sandbox limitations.
- **In-app browser escape (Android)**: Android deep links can launch the installed app directly via `intent://` only if the package name and verified App Links certificates match; otherwise, it falls back to Google Play.
- **Click deduplication**: Clicks from the same link, IP, and User-Agent are deduplicated within a 1-hour window. A re-tap refreshes the timestamp and signals, re-opening the match window.
- **In-app to real browser merge**: If a user taps "Open in browser" from an in-app webview within 1 hour, the new browser click merges with the in-app click. On shared IPs (offices, cellular CGNAT), two different users could theoretically be merged into one.
- **In-app source detection**: X and Telegram typically use Safari View Controller / Chrome Custom Tabs with standard browser User-Agents, making source detection rely on the delayed safety-net fallback.
- **Tap page locale**: The in-app tap page is currently in English only. Append `?detur-no-track=1` during development/testing to bypass it.

## Fraud

- **Tag-only vs. Active mode**: Fraud signals are in "Tagged" mode by default (monitoring only). Setting a signal to "Active" is per-app in the Fraud tab and saves immediately without confirmation.
- **Impact of Active signals**: In Active mode, flagged clicks are rejected from matching. The installing user will receive an organic 404 response with no deferred deep link.
- **Bundled datacenter IP lists**: Datacenter and hosting ASN ranges are compiled into each release. Pinned binary/Docker versions will have increasingly stale IP ranges over time.
- **Shared IPs & CGNAT velocity**: Cellular CGNAT and corporate NATs share one public IP across thousands of devices. Rapid installs may trip velocity thresholds. Review flagged installs in the portal before turning velocity checks to Active.
- **Private & proxy IPs**: Loopback and private IPs are always exempt. If running behind a reverse proxy with `TRUST_PROXY=0`, all clients share the proxy's IP and IP-based fraud signals will be blinded.
- **Bot clicks in analytics**: Clicks identified with suspicious or bot User-Agents are excluded from analytics click counts, even in Tagged mode.
- **Threshold updates**: Changing fraud thresholds or rules applies only to future match requests; historical installs are never retroactively re-scored.
- **Repeat device velocity**: When the same device profile reappears within `RETENTION_HOURS`, it reuses the earlier match instead of generating a new install. It can only be flagged after the original click expires.
- **Unsupported fraud checks**: Detur does not detect click injection, device resets, emulator environments, or store purchase receipts (see [DECISIONS.md](DECISIONS.md)).

## Ops

- **Database concurrency**: Uses SQLite in WAL mode with a single writer. Built for personal, team, or medium product scale; not horizontally scalable.
- **Shutdown & upgrades**: No graceful drain on shutdown; SQLite WAL ensures crash-safety. Schema migrations run on `Open()` by adding columns. Always back up `detur.db` before upgrading versions.
- **Storage growth on floods**: Public endpoints have no rate limits. Unmatched clicks accumulate until purged by `RETENTION_HOURS` (or `CLICK_ID_DAYS` for click IDs).
- **Analytics aggregation**: All rollups operate on UTC days. Deleting a link permanently removes its associated click and event rollup counts.
- **Event names**: Custom event names are truncated to 64 characters and retained indefinitely. Do not place PII, user IDs, or device IDs in event names.
- **Per-link conversions & retention**: Requires devices to be tagged by an event containing `data.link`. On Detour SDK 2.3.1, the client app only knows the link key if "Tag deferred links" is enabled in app settings.
- **Retention cohorts**: Cohort assignment begins on the device's tag day (not install day). D1/D7/D30 metrics record cold starts (`app_open`) on that exact UTC day; background/warm resumes are not counted unless the app explicitly invokes `logRetention`.
- **Retention tag lifecycle**: Device tag mappings expire after 90 days. If the same device is re-tagged later, it enters a new cohort and can be attributed to a different link.
- **Retention filtering**: Per-link conversion and retention metrics are aggregated across all devices and ignore the portal's platform filter.
- **Permanent records**: Installs and daily aggregated rollups are never purged by design.
- **Multi-tenancy**: Designed for a single operator/team; there is no role-based access control (RBAC) or organization isolation.

## Webhooks

- **Outbound network calls**: Detur only initiates outbound HTTP requests when webhooks are explicitly configured and enabled.
- **Dial-time SSRF protection**: Webhook delivery blocks private networks (RFC 1918, RFC 4193), link-local ranges, and cloud metadata services (`169.254.169.254`). HTTP redirects are never followed.
- **Secrets at rest**: Webhook signing secrets are stored in plaintext in SQLite (`detur.db`) so admins can view and rotate them in the portal. Ensure database file permissions are restricted (`chmod 600 detur.db`).
- **Replay & retention limits**: Only `installs` are retained permanently. Raw `clicks` and `events` are purged after `RETENTION_HOURS` (default 24h); webhook replaying past that window starts at the oldest retained record. If a purge empties a table, new rows reuse low rowids, which causes the worker to rewind the cursor to 0; receivers may receive duplicates, so always deduplicate on record `id`.
- **Consumer signature verification**: Webhook consumers must verify signatures with constant-time comparison (`crypto/subtle.ConstantTimeCompare`) and enforce a timestamp tolerance window (e.g., 5 minutes) to defend against replay attacks.

## Compatibility

- **Single-segment link paths**: Only single-segment slugs (e.g., `links.example.com/promo`) are supported. Multi-segment paths like `/app/promo` will return 404 without recording clicks.
- **No billing or platform API**: Detur is purely self-hosted. There is no billing engine, tier enforcement, or upstream godetour platform API.
- **SDK limits response**: Returns `effectiveLimit: -1` to signal unlimited clicks to `@swmansion/react-native-detour` (compatible with SDK 2.3.x).
- **SDK patch stability**: Because Detour SDK hardcodes its backend URLs to `godetour.dev`, patch files must be re-generated when upgrading SDK versions.
- **Example app testing**: The included example app in `example/expo-app/` demonstrates configuration but has not been tested on physical hardware.
