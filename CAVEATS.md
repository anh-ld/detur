# Caveats

Read before deploying. Why → [DECISIONS.md](DECISIONS.md).

## Security

- **Portal**: no user accounts → anyone reaching `:8081` sees links + analytics; `ADMIN_PASSWORD` unset → can also edit/delete apps. Loopback or zero-trust proxy (Cloudflare Access, Tailscale) + `PORTAL_HOSTS` = gateway hostname, else 403.
- **Admin cookie**: derived from the password → crackable offline if intercepted. Long random password + HTTPS. New `ADMIN_PASSWORD` → all sessions end.
- **Login delay**: per IP, serialized. Proxy + `TRUST_PROXY=0` → one shared IP → one brute-force run delays everyone.
- **Logout**: only redirects to `LOGOUT_URL`. IdP auto re-login (or Tailscale) → session survives.
- **Portal admin UI**: mirrors the server, never enforces. Dead session → locked notice, no prompt until Unlock. Server restarted without `ADMIN_PASSWORD` while a tab is open → Webhooks/API show an Unlock that cannot succeed until reload.
- **TLS**: plain HTTP only → leaks API keys + fingerprints. Terminate HTTPS at a proxy.
- **API keys**: unsalted SHA-256; safe only with ~131-bit generated keys.
- **`TRUST_PROXY`**: `1` without a header-stripping proxy → spoofed `X-Forwarded-For` games the 400-point IP score. `0` behind a proxy → everyone shares its IP → unrelated installs get the 400.
- **`TRUSTED_PROXIES`**: forwarded headers honored only from these (default loopback + private). Public-IP proxy (Cloudflare, ALB) → add its CIDR. Chains (Cloudflare → ingress → detur): list every hop; `X-Forwarded-For` read right to left. `CF-Connecting-IP` ignored (spoofable around Cloudflare). Startup log + Health warn.

## Matching

- **Shared Wi-Fi**: timezone + language count only with pasteboard or device + screen. Other OS version or screen → ≤ 750 → organic.
- **Identical devices, same Wi-Fi**: 1250, indistinguishable without pasteboard → a non-clicker may take the link. Two identical clickers: paste token (iOS) tells them apart.
- **No interstitial**: no screen → IP + device ≤ 800 → organic unless pasteboard.
- **One click, one install**: two installs on one Wi-Fi after one click → second goes organic.
- **Late `clickId`**: matches for `CLICK_ID_DAYS` (30), but fingerprint purged at `RETENTION_HOURS` (24h) → click metadata (platform) lost.
- **Threshold ceilings**: cellular → Wi-Fi (no IP) max 850 iOS / 900 Android without pasteboard; threshold above → organic. Below 801 Android / 751 iOS → IP + device alone matches → shared Wi-Fi peers can take the link.
- **Device hash = setup** (model, OS, screen, locale, timezone), not a device. Same hash within 15 min → earlier answer (SDK retries). Later → fresh match: reinstall gets a newer link or organic; identical phone never gets another's link; counts toward the fingerprint fraud signal.
- **Threshold + window**: per app (850, 15 min); no per-link override.
- **Clock drift**: SDK timestamp used only within 180 min of server time.
- **iPad Safari**: sends a Mac UA → Macs get the interstitial too (one extra reload); touch screen → iPad. No tap-to-copy (unknown at hop 1) → no pasteboard.
- **Interstitial**: one reload → screen, timezone, client hints (model + OS). iOS max without pasteboard 1250.
- **iOS pasteboard**: tap-to-copy before the App Store; paste token (`?_dc=`) → exact match, no window, lives `CLICK_ID_DAYS`. No token → 350 exact / 175 domain. Paste denied → lost. `pastedLink` checked against the clicked link.
- **Paste guard (SDK patch)**: SDK reads only URL-typed clipboards → no alert. Tap page writes `text/uri-list`; browser can't → no pasteboard → device + screen.
- **AASA**: >1 iOS app → newest 256 keys per app, older links open the browser. File > 128 KB → Apple drops it all. Keep keys short.
- **In-app (iOS)**: Messenger, TikTok, Zalo → "Get the app" tap page. Button → App Store; iOS URL scheme set (Settings) → `<scheme>://<route>?<query>` first, App Store after 1.5s. SDK reads schemes only with `linkProcessingMode: "all"`. Webview prompts or blocks schemes → App Store.
- **In-app (Android)**: `intent://` opens the app only if package + App Links cert match; else Play.
- **Dedup**: link + IP + UA within 1h = one click; re-tap refreshes time + signals.
- **In-app → browser merge**: "Open in browser" within 1h merges into the in-app click. Shared IPs (office, CGNAT) → two users can merge.
- **X, Telegram**: Safari View Controller / Custom Tabs send plain browser UAs → only the delayed safety net catches them.
- **Tap page**: English only. Skip in dev: `?detur-no-track=1`.
- **Custom get-app page**: admin HTML runs on the link domain (can break the install flow). CSP blocks scripts, not `<meta http-equiv="refresh">`. Old webviews ignore CSP; the save check only catches `<script` tags.

