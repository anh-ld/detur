# Decisions

Every technical decision in detur, where it came from, and why.

## Source rule

detur reimplements the hosted Detour backend. Two references, ranked:

1. **Detour docs** ([matching](https://detour.swmansion.com/docs/platform/architecture/matching/), [click handling](https://detour.swmansion.com/docs/platform/architecture/click-handling/), [limitations](https://detour.swmansion.com/docs/platform/architecture/architecture-limitations/)) win for anything they document. Server is closed; docs are the spec.
2. **Dub** ([dubinc/dub](https://github.com/dubinc/dub)) fills what Detour doesn't document. Production-tested link funnel, built for analytics, not deferred matching.
3. **Ours** where neither covers it, or they conflict with a self-hosting constraint. Reason required.

Changing a decision: edit its row, keep the ID. New conflict found: add a row before writing code.

## Matching

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| M1 | Weights: IP 500, model+version 450, iOS version 350, UA signature 350, pasteboard 350/175, timezone 200, screen 200 (±1, ±0.01 scale), language 100. One device signal per candidate. Max 1700 iOS / 1450 Android. | Detour | Documented. | — | in place |
| M2 | Threshold 850 (700–1200) and window 15 min (5–180) are per-app settings. No instance-wide default, no per-link override. | Detour | "Both values are app-level settings." Old global value seeded into every app on upgrade. | 005 | in place |
| M3 | Matched click is consumed: one click, one install. | Detour | "marks the click as matched"; candidates are "unmatched clicks". Dub doesn't consume (Redis cache read, not deleted). | 006 | in place |
| M4 | Same device retrying match-link gets its earlier click again. | Ours | SDK retries must stay idempotent; Detour silent on retries. | 006 | in place |
| M5 | Deterministic `clickId`: exact lookup, click must be unmatched, no window, lives `RETENTION_HOURS`. | Detour + ours | Detour: exact + unmatched. Retention bound is ours (no infinite storage). | 006 | in place |
| M6 | Window measured from SDK `timestamp`; ignored if more than 180 min from server time or in the future. | Detour + ours | Detour measures click→fingerprint time. Clamp is ours: client clocks drift. | 007 | in place |
| M7 | iOS 26 frozen UA (`OS 18_6`): read Safari `Version/` token when its major is higher. | Detour | Documented limitation + workaround. | 007 | in place |
| M8 | UA device signature scored on Android only. | Detour | Weights table "Scored on: Android". | 007 | in place |
| M9 | Pasteboard compares install's pasted URL with the clicked short link; click's `pasted_link` stored only if it is that link. | Detour + ours | Detour compares with "the click URL". Server-side check is ours: query param was forgeable. | 007 | in place |
| M10 | Ties → newer click. | Detour | Documented. | — | in place |
| M11 | No match → organic install + 404. Backend error → 404 + `unknown` install, hidden from analytics; later real result replaces `unknown`. | Detour + ours | Organic/404 documented. `unknown` + fail-open is ours: backend trouble never blocks the app. | 003 | in place |
| M12 | Device hash = SHA-256 of fingerprint fields, timestamp excluded. | Ours | Stable key for idempotent install rows. | — | in place |

## Click capture

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| C1 | Mobile first hop: one-reload interstitial reads screen + timezone; `Accept-CH` asks for model + OS version. | Dub | Dub deeplink preview pattern. Detour: "Detour collects the device fingerprint right away". | — | in place |
| C2 | iOS + App Store target → tap-to-copy page (copies short link, feeds M9). Always on. | Detour + ours | Detour copy-link flow; Detour makes it a per-app toggle — not adopted (YAGNI until asked). | 008 | in place |
| C3 | Bot filter: Dub `UA_BOTS`, HEAD and `?bot=` count as bots, Google webview exception. | Dub | Detour says "automated traffic is filtered", no list. | — | in place |
| C4 | Dedup: one click per link + IP + UA per hour; re-tap refreshes time/signals, keeps id and matched state. | Dub | Dub record-click cache. Detour silent. | — | in place |
| C5 | Clicks kept `RETENTION_HOURS` (24–8760), purged hourly. | Ours | Bounded storage; deterministic lookups need ≥ 24h. | 005 | in place |
| C6 | Deleting a link deletes its clicks; deleting an app deletes its clicks, installs, events. | Ours | Orphaned clicks kept matching with stale destinations. | 004 | in place |
| C7 | Click `tz` / `screen` stored only if well-formed and short. | Ours | Unvalidated URL input fed scoring. | 007 | in place |

## Redirects

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| R1 | App Store URL gets only `pt`, `ct`, `mt`, and `ppid` when it is a UUID. | Detour | "Each store URL carries only its own keys"; malformed `ppid` dropped. | 008 | in place |
| R2 | Play Store URL gets `utm_*` inside `referrer`, next to `click_id`. No top-level forwarding. | Detour | Documented. Blocks `?id=other.app` hijack. | 008 | in place |
| R3 | Other targets and the app destination get all non-empty params, add-only; operator URL never re-encoded. | Detour + ours | Detour "pass all parameters". Add-only is ours: operator keys win. Dub overwrites. | 008 | in place |
| R4 | Store/fallback targets set per link (iOS, Android, desktop). | Dub | Detour configures them per app; per-link is more flexible, already shipped. | — | in place |
| R5 | Short links are one path segment. | Ours (divergence) | Detour allows extra segments after the hash. Not adopted yet; see CAVEATS. | — | in place |
| R6 | Link targets must be absolute URLs; `javascript:`, `data:`, `vbscript:`, `file:` rejected; app schemes and `market:` allowed. | Ours | Typos became relative redirects. | 003 | in place |
| R7 | Expired link → `expiredUrl`, else 410. | Dub | Dub link expiry. | — | in place |

## Analytics

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| A1 | Overview parity: clicks, already installed opens, installs via link + match %, web fallbacks, daily chart, organic vs non-organic, per-link clicks/matches, top events. Filters: platform, 7/30/90 days. | Detour | Same overview page. Retention comparison skipped: events carry no device id. | — | in place |
| A2 | Daily rollup tables (`click_days`, `event_days`) hold counts only, never purged. | Ours | Raw clicks/events live `RETENTION_HOURS` (default 24h); a 90-day chart needs counts that outlive the purge without keeping device data. | — | in place |
| A3 | Web fallback = browser click redirected to a non-store URL. Already installed open = SDK universal-link-click. | Ours | Detour does not define them; these are what detur can observe. | — | in place |
| A4 | Days are UTC. | Ours | One bucket key for every viewer. | — | in place |

## SDK API

| ID | Decision | Source | Why | Plan | Status |
|---|---|---|---|---|---|
| S1 | Same five endpoints and response shapes as godetour.dev; universal-link-click returns `effectiveLimit: -1`. | Ours | Drop-in for SDK 2.3.x; no limits to report. | — | in place |
| S2 | universal-link-click: auth backend error → allow shape with fresh `clickId`, nothing written. | Ours | Never block a link; never write for an unverified app. | 004 | in place |

## Not adopted from Detour (yet)

In-app browser hand-off (Instagram/Facebook/TikTok), return-to-web after store dismiss, App Preview page, custom HTML redirect pages, fallback param strategies (specific / none), copy-link toggle, Smart Banners, multi-segment links, webhooks, Platform API, billing.

## Security and operations

Trust boundaries (portal without auth, TLS, `TRUST_PROXY`, key hashing): [CAVEATS.md](CAVEATS.md).
