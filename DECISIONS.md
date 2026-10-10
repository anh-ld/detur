# Decisions

What detur does, who said so, why.

## Source rule

1. **Detour docs** ([matching](https://detour.swmansion.com/docs/platform/architecture/matching/), [click handling](https://detour.swmansion.com/docs/platform/architecture/click-handling/), [limitations](https://detour.swmansion.com/docs/platform/architecture/architecture-limitations/)) win. Server is closed; docs = spec.
2. **Dub** ([dubinc/dub](https://github.com/dubinc/dub)) fills gaps. Analytics funnel only, not deferred matching.
3. **Ours**: neither covers it, or it breaks self-hosting. Reason required.

Change a decision: edit the row, keep the ID. New conflict: add the row before the code.

## Matching

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| M1 | Weights: IP 500, model+version 450, iOS version 350, UA signature 350, pasteboard 350/175, timezone 200, screen 200 (±1, ±0.01 scale), language 100. One device signal per candidate. Max 1700 iOS / 1450 Android. | Detour | Documented. | — |
| M2 | Threshold 850 (700–1200), window 15 min (5–180), per app. No global default, no per-link override. | Detour | "App-level settings." Upgrade copies the old global value into each app. | 005 |
| M3 | Matched click is consumed: 1 click, 1 install. | Detour | Candidates = "unmatched clicks". Dub doesn't consume. | 006 |
| M4 | Same device retries → same earlier click. Claim stores the device hash: an overlapping retry (install not written yet) still wins its own click. | Ours | SDK retries must be idempotent. Detour silent. Client timeout + retry raced to organic. | 006 |
| M5 | `clickId` match: exact, unmatched, no window, lives `CLICK_ID_DAYS` (30, 1–90). Fingerprint scrubbed at `RETENTION_HOURS`; matched clicks deleted then. | Detour + ours | Play referrer comes back days later. 90 = its limit. No extra PII. | — |
| M6 | Window starts at SDK `timestamp`; ignored if >180 min off or in the future. | Detour + ours | Clamp is ours: clock drift. | 007 |
| M7 | iOS 26 frozen UA (`OS 18_6`): use Safari `Version/` if its major is higher. `Version/` click compared at its precision (`26.0` = SDK `26.0.1`); OS-token click exact (`17_4` = `17.4.0`). | Detour + ours | Documented workaround. `Version/` has no patch; exact compare lost 350 on every 26.x.y. | 007 |
| M8 | UA signature scored on Android only. | Detour | Weights table. | 007 |
| M9 | Pasteboard: install's pasted URL vs clicked short link. Click `pasted_link` stored only if it is that link. | Detour + ours | Server check is ours: query param is forgeable. | 007 |
| M10 | Tie → newer click. | Detour | Documented. | — |
| M11 | No match → organic + 404. Backend error → 404 + hidden `unknown` install, replaced by the next real result (new row). Unknown never sent to webhooks. | Detour + ours | Fail-open is ours: never block the app. New row: webhook cursor already past the unknown one. | 003 |
| M12 | Device hash = SHA-256 of fingerprint, no timestamp. | Ours | Stable key, idempotent installs. | — |
| M13 | Install receipt: method (`click_id`, `probabilistic`, `prior`, `organic`, `unknown`), best score, runner-up. Organic keeps its best below-threshold score. Settings shows method mix, histogram, threshold what-if. | Ours | Otherwise the threshold is set blind. | — |

## Click capture

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| C1 | Mobile first hop: one-reload interstitial (screen, timezone); `Accept-CH` for model + OS. Mac Safari UA too: touch screen → iPad (UA rewritten to iPad for rules, redirect, click, scoring). | Dub + ours | Dub deeplink preview. Detour fingerprints immediately. iPadOS Safari sends a Mac UA: got the web fallback, never matched. | — |
| C2 | iOS + App Store target → tap-to-copy page (feeds M9). Always on. Copy + reload in one tap. | Detour + ours | Detour's per-app toggle: skipped. One tap: webviews block a hand-off after an async copy. | 008, in-app |
| C3 | Bots: Dub `UA_BOTS`, HEAD, `?bot=`. Exempt: Google webview, TikTok `Channel/googleplay`. | Dub + ours | Detour has no list. TikTok's webview tripped "google". | in-app |
| C4 | Dedup: link + IP + UA, 1h. Re-tap refreshes time + signals, keeps id + matched. Also merged: a real-browser click (no in-app source, same platform) on the same link + IP within 1h of an in-app click. Keeps the in-app destination + pasteboard. SDK opens and other in-app browsers never merge. | Dub + ours | Dub click cache. "Open in browser" changes the UA. | in-app |
| C5 | Clicks kept `RETENTION_HOURS` (24–8760), purged hourly. | Ours | Bounded storage. Deterministic match needs ≥24h. | 005 |
| C6 | Delete link → its clicks. Delete app → clicks, installs, events. | Ours | Orphans matched stale destinations. | 004 |
| C7 | Click `tz` / `screen` stored only if well-formed and short. | Ours | Unvalidated input fed scoring. | 007 |
| C8 | In-app browsers (Messenger, Facebook, Instagram, Threads, Zalo, TikTok, LinkedIn, Snapchat, LINE, X, Telegram, WeChat) get a tap page at hop 2, not the 302: Get the app link, Android `intent://` + Play backup, "Open in browser" steps. No store target → 302. | Ours | Webviews block automatic store hand-offs. A tapped link gets through. Detour docs: nothing. | in-app |
| C9 | Safety net: hop-1 page still visible after the delay → reload with `_tap` → tap page. 1.5s generic webview, 4s real-browser UA, off for named apps. | Ours | Covers apps no token names. 4s keeps slow real browsers unchanged. Tune on devices. | in-app |
| C10 | Click `source`: app name or `unknown-inapp`. Stored on the click, rolled up per link in `click_sources`, sent in webhooks, shown in the portal. | Ours | Makes social traffic visible. Own table keeps the `click_days` key. | in-app |

## Redirects

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| R1 | App Store URL: only `pt`, `ct`, `mt`, UUID `ppid`. | Detour | Store keys only. | 008 |
| R2 | Play Store: `utm_*` + `click_id` inside `referrer`. No top-level params. | Detour | Blocks `?id=other.app` hijack. | 008 |
| R3 | Other targets + app destination: all non-empty params, add-only, URL not re-encoded. | Detour + ours | Add-only is ours: operator keys win. Dub overwrites. | 008 |
| R4 | Store/fallback targets per link. | Dub | Detour: per app. Per link is more flexible. | — |
| R5 | Links: one path segment. | Ours (divergence) | Detour allows more. See CAVEATS. | — |
| R6 | Targets absolute. `javascript:`, `data:`, `vbscript:`, `file:` rejected. App schemes + `market:` OK. | Ours | Typos became relative redirects. | 003 |
| R7 | Expired → `expiredUrl`, else 410. | Dub | Dub expiry. | — |

## Link rules & A/B testing

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| LR1 | Ordered rules per link evaluated first-match-wins on visitor `platform` (`ios`/`android`/`desktop`), `lang` (`Accept-Language` prefix, e.g. `de`), `param` (query key and optional value), and `date` window (`from`/`until` in UTC). Unmatched traffic continues to subsequent rules or fallback link targets. | Dub + ours | Dub geo/device/param routing. Unified single table keeps evaluation deterministic. | feat-rules-ab |
| LR2 | Partial target inheritance: empty destination, iOS, Android, or fallback URL in rule actions or split variants inherit directly from the parent link. | Ours | Operators configure only what deviates; zero duplication of store URLs. | feat-rules-ab |
| LR3 | Sticky deterministic A/B split engine: 64-bit FNV-1a hash over `link_id\|ip\|ua` modulo 100 maps visitors to cumulative weight thresholds without cookies or client-side storage. | Ours | Stateless consistency across repeat clicks on the same device. | feat-rules-ab |
| LR4 | Variant attribution: click records the matched label in `clicks.variant` (split variant name, or the rule name for a direct rule; labels unique per link), inherited by attributed install upon `matchLink`, aggregated daily in `variant_days`, delivered in webhook payloads, and displayed with live conversion rates in portal. | Ours | Direct attribution loop from click split to installed app without SDK modification. | feat-rules-ab |
| LR5 | Gating: viewing rules and variant metrics is public to portal viewers; saving rule changes requires admin session when `ADMIN_PASSWORD` is set. | Ours | Consistent with P1 portal access model. | feat-rules-ab |

## Analytics

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| A1 | Overview: clicks, installed opens, installs via link + match %, web fallbacks, daily chart, organic vs non-organic, per-link clicks/matches, top events, in-app sources, per-link retention + conversions (A5–A8). Filters: platform, 7/30/90d. | Detour | Same page. | — |
| A2 | Rollups `click_days`, `click_sources`, `event_days`: counts only, never purged. | Ours | Raw data goes at 24h; a 90d chart needs counts without device data. | — |
| A3 | Web fallback = browser click → non-store URL. Installed open = universal-link-click. | Ours | Detour doesn't define them; this is what detur sees. | — |
| A4 | UTC days. | Ours | One bucket key. | — |
| A5 | Tag: SDK event with `device_id` + `data.link` (key or short URL) maps the device to that link. First tag wins. Device stored as SHA-256. Mapping purged 90d after tag. | Ours | `match-link` sends no `device_id`. Only the app knows where the device came from. | link-analytics |
| A6 | Conversions: every `/analytics/event` call from a tagged device, per link + event + UTC day, the tagging call included. Marker `detur_link` not counted. Retention calls never. | Ours | What each link's users do after install. | link-analytics |
| A7 | Retention: cohort = devices first tagged on one UTC day. D1/D7/D30 = retention call exactly N days later, once per device. Rate uses cohorts whose mark day is in range. Counts only, never purged (A2). All platforms. | Detour + ours | Detour's retention comparison. Mark-day range: every range shows all three marks. | link-analytics |
| A8 | Tag deferred links: per app, off by default, admin. On → `match-link` + `resolve-short` destination gets `detur_link=<key>`, add-only (R3). | Ours | SDK 2.3.1 hands the app the resolved destination, never the key. Off = destinations as stored. | link-analytics |

## Fraud

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| F1 | Server only. No SDK change. | Ours | Least scope. | — |
| F2 | Signals tagged first. Active = per-app opt-in. Fraud tab: press = saved + toast, no confirm. | Singular + ours | Nothing excluded unseen. No confirm: undo is one press. | — |
| F3 | Active: skip flagged click → best other ≥ threshold, else organic. Flagged `clickId` → organic. Install IP: own `install_ip` label, tag only. | Ours | M2 selection. `clickId` has no fingerprint to fall back on. | — |
| F4 | Installs never deleted; labels survive the scrub. | Ours | M11. | — |
| F5 | Velocity: raw hits per IP and per link, own counter. | Ours | C4 merges, C5 scrubs. | — |
| F6 | Click→install time: short = hijack; long = flood, `clickId` only. | AppsFlyer | M2 window caps probabilistic. | — |
| F7 | UA: C3 unchanged. Extra bot list + empty UA = tagged signal, kept out of A2. | Ours | C3 drops silently. | — |
| F8 | IP: hosting ASNs (ipverse), bundled. Akamai/Cloudflare/Fastly excluded; Private Relay subtracted at refresh. | ipverse + ours | No outbound calls. Relay = real users. Apple's list can't be redistributed. | — |
| F9 | Same hash, many installs, one link, probabilistic only. | Ours | M12 hashes config, not the device. | — |

## Ops

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| O1 | Settings Health: SDK last seen (`X-SDK`), app detail formats, AASA refresh (>1 iOS app, links <48h), proxy (private peers with `TRUST_PROXY=0`). No outbound checks. | Ours | Setup mistakes fail silently. | — |

## SDK API

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| S1 | Same 5 endpoints + shapes as godetour.dev. `effectiveLimit: -1`. | Ours | Drop-in for SDK 2.3.x. No limits. | — |
| S2 | universal-link-click auth backend error → allow + fresh `clickId`, no write. | Ours | Never block; never write unverified. | 004 |

## Portal access

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| P1 | Gateway lets everyone in as viewer (links + monitoring). Admin actions need `ADMIN_PASSWORD` → 12h signed session per browser. No user table, no RBAC. | Ours | One operator, self-hosted, team brings its own auth. One password beats roles at this scale. | 009 |
| P2 | Portal guard: Host = loopback, listener, or `PORTAL_HOSTS`. Origin = same origin (port included), or a `PORTAL_HOSTS` name. Other localhost ports rejected. | Ours | DNS rebinding + CSRF. Cookies ignore ports: any localhost page rode the admin cookie. Gateway names were 403. | — |

## Webhooks

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| W1 | Background worker drains SQLite `rowid` cursors. No outbox table. Rowids never reused (`row_seq` high-water mark). Stream added later → starts at its tail. | Ours | Zero cost on redirects and SDK calls. Replay for free. Deleted tail + refill skipped rows under the cursor. | feat-webhooks |
| W2 | Per endpoint, opt in to `installs`, `events`, `clicks`. | Ours | Installs without the click flood. | feat-webhooks |
| W3 | `Detur-Signature: t=<unix>,v1=<hex>`, HMAC-SHA256 over `t.<unix>.<raw_body>`. Replays carry `Detur-Replay: true`. | Stripe + ours | Standard verification; timestamp stops replay. | feat-webhooks |
| W4 | Dial-time SSRF block: private ranges (RFC 1918, RFC 4193), link-local, CGNAT `100.64/10`, `192.0.0/24`, `198.18/15`, reserved, NAT64 / 6to4 / Teredo. Covers metadata at 169.254.169.254, 100.100.100.200 (Alibaba), 192.0.0.192 (Oracle). Loopback HTTP allowed for dev. No redirects followed. | Ours | Cloud hosts + DNS rebinding. IPv6 ranges embed an IPv4. | feat-webhooks |
| W5 | Backoff per `(endpoint, event_type)`: `min(30s * 2^fails, 1h)`. Failing clicks don't block installs. | Ours | One bad stream can't stall the rest. | feat-webhooks |
| W6 | Managing webhooks needs admin. `ADMIN_PASSWORD` unset → 403. | Ours | Guards secrets, endpoints, replay. | feat-webhooks |
| W7 | No outbound calls unless a webhook is configured and enabled. | Ours | Offline by default. | feat-webhooks |

## Not built

| Item | Why not |
|---|---|
| Ad-network / MMP postbacks (Meta, Google, TikTok, Snap) | Google, TikTok, Snap accept data only from certified MMPs. Meta needs SDK fields Detour doesn't send. |
| Auto escape to Safari/Chrome (`x-safari-https`) | Undocumented, fails on iOS 16. C8 tap page instead. |
| Opening an installed iOS app at the link from a webview | iOS gives no way out. App opens on its home screen. |
| Click-injection detection | Needs the raw Play referrer; SDK sends only `click_id`. |
| VPN/Tor IP detection | Second dataset; flags real users. |
| Copy-link toggle | C2 is always on. No one needs it off. |
| Multi-segment links | R5. |
| Platform API | Portal API + webhooks cover it. |
| Billing | Self-hosted. No tiers. |
| Revenue sums, organic-cohort retention, marks beyond D1/D7/D30 | No request yet. |
| Return-to-web after store dismiss, App Preview page, custom redirect HTML, fallback param strategies, Smart Banners | Detour features with no request yet. |

## Security, ops

Trust boundaries: [CAVEATS.md](CAVEATS.md). Log out → `LOGOUT_URL` (gateway sign-out). Admin: `ADMIN_PASSWORD` → 12h browser session (P1); unset = no gating. Webhooks always need it (W6).