## Fraud

- **Modes**: signals start Tagged (monitor). Active: per app, Fraud tab, saves instantly, no confirm.
- **Active**: flagged clicks skipped → organic 404, no deferred link.
- **Datacenter IPs**: ASN ranges bundled per release → pinned versions go stale.
- **CGNAT velocity**: thousands of devices per IP → bursts trip velocity. Review before Active.
- **Private IPs**: always exempt. Proxy + `TRUST_PROXY=0` → IP signals blind.
- **Bots**: bot/suspicious UAs never counted in analytics, even Tagged.
- **Rule changes**: future matches only; no re-scoring.
- **Not detected**: click injection, device resets, emulators, receipts ([DECISIONS.md](DECISIONS.md)).

## Ops

- **SQLite**: WAL, one connection → reads + writes queue (big analytics query delays redirects). Hourly purge in 2,000-row batches. Personal → mid-size scale; no horizontal scaling.
- **Shutdown**: SIGTERM drains requests (8s), cancels webhook POSTs (redelivered on start), closes DB; WAL survives a hard kill. Migrations add columns on `Open()`. Back up `detur.db` before upgrading.
- **Floods**: no rate limits. Unmatched clicks pile up until `RETENTION_HOURS` (`CLICK_ID_DAYS` for click IDs). Match scans newest 5,000 per window → older can't match. Headers ≤ 32 KB, link targets ≤ 2 KB; stored UA ≤ 1 KB, destination ≤ 4 KB (longer → link URL, no visitor params).
- **Rollups**: UTC days. Link delete → its rollups gone. Installs + rollups never purged.
- **Events**: body > 16 KB → 413. Names cut to 64 chars, kept forever → no PII/IDs.
- **Per-link conversions**: need a `data.link` tag event. SDK 2.3.1 knows the key only with "Tag deferred links" on.
- **Retention**: cohort = tag day, not install day. D1/D7/D30 = cold start (`app_open`) that UTC day; warm resume only via `logRetention`. Tags expire at 90 days → re-tag = new cohort. Ignores the platform filter.
- **Sub-day analytics**: 15m/1h/3h/12h windows read raw clicks/installs, aggregated in SQLite (`strftime('%s')` buckets). Raw rows last `RETENTION_HOURS` (24h); older data shows in day ranges only.
- **Link-filtered analytics**: counts only that link's traffic, conversions and retention cohorts; organic installs show 0.
- **Tenancy**: one team; no RBAC, no org isolation.

## Data API

- **Tokens**: never expire; survive `ADMIN_PASSWORD` change → revoke in portal (API page). Removing `ADMIN_PASSWORD` disables every token.
- **Public port**: `:8080` → HTTPS proxy required (bearer in every request).
- **App list**: explicit → new app needs a token edit.
- **No rate limit**: looping agent or leaked token can hammer long-range analytics on the one SQLite connection → redirects slow. Revoke to stop.
- **MCP**: any client that sends a header to a Streamable HTTP server. Web connectors needing OAuth (claude.ai, ChatGPT) can't connect.

## Webhooks

