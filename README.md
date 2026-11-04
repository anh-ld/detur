# detur

Self-hosted, drop-in [godetour](https://godetour.dev) backend for `@swmansion/react-native-detour`: deferred deep links (tap → install → first launch opens the link) + click, install, event analytics.

- **Single binary**: Go + SQLite + Preact portal; ~30 MB RAM, <0.1 vCPU.
- **Rules & A/B**: route by platform, language, query, date; sticky weighted splits.
- **In-app browsers**: tap page → Messenger, TikTok, Zalo users never get stuck.
- **Exact iOS match**: copied link carries a one-time token → no time window.
- **Link pages**: per-link social previews; desktop QR page for app-only links.
- **Fraud**: flag/block click floods, bots, datacenter IPs, repeat devices.
- **Analytics & webhooks**: per-link conversions, D1/D7/D30 retention, HMAC-signed webhooks.

[DECISIONS.md](DECISIONS.md) · [CAVEATS.md](CAVEATS.md)

<table>
  <tr>
    <td width="20%"><img src="images/apps.webp" alt="Apps list"></td>
    <td width="20%"><img src="images/analytics.webp" alt="Analytics: clicks, installs via link, web fallbacks"></td>
    <td width="20%"><img src="images/charts.webp" alt="Daily activity and organic vs. non-organic installs"></td>
    <td width="20%"><img src="images/fraud.webp" alt="Fraud signals: tagged or active per signal, thresholds inline"></td>
    <td width="20%"><img src="images/settings.webp" alt="App settings: health, config, API key, matching"></td>
  </tr>
  <tr>
    <td align="center">Apps</td>
    <td align="center">Analytics</td>
    <td align="center">Charts</td>
    <td align="center">Fraud</td>
    <td align="center">Settings</td>
  </tr>
</table>

## Comparison (2026-10-07)

| | detur | godetour.dev | AppsFlyer |
|---|---|---|---|
| Cost & data ownership | Free & unlimited · Yours | Tiered · Vendor-hosted | Volume-based · Vendor-hosted |
| SDK | Detour (drop-in) | Detour (official) | AppsFlyer SDK |
| Matching & attribution | Detour weights (tunable) · 1:1 install | Detour weights · 1:1 install | Proprietary · Multi-touch |
| Routing rules & A/B splits | ✅ Rules & sticky splits | ❌ | ✅ Enterprise |
| Analytics | Events tracking | Events tracking | Events, revenue, cohorts |
| Fraud detection | ✅ Built-in | ❌ | ✅ Enterprise |
| MMP & ad networks | ❌ | ❌ | ✅ |
| Webhooks | ✅ HMAC-signed | ✅ | ✅ |
| Data API | ❌ | ❌ | ✅ |

## Quick start

> **Agent skill:** `npx skills add anh-ld/detur` ([skills/detur/](skills/detur/)) → automates steps 3–4.

### 1. Docker

```text
ghcr.io/anh-ld/detur:latest
```

```sh
docker run -d --restart unless-stopped --env-file .env \
  -v detur-data:/data \
  -p 8080:8080 -p 127.0.0.1:8081:8081 \
  ghcr.io/anh-ld/detur:0.15.1
```

- Prod: pin a version ([GitHub tags](https://github.com/anh-ld/detur/tags), no `v`).
- `-v`: no named volume → `docker rm` loses every app, key, install. Bind mount → writable by uid 1000.
- `127.0.0.1:8081`: portal has no login; bare `-p 8081:8081` → public (Docker bypasses ufw).

> Ports: `8080` (public SDK, links, `/health`) · `8081` (portal UI).

| Env | Required | Default | Description |
|---|---|---|---|
| `DOMAIN` | prod | `localhost` | Public domain |
| `DB_PATH` | no | `detur.db` (image: `/data/detur.db`) | SQLite path |
| `RETENTION_HOURS` | no | `24` | Click TTL (hours) |
| `CLICK_ID_DAYS` | no | `30` | Referrer TTL (days) |
| `TRUST_PROXY` | no | `0` | Trust reverse proxy |
| `TRUSTED_PROXIES` | no | private ranges | Trusted proxy CIDRs |
| `LOGOUT_URL` | no | empty | Sign-out URL |
| `ADMIN_PASSWORD` | no | empty | Admin password |
| `ADMIN_SESSION_HOURS` | no | `12` | Session length (hours) |
| `PORTAL_HOSTS` | gateway | empty | Portal hostnames behind a proxy |

Details → [.env.example](.env.example).

### 2. Create app

Portal: `:8081`.

- Create app; copy the API key (shown once).
- Links: `/key` → app route (`/product/42`) + optional iOS / Android / fallback.
- App details (iOS app ID, Android package, cert fingerprint) → well-known files.
- `ADMIN_PASSWORD` set → app changes need admin (top bar).

### 3. Patch SDK

SDK hardcodes 5 godetour.dev URLs (no `baseURL`), layout varies per version → give your AI agent:

```text
Point the installed @swmansion/react-native-detour at your detur server.

1. Patch node_modules/@swmansion/react-native-detour — BOTH copies:
   src/links/api/* + src/analytics/api/* (TS) and lib/module/*.js (compiled).
2. Replace the five endpoint base URLs with BASE_URL
   (dev: :8080, prod: https://<DOMAIN>). Keep /api/... paths.
3. Verify: grep -r "godetour.dev" node_modules/@swmansion/react-native-detour
   → zero matches. package.json author line is metadata — ignore.
4. Generate: npm/yarn app → npx patch-package @swmansion/react-native-detour;
   Bun app → bun patch @swmansion/react-native-detour before editing, then
   bun patch --commit node_modules/@swmansion/react-native-detour.
5. Survive fresh installs: npm/yarn → add "postinstall": "patch-package";
   Bun → patchedDependencies (written by bun patch --commit).
6. Report: changed files + patch file path.
```

Example (2.3.1 only, not a pin): [example/patch/](example/patch/).

### 4. Wire app

Full example: [example/expo-app/](example/expo-app/).

`+native-intent.tsx`, add your domain to `hosts`:

```tsx
import { createDetourNativeIntentHandler } from "@swmansion/react-native-detour/expo-router";

export const redirectSystemPath = createDetourNativeIntentHandler({
  fallbackPath: "",
  hosts: ["localhost"], // dev; add deployed domain, e.g. ["links.example.com"]
  config: {
    apiKey: process.env.EXPO_PUBLIC_DETOUR_API_KEY!,
    appID: process.env.EXPO_PUBLIC_DETOUR_APP_ID!,
  },
});
```

- Env: `EXPO_PUBLIC_DETOUR_API_KEY`, `EXPO_PUBLIC_DETOUR_APP_ID` (step 2).
- Base URL: Android emulator `http://10.0.2.2:8080`; device → host LAN IP (trusted LAN, `-p 8080:8080`); prod `https://<DOMAIN>`.

## Dev run

Prerequisites: Go 1.27, Bun 1.4.4.

```sh
./dev.sh
```

- Portal HMR `:8000` + Go reload via [air](https://github.com/air-verse/air).
- Tests: `cd server && go test ./...` · `cd portal && bun --bun run test`

## Credits

- [dub.sh](https://dub.sh): link-funnel server logic.
- [Detour matching docs](https://detour.swmansion.com/docs/platform/architecture/matching/): weights, threshold, window.
- [@swmansion/react-native-detour](https://github.com/software-mansion-labs/react-native-detour): the SDK.
