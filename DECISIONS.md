# Decisions

What detur does, who said so, why.

## Source rule

1. **Detour docs** ([matching](https://detour.swmansion.com/docs/platform/architecture/matching/), [click handling](https://detour.swmansion.com/docs/platform/architecture/click-handling/), [limitations](https://detour.swmansion.com/docs/platform/architecture/architecture-limitations/)) win: server closed → docs = spec.
2. **Dub** ([dubinc/dub](https://github.com/dubinc/dub)) fills gaps: analytics funnel only, not deferred matching.
3. **Ours**: neither covers it, or it breaks self-hosting. Reason required.

Change a decision → edit the row, keep the ID. New conflict → add the row before the code.

## Matching

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| M1 | Weights: IP 400 (was 500), model+version 400 (was 450), iOS version 350, UA signature 350, pasteboard 350/175, timezone 200, screen 200 (±1, ±0.01 scale), language 100. One device signal per candidate. Max 1600 iOS / 1300 Android. Timezone + language count only with pasteboard or device + screen → 850 unreachable without one (tested on every combination). | Detour + ours | Timezone + language = whole network → confirm, never decide; shared Wi-Fi peers stay organic. Higher threshold rejected: breaks cellular → Wi-Fi (850). Off-network max 850 iOS / 900 Android (was 950) → saved thresholds above lose those matches (CAVEATS). | 0142 |
| M2 | Per app: threshold 850 (700–1200), window 15 min (5–180). No global default, no per-link override. | Detour | "App-level settings." Upgrade copies the old global into each app. | 005 |
| M3 | Match consumes the click: 1 click, 1 install. | Detour | Candidates = "unmatched clicks". Dub doesn't consume. | 006 |
| M4 | Same hash within 15 min → same earlier click; later = fresh match; organic rows dedupe within the window only (hash = setup, not device). Claim stores the hash → overlapping retry (install not yet written) still wins its click. | Ours | Idempotent SDK retries; Detour silent. Timeout + retry raced to organic. | 006 |
| M5 | `clickId`: exact, unmatched, no window, lives `CLICK_ID_DAYS` (30, 1–90). Fingerprint scrubbed at `RETENTION_HOURS`; matched clicks deleted then. | Detour + ours | Play referrer returns days later; 90 = its limit. No extra PII. | — |
| M6 | Window starts at SDK `timestamp`; ignored if >180 min off or future. | Detour + ours | Clamp (ours): clock drift. | 007 |
| M7 | iOS 26 frozen UA (`OS 18_6`) → Safari `Version/` if its major is higher. `Version/` compared at its precision (`26.0` = `26.0.1`); OS token exact (`17_4` = `17.4.0`). | Detour + ours | Documented workaround. `Version/` has no patch → exact compare lost 350 on every 26.x.y. | 007 |
| M8 | UA signature: Android only. | Detour | Weights table. | 007 |
| M9 | Pasteboard: pasted URL vs clicked short link. Click `pasted_link` stored only if it is that link (`?_dc=` OK, M15). | Detour + ours | Server check (ours): query param forgeable. | 007 |
| M10 | Tie → newer click. | Detour | Documented. | — |
| M11 | No match → organic + 404. Backend error → 404 + hidden `unknown` install, replaced by the next real result (new row); never sent to webhooks. | Detour + ours | Fail-open (ours): never block the app. New row: webhook cursor already past it. | 003 |
| M12 | Device hash = SHA-256(fingerprint), no timestamp. | Ours | Stable key → idempotent installs. | — |
| M13 | Install receipt: method (`click_id`, `pasteboard`, `probabilistic`, `prior`, `organic`, `unknown`), best score, runner-up; organic keeps its best sub-threshold score. Settings: method mix, histogram, threshold what-if. | Ours | Else thresholds are set blind. | — |
| M14 | >1 iOS app → AASA: each app claims its own keys (paths + components), newest 256; zero-link apps omitted. Single app keeps `["*"]` (R7 freshness). | Ours | Wildcard per app → iOS opens the first match → first app shadowed every sibling's Universal Links. | 0142 |
| M15 | Paste token: copy + tap pages copy `/<key>?_dc=<16 chars>`; click stores it (`paste_token`), kept `CLICK_ID_DAYS`, survives scrub. Pasted → exact match: no window, same key (case-insensitive), unmatched or same device, fraud as `clickId` (F3). Miss → scoring (ignores the token), not organic. `_dc` never forwarded. | DeepLinkNow + ours | Pasteboard alone = 350, window-bound → slow installs went organic, identical clickers on one Wi-Fi swapped links. Own token, not `click_id`: copy precedes the click. Link-bound → leaked token can't claim another link. | link-page-gaps |

## Click capture

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| C1 | Mobile hop 1: one-reload interstitial (screen, timezone) + `Accept-CH` (model, OS). Mac Safari too: touch screen → iPad (UA rewritten for rules, redirect, click, scoring). | Dub + ours | Dub deeplink preview; Detour fingerprints immediately. iPadOS Safari sends a Mac UA → got the web fallback, never matched. | — |
| C2 | iOS + App Store target → tap-to-copy page (feeds M9), always on; copy + reload in one tap. Same get-app page design as C8 (C12). | Detour + ours | Detour's per-app toggle skipped. One tap: webviews block hand-off after an async copy. | 008, in-app |
| C3 | Bots: Dub `UA_BOTS`, HEAD, `?bot=`. Exempt: Google webview, TikTok `Channel/googleplay`. | Dub + ours | Detour has no list. TikTok's webview tripped "google". | in-app |
| C4 | Dedup: link + IP + UA, 1h; re-tap refreshes time + signals, keeps id + matched. Also merged: real-browser click (no in-app source, same platform, link, IP) within 1h of an in-app click → keeps the in-app destination. Pasteboard: latest copy wins; no browser copy → in-app one kept (M15). SDK opens + other in-app browsers never merge. | Dub + ours | Dub click cache. "Open in browser" changes the UA. | in-app |
| C5 | Clicks kept `RETENTION_HOURS` (24–8760), purged hourly; `click_id` + `paste_token` live `CLICK_ID_DAYS` (M5, M15). | Ours | Bounded storage; deterministic match needs ≥24h. | 005 |
| C6 | Delete link → its clicks. Delete app → clicks, installs, events. | Ours | Orphans matched stale destinations. | 004 |
| C7 | Click `tz` / `screen` stored only if well-formed + short. | Ours | Unvalidated input fed scoring. | 007 |
| C8 | In-app browsers (Messenger, Facebook, Instagram, Threads, Zalo, TikTok, LinkedIn, Snapchat, LINE, X, Telegram, WeChat) → tap page at hop 2, not 302: Get the app, Android `intent://` + Play backup, "Open in browser" steps. No store target → 302. | Ours | Webviews block automatic store hand-offs; a tap gets through. Detour docs: nothing. | in-app |
| C9 | Safety net: hop-1 page still visible after delay → reload with `_tap` → tap page. 1.5s generic webview, 4s real-browser UA, off for named apps. | Ours | Covers unnamed apps; 4s spares slow real browsers. Tune on devices. | in-app |
| C10 | Click `source`: app name or `unknown-inapp` → stored on the click, rolled up per link (`click_sources`), sent in webhooks, shown in the portal. | Ours | Social traffic visible. Own table keeps the `click_days` key. | in-app |
| C11 | Copy + tap page store icon: App Store (iTunes Lookup `artworkUrl512`, link's storefront), else Play (listing `og:image`; no API). Background fetch, in-memory 24h (1h after failure); first visit → generic glyph. | Ours | Recognizable app, no new field. Page never waits on a store. | in-app |
| C12 | Per-app custom get-app page (`apps.page_html`): one HTML for the copy page + tap page, `{{placeholders}}`, admin-only. We own the button script (`id="detur-cta"`, reserved); inserted markup styled via `.detur-cta`, `.detur-alt`, `.detur-hint`, `.detur-steps`; user scripts blocked by CSP (nonce) + rejected on save. Empty = default design. | Detour-inspired | Detour has a template form; custom HTML covers it. A dead or scripted page would break installs. | custom-page |

## Redirects

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| R1 | App Store URL: only `pt`, `ct`, `mt`, UUID `ppid`. | Detour | Store keys only. | 008 |
| R2 | Play: `utm_*` + `click_id` inside `referrer`; no top-level params. | Detour | Blocks `?id=other.app` hijack. | 008 |
| R3 | Other targets + app destination: all non-empty params, add-only, URL not re-encoded. | Detour + ours | Add-only (ours): operator keys win; Dub overwrites. | 008 |
| R4 | Store/fallback targets per link. | Dub | Detour: per app; per link is more flexible. | — |
| R5 | Links: one path segment. | Ours (divergence) | Detour allows more. CAVEATS. | — |
| R6 | Store/fallback targets absolute; destination = app route (R10). `javascript:`, `data:`, `vbscript:`, `file:` rejected; app schemes + `market:` OK. | Ours | Typos became relative redirects. | 003 |
| R7 | Expired → `expiredUrl`, else 410. | Dub | Dub expiry. | — |
| R8 | Desktop (not bot) + non-http(s) target → 200 QR page: short link, copy button, store buttons when set; click recorded (`web`). QR: `rsc.io/qr`, only dep besides sqlite. | DeepLinkNow + ours | 302 to `myapp://` opened nothing. | link-page-gaps |
| R9 | Per link: `og_title` (≤100), `og_description` (≤300), `og_image` (https). Set + GET + bot (C3) → OG page (canonical = short link, script redirect + Continue link); unset → 302. Base link only; rules pick the onward target. | DeepLinkNow + ours | Store/scheme targets had no chat preview. Redirect: humans misread as bots still get through. | link-page-gaps |
| R10 | Destination = app route only (`/product/42?x=1`; link, rule, variant). SDK gets `/<first 3 of appID>/product/42?x=1` (match-link, resolve-short). Browsers never redirect to it: no fallback + no store target for the platform → app page (store buttons on phone, QR on desktop); OG page = preview only. Web visitors + phones without their platform's store target (iPhone, Android-only link) → fallback URL. | Detour + ours | SDK drops the first path segment (Detour's app hash) and non-web URLs in deferred-only mode → full URLs lost a segment, `myapp://` never arrived. Path branch avoids both, skips resolve-short. Relative 302 hit another short link or 404. | route-destinations |

## Link rules & A/B testing

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| LR1 | Ordered rules per link, first match wins: `platform` (`ios`/`android`/`desktop`), `lang` (`Accept-Language` prefix, e.g. `de`), `param` (key, optional value), `date` (`from`/`until`, UTC). No match → next rule → link targets. | Dub + ours | Dub geo/device/param routing. One table → deterministic. | feat-rules-ab |
| LR2 | Empty destination/iOS/Android/fallback in a rule or variant → inherited from the link. | Ours | Set only what differs; store URLs never duplicated. | feat-rules-ab |
| LR3 | Sticky split: 64-bit FNV-1a(`link_id\|ip\|ua`) mod 100 → cumulative weight bands. No cookies, no client storage. | Ours | Same device → same variant, stateless. | feat-rules-ab |
| LR4 | Click stores the label in `clicks.variant` (variant name, or rule name for a direct rule; unique per link) → matched install inherits it (`matchLink`) → `variant_days`, webhooks, portal conversion rates. | Ours | Installs attributed to variants, no SDK change. | feat-rules-ab |
| LR5 | Rules + variant metrics: portal viewers can view + save (P1). | Ours | Rules route traffic like link targets, which viewers already edit → admin gate on rules alone protected nothing. | feat-rules-ab |

## Analytics

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| A1 | Overview: clicks, installed opens, installs via link + match %, web fallbacks, daily chart, organic vs non-organic, per-link clicks/matches, top events, in-app sources, per-link retention + conversions (A5–A8). Filters: platform, 7/30/90d. | Detour | Same page. | — |
| A2 | Rollups `click_days`, `click_sources`, `event_days`: counts only, never purged. | Ours | Raw data gone at 24h; 90d chart needs counts without device data. | — |
| A3 | Web fallback = browser click → non-store URL. Installed open = universal-link-click. | Ours | Detour doesn't define them; this is what detur sees. | — |
| A4 | UTC days. | Ours | One bucket key. | — |
| A5 | Tag: SDK event with `device_id` + `data.link` (key or short URL) → device mapped to that link; first tag wins; device stored as SHA-256; mapping purged 90d after tag. | Ours | `match-link` sends no `device_id`; only the app knows the source. | link-analytics |
| A6 | Conversions: every `/analytics/event` from a tagged device, per link + event + UTC day, tagging call included. `detur_link` marker not counted; retention calls never. | Ours | What each link's users do after install. | link-analytics |
| A7 | Retention: cohort = devices first tagged on one UTC day. D1/D7/D30 = retention call exactly N days later, once per device. Rate uses cohorts whose mark day is in range. Counts only, never purged (A2). All platforms. | Detour + ours | Detour's retention comparison. Mark-day range → every range shows all three marks. | link-analytics |
| A8 | Tag deferred links: per app, off by default, admin. On → `match-link` + `resolve-short` destination gets `detur_link=<key>`, add-only (R3). | Ours | SDK 2.3.1 hands the app the destination, never the key. Off = destinations as stored. | link-analytics |

## Fraud

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| F1 | Server only; no SDK change. | Ours | Least scope. | — |
| F2 | Signals tagged first; Active = per-app opt-in. Fraud tab: press = saved + toast, no confirm. | Singular + ours | Nothing excluded unseen; undo is one press. | — |
| F3 | Active: skip flagged click → best other ≥ threshold, else organic. Flagged `clickId` → organic. Install IP: own `install_ip` label, tag only. | Ours | M2 selection. `clickId` has no fingerprint fallback. | — |
| F4 | Installs never deleted; labels survive the scrub. | Ours | M11. | — |
| F5 | Velocity: raw hits per IP + per link, own counter. | Ours | C4 merges, C5 scrubs. | — |
| F6 | Click→install time: short = hijack; long = flood, `clickId` only. | AppsFlyer | M2 window caps probabilistic. | — |
| F7 | UA: C3 unchanged. Extra bot list + empty UA = tagged signal, kept out of A2. | Ours | C3 drops silently. | — |
| F8 | IP: hosting ASNs (ipverse), bundled. Akamai/Cloudflare/Fastly excluded; Private Relay subtracted at refresh. | ipverse + ours | No outbound calls. Relay = real users. Apple's list not redistributable. | — |
| F9 | Same hash, many installs, one link, probabilistic only. | Ours | M12 hashes config, not device. | — |

## Ops

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| O1 | Settings Health: SDK last seen (`X-SDK`), app detail formats, AASA refresh (>1 iOS app, links <48h), proxy (private peers + `TRUST_PROXY=0`). No outbound checks. | Ours | Setup mistakes fail silently. | — |

## SDK API

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| S1 | Same 5 endpoints + shapes as godetour.dev; `effectiveLimit: -1`. | Ours | Drop-in for SDK 2.3.x; no limits. | — |
| S2 | universal-link-click auth backend error → allow + fresh `clickId`, no write. | Ours | Never block; never write unverified. | 004 |

## Portal access

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| P1 | Gateway → everyone is a viewer (internal team). Marketing actions (links, rules, A/B splits, link delete) open; important ones (apps, keys, matching, tagging, fraud, webhooks) admin → `ADMIN_PASSWORD` → 12h signed browser session. No user table, no RBAC. | Ours | One operator, self-hosted, team brings its own auth; one password beats roles at this scale. | 009 |
| P2 | Portal guard: Host = loopback, listener, or `PORTAL_HOSTS`. Origin = same origin (port included) or a `PORTAL_HOSTS` name; other localhost ports rejected. | Ours | DNS rebinding + CSRF. Cookies ignore ports → any localhost page rode the admin cookie. Gateway names got 403. | — |

## Webhooks

| ID | Decision | Source | Why | Plan |
|---|---|---|---|---|
| W1 | Background worker drains SQLite `rowid` cursors; no outbox. Rowids never reused (`row_seq` high-water mark). Stream added later → starts at its tail. | Ours | Zero cost on redirects + SDK calls; replay for free. Deleted tail + refill skipped rows under the cursor. | feat-webhooks |
| W2 | Per endpoint, opt in to `installs`, `events`, `clicks`. | Ours | Installs without the click flood. | feat-webhooks |
| W3 | `Detur-Signature: t=<unix>,v1=<hex>`, HMAC-SHA256 over `t.<unix>.<raw_body>`. Replays carry `Detur-Replay: true`. | Stripe + ours | Standard verification; timestamp stops replay. | feat-webhooks |
| W4 | Dial-time SSRF block: RFC 1918, RFC 4193, link-local, CGNAT `100.64/10`, `192.0.0/24`, `198.18/15`, reserved, NAT64 / 6to4 / Teredo → covers 169.254.169.254, 100.100.100.200 (Alibaba), 192.0.0.192 (Oracle). Loopback HTTP allowed (dev). No redirects. | Ours | Cloud hosts + DNS rebinding; IPv6 ranges embed IPv4. | feat-webhooks |
| W5 | Backoff per `(endpoint, event_type)`: `min(30s * 2^fails, 1h)`; failing clicks don't block installs. | Ours | One bad stream can't stall the rest. | feat-webhooks |
| W6 | Managing webhooks needs admin; `ADMIN_PASSWORD` unset → 403. | Ours | Guards secrets, endpoints, replay. | feat-webhooks |
| W7 | Outbound calls: enabled webhooks + tap-page icons (C11), nothing else. | Ours | No surprise traffic. | feat-webhooks |

## Not built

| Item | Why not |
|---|---|
| Ad-network / MMP postbacks (Meta, Google, TikTok, Snap) | Google, TikTok, Snap accept only certified MMPs; Meta needs SDK fields Detour doesn't send. |
| Auto escape to Safari/Chrome (`x-safari-https`) | C8 already = one tap + copy; escape adds an app switch, saves nothing. Undocumented, fails on iOS 16. Re-checked 2026-10-26. |
| Opening an installed iOS app at the link from a webview | iOS gives no way out → app opens on its home screen. |
| Click-injection detection | Needs the raw Play referrer; SDK sends only `click_id`. |
| VPN/Tor IP detection | Second dataset; flags real users. |
| Copy-link toggle | C2 always on; no one needs it off. |
| Multi-segment links | R5. |
| Platform API | Portal API + webhooks cover it. |
| Billing | Self-hosted; no tiers. |
| Revenue sums, organic-cohort retention, marks beyond D1/D7/D30 | No request yet. |
| Return-to-web after store dismiss, App Preview page, fallback param strategies, Smart Banners | Detour features, no request yet. |

## Security, ops

Trust boundaries → [CAVEATS.md](CAVEATS.md). Log out → `LOGOUT_URL` (gateway sign-out). Admin: `ADMIN_PASSWORD` → 12h browser session (P1); unset = no gating. Webhooks always need it (W6).