- **Outbound calls**: webhooks (enabled) + tap-page icons (iTunes Lookup in the link's storefront, Play listing; 24h in-memory, failures logged). Browsers load icons from Apple/Google CDN → they see IP + UA; Referer = origin.
- **SSRF**: delivery blocks RFC 1918, RFC 4193, link-local, CGNAT `100.64.0.0/10`, reserved, IPv4-in-IPv6 (NAT64, 6to4, Teredo) → covers `169.254.169.254`, `100.100.100.200`, `192.0.0.192`. No redirects.
- **Secrets**: plaintext in `detur.db` (viewable + rotatable) → `chmod 600 detur.db`.
- **Retention**: only installs kept forever; clicks + events purged at `RETENTION_HOURS` → replay starts at the oldest left; endpoint down longer → those lost. One shared worker → slow endpoint delays others. Rowids never reused. Older backup restore → cursors 0 → duplicates; dedupe on `id`.
- **Revoke**: disable, delete, or stream removal stops after the batch in flight; URL/secret change applies to the next batch.
- **New stream**: starts at the tail; past → Replay.
- **`unknown` installs**: never sent; replacement arrives as a new record.
- **Consumers**: constant-time signature compare (`crypto/subtle.ConstantTimeCompare`) + timestamp tolerance (~5 min).

## Rules & A/B

- **Bucket**: hash of `link_id|client_ip|user_agent` → CGNAT/office Wi-Fi + same phone/browser = same bucket; Wi-Fi → cellular may shift it.
- **Order**: `1..N`, first match wins → broad rule above specific one shadows it.
- **Dates**: `from`/`until` UTC (RFC 3339).
- **Weights**: 1–100% each, total 100%; new variant = remainder. Changing them keeps past attributions; returning visitors may switch variant (`hash mod 100`).
- **Drawer**: edits kept until Save/Discard; nothing applies before Save.
- **Installed app**: rules run on browser clicks (→ carried into deferred installs); SDK `resolve-short` → base destination, no variant.
- **Labels = analytics key** (`variant_days`: link + label) → unique per link; rename = new stats row.
- **Link delete** → its rules + `variant_days` gone.

## Compatibility

- **Desktop**: app-scheme target, no fallback → QR page (`https://<host>`; dev `host:port` → `http`).
- **Destination**: app route only (`/product/42`). Web visitors need the fallback URL, else app page.
- **Paths**: one segment (`/promo`); `/app/promo` → 404, no click. `/promo/` → 301 `/promo`, query kept.
- **Link title/note**: optional, ≤100 chars; a label only, no effect on path or routing.
- **Sub-day window analytics** (`?window=`): buckets are RFC3339 starts, latest partial; no conversions/retention (day-keyed rollups); events app-wide, empty when filtered to a link (raw events have no link). Other endpoints reject `window` (400).
- **Click platform backfill**: pre-upgrade clicks get `platform` from their user agent at startup; rows with a scrubbed UA stay unset and drop out of platform-filtered window views.
- **Link tags**: optional labels (≤10, ≤24 chars each); filter the Links list only — not analytics, rules, or routing.
- **Store and web redirect presets**: templates, not inheritance. Picking one copies its URLs into the link; editing the preset later leaves existing links unchanged.
- **Social preview presets**: same rule. Picking one copies title/description/image into the link; editing the preset later leaves existing links unchanged.
- **Self-hosted**: no billing, tiers, or godetour platform API.
- **SDK**: `effectiveLimit: -1` = unlimited (2.3.x). URLs hardcoded to `godetour.dev` → regenerate the patch per SDK upgrade.
- **Example app** (`example/expo-app/`): untested on real devices.
- **Portal tests**: need `:8080` free (a running `detur` makes every server-backed test file fail). Stop it first.
- **kinu `Collapsible`**: injects its own empty `<summary>`; use `Accordion` for `<details>`-style sections.
- **Portal dev**: `bun run dev` full-reloads on edit (no state-preserving HMR for Preact). Build prints `@position-try` warnings from kinu CSS: harmless.
- **Portal wire format**: link `expiresAt` / `ruleSummary` `""` = never / no rules; `tags` always `[]`; `AppPage.html` `""` = default page; flagged `fraudAction` `""` = tagged only; `lastUsedAt` null = never used. Older servers omit analytics `conversions` / `retention` / `sources` → treated as empty.
- **Portal tests**: use `gone(el)`, not `expect(el).toBeNull()`. Bun formats a failing DOM node eagerly (millions of lines, blocks the event loop ~1s per `waitFor` poll).
- **Server tests**: CI runs `-race` only on goroutine tests and non-SQLite packages (`server/scripts/test.sh`). `-race` makes the pure-Go SQLite driver ~16x slower (full suite ~3 min vs ~25s). New concurrency test → add its name to the `-run` list. Full check: `go test -race ./...`.
- **Portal CSS order**: `bun run dev` serves each CSS import as its own sheet, order not guaranteed (theme loaded before kinu → drawer, card, table overrides lost). `theme.css` starts with `@import 'kinu/style.css'`; never import kinu CSS from JS.
- **kinu `Accordion`**: body is `overflow: hidden`, so a flush field loses its 2px focus ring on the sides. `theme.css` pads the body 4px and pulls it back with a −4px margin.
