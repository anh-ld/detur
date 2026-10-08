# Decisions

Every decision + source.

## Source rule

1. **Detour docs** ([matching](https://detour.swmansion.com/docs/platform/architecture/matching/), [click handling](https://detour.swmansion.com/docs/platform/architecture/click-handling/), [limitations](https://detour.swmansion.com/docs/platform/architecture/architecture-limitations/)) win. Closed server; docs = spec.
2. **Dub** ([dubinc/dub](https://github.com/dubinc/dub)) fills gaps. Analytics funnel, not deferred matching.
3. **Ours**: neither covers, or conflicts with self-hosting. Reason required.

Change: edit row, keep ID. New conflict: add row before code.

## Matching

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| M1 | Weights: IP 500, model+version 450, iOS version 350, UA signature 350, pasteboard 350/175, timezone 200, screen 200 (±1, ±0.01 scale), language 100. One device signal per candidate. Max 1700 iOS / 1450 Android. | Detour | Documented. | — | in place |
| M2 | Threshold 850 (700–1200), window 15 min (5–180), per app. No global default, no per-link override. | Detour | "App-level settings." Upgrade seeds old global into each app. | 005 | in place |
| M3 | Matched click consumed: 1 click, 1 install. | Detour | Candidates = "unmatched clicks". Dub doesn't consume. | 006 | in place |
| M4 | Same device retry → same earlier click. | Ours | SDK retries idempotent. Detour silent. | 006 | in place |
| M5 | Deterministic `clickId`: exact, unmatched, no window, lives `CLICK_ID_DAYS` (30, 1–90). Fingerprint scrubbed at `RETENTION_HOURS`; matched clicks deleted then. | Detour + ours | Play referrer returns days later. 90 = its limit. No extra PII. | — | in place |
| M6 | Window from SDK `timestamp`; ignored if >180 min off or future. | Detour + ours | Clamp ours: clock drift. | 007 | in place |
| M7 | iOS 26 frozen UA (`OS 18_6`): use Safari `Version/` major if higher. | Detour | Documented workaround. | 007 | in place |
| M8 | UA signature scored Android only. | Detour | Weights table. | 007 | in place |
| M9 | Pasteboard: install's pasted URL vs clicked short link; click `pasted_link` stored only if it matches. | Detour + ours | Server check ours: query param forgeable. | 007 | in place |
| M10 | Tie → newer click. | Detour | Documented. | — | in place |
| M11 | No match → organic + 404. Backend error → 404 + hidden `unknown` install, replaced by later real result. | Detour + ours | Fail-open ours: never block app. | 003 | in place |
| M12 | Device hash = SHA-256 of fingerprint, no timestamp. | Ours | Stable key, idempotent installs. | — | in place |
| M13 | Install receipt: method (`click_id`, `probabilistic`, `prior`, `organic`, `unknown`), best score, runner-up. Organic keeps best below-threshold score. Settings: method mix, histogram, threshold what-if. | Ours | Threshold set blind otherwise. | — | in place |

## Click capture

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| C1 | Mobile first hop: 1-reload interstitial (screen, timezone); `Accept-CH` for model + OS. | Dub | Dub deeplink preview. Detour fingerprints immediately. | — | in place |
| C2 | iOS + App Store → tap-to-copy page (feeds M9). Always on. | Detour + ours | Detour's per-app toggle skipped (YAGNI). | 008 | in place |
| C3 | Bots: Dub `UA_BOTS`, HEAD + `?bot=`, Google webview exempt. | Dub | Detour: no list. | — | in place |
| C4 | Dedup: link + IP + UA per hour; re-tap refreshes time/signals, keeps id + matched. | Dub | Dub click cache. | — | in place |
| C5 | Clicks kept `RETENTION_HOURS` (24–8760), purged hourly. | Ours | Bounded storage; deterministic needs ≥24h. | 005 | in place |
| C6 | Delete link → its clicks. Delete app → clicks, installs, events. | Ours | Orphans matched stale destinations. | 004 | in place |
| C7 | Click `tz` / `screen` stored only if well-formed, short. | Ours | Unvalidated input fed scoring. | 007 | in place |

## Redirects

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| R1 | App Store URL: only `pt`, `ct`, `mt`, UUID `ppid`. | Detour | Store keys only. | 008 | in place |
| R2 | Play Store: `utm_*` + `click_id` inside `referrer`. No top-level params. | Detour | Blocks `?id=other.app` hijack. | 008 | in place |
| R3 | Other targets + app destination: all non-empty params, add-only, URL not re-encoded. | Detour + ours | Add-only ours: operator keys win. Dub overwrites. | 008 | in place |
| R4 | Store/fallback targets per link. | Dub | Detour: per app. Per-link more flexible. | — | in place |
| R5 | Links: one path segment. | Ours (divergence) | Detour allows more. See CAVEATS. | — | in place |
| R6 | Targets absolute; `javascript:`, `data:`, `vbscript:`, `file:` rejected; app schemes + `market:` OK. | Ours | Typos → relative redirects. | 003 | in place |
| R7 | Expired → `expiredUrl`, else 410. | Dub | Dub expiry. | — | in place |

## Analytics

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| A1 | Overview: clicks, installed opens, installs via link + match %, web fallbacks, daily chart, organic vs non-organic, per-link clicks/matches, top events. Filters: platform, 7/30/90d. | Detour | Same page. Retention not built yet (events do carry `device_id`). | — | in place |
| A2 | Rollups `click_days`, `event_days`: counts only, never purged. | Ours | Raw data purged at 24h; 90d chart needs counts, no device data. | — | in place |
| A3 | Web fallback = browser click → non-store URL. Installed open = universal-link-click. | Ours | Detour undefined; what detur sees. | — | in place |
| A4 | UTC days. | Ours | One bucket key. | — | in place |

## Fraud

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| F1 | Server-only. No SDK change. | Ours | Least scope. | — | in place |
| F2 | Signals tagged first; active = per-app opt-in. App Fraud tab: press = saved, toast, no confirm. | Singular + ours | Nothing excluded unseen. Confirm dialog dropped: one press, undo = one press. | — | in place |
| F3 | Active: skip flagged click → best other ≥ threshold, else organic. Flagged `clickId` → organic. Install IP: own `install_ip` label, tag only. | Ours | M2 selection. No fingerprint on `clickId`. | — | in place |
| F4 | Never delete installs; labels survive scrub. | Ours | M11. | — | in place |
| F5 | Velocity: raw hits per IP, link. Own counter. | Ours | C4 merges, C5 scrubs. | — | in place |
| F6 | Click→install time: short = hijack; long = flood, `clickId` only. | AppsFlyer | M2 window caps probabilistic. | — | in place |
| F7 | UA: C3 as is. Extra bot list + empty UA = tagged signal, out of A2. | Ours | C3 drops unseen. | — | in place |
| F8 | IP: hosting ASNs (ipverse), bundled. Akamai/Cloudflare/Fastly out; Private Relay subtracted at refresh. | ipverse + ours | No outbound. Relay = real users. Apple list not redistributable. | — | in place |
| F9 | Same hash, many installs, one link, probabilistic only. | Ours | M12 = config, not device. | — | in place |

## Ops

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| O1 | Settings Health: SDK last seen (`X-SDK`), app detail formats, AASA refresh (>1 iOS app, links <48h), proxy (private peers with `TRUST_PROXY=0`). No outbound checks. | Ours | Setup mistakes fail silently. | — | in place |

## SDK API

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| S1 | Same 5 endpoints + shapes as godetour.dev; `effectiveLimit: -1`. | Ours | Drop-in SDK 2.3.x; no limits. | — | in place |
| S2 | universal-link-click auth backend error → allow + fresh `clickId`, no write. | Ours | Never block; never write unverified. | 004 | in place |

## Portal access

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| P1 | Gateway admits everyone as viewer (links + monitoring). Admin actions need `ADMIN_PASSWORD` → 12h per-browser signed session. No user table, no RBAC. | Ours | Single-operator self-hosted; each team brings its own auth; shared password beats per-person roles at this scale. | 009 | in place |

## Webhooks

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| W1 | Cursor-based background delivery: periodic worker drains SQLite sequential `rowid` cursors. No outbox table, zero overhead on redirects and SDK match endpoints. | Ours | Eliminates redirect latency and external failure risk. Natural replay. | feat-webhooks | in place |
| W2 | Granular event subscriptions: per-endpoint opt-in to `installs`, `events`, and/or `clicks`. | Ours | High-value installs without high-volume click floods. | feat-webhooks | in place |
| W3 | HMAC-SHA256 signature (`Detur-Signature: t=<unix>,v1=<hex>`) computed over `t.<unix>.<raw_body>` with endpoint secret. Replay requests carry `Detur-Replay: true`. | Stripe-style + ours | Standard consumer signature verification with anti-tampering and timestamp replay defense. | feat-webhooks | in place |
| W4 | SSRF dial-time defense: blocks private networks (RFC 1918, RFC 4193), link-local, and cloud metadata (169.254.169.254); loopback HTTP allowed for dev; redirect following disabled. | Ours | Protects cloud hosting environments against SSRF and DNS rebinding. | feat-webhooks | in place |
| W5 | Independent stream backoff: exponential backoff (`min(30s * 2^fails, 1h)`) tracked per `(endpoint, event_type)`. Clicks failing 500 does not block install 200 delivery. | Ours | Failure isolation across high-volume and critical streams. | feat-webhooks | in place |
| W6 | Admin-gated management: requires `ADMIN_PASSWORD` elevation; fails closed (403 Forbidden) if `ADMIN_PASSWORD` is unset. | Ours | Protects secret generation, endpoint creation, and replay controls. | feat-webhooks | in place |
| W7 | Outbound stance: zero outbound network calls by default; outbound HTTP only when webhooks are configured and enabled. | Ours | Preserves self-contained offline capability by default. | feat-webhooks | in place |

## Not adopted (yet)

In-app browser hand-off, return-to-web after store dismiss, App Preview page, custom redirect HTML, fallback param strategies, copy-link toggle, Smart Banners, multi-segment links, Platform API, billing, click-injection detection (needs SDK referrer), VPN/Tor IP detection.

## Security, ops

Trust boundaries: [CAVEATS.md](CAVEATS.md). Log out → `LOGOUT_URL` (gateway sign-out). Admin elevation: `ADMIN_PASSWORD` → 12h browser session (P1). Unset = no gating. Webhooks require `ADMIN_PASSWORD` (W6).
