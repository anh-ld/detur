# detur

Self-hosted backend for `@swmansion/react-native-detour`. Drop-in for
[godetour](https://godetour.dev).

Deferred deep links + analytics: tap link → install → first launch opens link. Clicks, installs, events tracked.

- **Single binary**: Go + SQLite + Preact portal. ~30 MB RAM, <0.1 vCPU. Zero outbound calls.
- **Link rules & A/B splits**: Route by platform, language, query, or date. Sticky weighted splits.
- **In-app browser bypass**: Tap-through page for Messenger, TikTok, Zalo so users never get stuck.
- **Fraud protection**: Detect & block click floods, bots, datacenter IPs, and repeat devices.
- **Analytics & webhooks**: Per-link conversions, D1/D7/D30 retention, and HMAC-signed webhook delivery.

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

> **Agent skill:** `npx skills add anh-ld/detur` (or [skills/detur/](skills/detur/)) automates steps 3–4.

### 1. Docker

```text
ghcr.io/anh-ld/detur:latest
```

Prod: pin version (`:0.1.0`).

> Ports: `8080` (public SDK, links, `/health`) · `8081` (portal UI).

| Env | Required | Default | Description |
|---|---|---|---|
| `DOMAIN` | prod | `localhost` | Public domain |
| `DB_PATH` | no | `/data/detur.db` | SQLite path |
| `RETENTION_HOURS` | no | `24` | Click TTL (hours) |
| `CLICK_ID_DAYS` | no | `30` | Referrer TTL (days) |
| `TRUST_PROXY` | no | `0` | Trust reverse proxy |
| `LOGOUT_URL` | no | empty | Sign-out URL |
| `ADMIN_PASSWORD` | no | empty | Admin password |
| `ADMIN_SESSION_HOURS` | no | `12` | Session length (hours) |
| `PORTAL_HOSTS` | gateway | empty | Portal hostnames behind a proxy |

See [.env.example](.env.example) for detailed explanations.

### 2. Create app

Portal: `:8081`.

- Create app. API key shown once: copy.
- Add links: `/key` → URL, optional iOS / Android / fallback.
- App details: iOS app ID, Android package, cert fingerprint → well-known
  files.
- `ADMIN_PASSWORD` set → app changes need admin (top bar).

### 3. Patch SDK

SDK hardcodes 5 URLs to godetour.dev, no `baseURL`. Layout varies per
version → give your AI agent:

```text
Point the installed @swmansion/react-native-detour at your detur server.

1. Patch node_modules/@swmansion/react-native-detour — BOTH copies:
   src/links/api/* + src/analytics/api/* (TS) and lib/module/*.js (compiled).
2. Replace the five endpoint base URLs with BASE_URL
   (dev: :8080, prod: https://<DOMAIN>). Keep /api/... paths.
3. Verify: grep -r "godetour.dev" node_modules/@swmansion/react-native-detour
   → zero matches. package.json author line is metadata — ignore.
4. Generate: npx patch-package @swmansion/react-native-detour.
5. Survive fresh installs: add "postinstall": "patch-package" to package.json.
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

- Env: `EXPO_PUBLIC_DETOUR_API_KEY`, `EXPO_PUBLIC_DETOUR_APP_ID` (from step 2).
- Base URL by target: Android emulator `http://10.0.2.2:8080`, device (trusted LAN dev: `-p 8080:8080`, host LAN IP), prod `https://<DOMAIN>`.

## Dev run

```sh
./dev.sh
```

- Portal HMR on `:8000` + Go server reload via [air](https://github.com/air-verse/air).
- Tests: `cd server && go test ./...` · `cd portal && npm test`

## Credits

- [dub.sh](https://dub.sh): server logic from their link funnel.
- [Detour matching docs](https://detour.swmansion.com/docs/platform/architecture/matching/): weights, threshold, window.
- [@swmansion/react-native-detour](https://github.com/software-mansion-labs/react-native-detour): the SDK.
