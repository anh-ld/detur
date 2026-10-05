# detur

Self-hosted backend for `@swmansion/react-native-detour`. Drop-in for
[godetour.dev](https://godetour.dev).

Deferred deep links + analytics: tap link → install → first launch opens link. Clicks, installs, events tracked.

<table>
  <tr>
    <td width="25%"><img src="images/apps.webp" alt="Apps list"></td>
    <td width="25%"><img src="images/analytics.webp" alt="Analytics: clicks, installs via link, web fallbacks"></td>
    <td width="25%"><img src="images/charts.webp" alt="Daily activity and organic vs. non-organic installs"></td>
    <td width="25%"><img src="images/settings.webp" alt="App settings: health, config, API key, matching"></td>
  </tr>
  <tr>
    <td align="center">Apps</td>
    <td align="center">Analytics</td>
    <td align="center">Charts</td>
    <td align="center">Settings</td>
  </tr>
</table>

## Why

- No tiers, no click limits. Your data.
- Same 5 SDK endpoints, same matching.
- One binary: Go + SQLite + portal. No outbound calls.

## 1. Run

```sh
docker run -d --name detur --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -p 127.0.0.1:8081:8081 \
  -v detur-data:/data \
  -e DOMAIN=links.example.com \
  ghcr.io/anh-ld/detur:latest
```

| Env | Required | Default |
|---|---|---|
| `DOMAIN` | prod | `localhost` |
| `DB_PATH` | no | `/data/detur.db` (Docker), `detur.db` (bare) |
| `RETENTION_HOURS` | no | `24` |
| `CLICK_ID_DAYS` | no | `30` (1–90) |
| `TRUST_PROXY` | no | `0` |
| `LOGOUT_URL` | no | empty = no Log out link |

- Min: 1 vCPU, 512 MB, 1 GB disk.
- Prod: pin version (`:0.1.0`), `8080` on loopback behind TLS proxy.
- Health: `GET /health` → `200 ok`.
- Build: `docker build -t ghcr.io/anh-ld/detur:latest .`
- No Docker: `cd server && go run ./cmd/detur`.
- Dev: `./dev.sh`. Portal HMR `:8000`, Go rebuild via [air](https://github.com/air-verse/air).
- `TRUST_PROXY=1`: real proxy only. Trusts rightmost `X-Forwarded-For`, else
  `X-Real-IP`. Proxy must append XFF (or overwrite `X-Real-IP`). No proxy =
  IP spoofable.
- `LOGOUT_URL`: any gateway sign-out URL or `/path`, e.g. `/cdn-cgi/access/logout` (Cloudflare), etc.

## 2. Create app

Portal: `http://127.0.0.1:8081`.

- Create app. API key shown once: copy.
- Add links: `/key` → URL, optional iOS / Android / fallback.
- App details: iOS app ID, Android package, cert fingerprint → well-known
  files.

## 3. Patch SDK

SDK hardcodes 5 URLs to godetour.dev, no `baseURL`. Layout varies per
version → give your AI agent:

```text
Point the installed @swmansion/react-native-detour at your detur server.

1. Patch node_modules/@swmansion/react-native-detour — BOTH copies:
   src/links/api/* + src/analytics/api/* (TS) and lib/module/*.js (compiled).
2. Replace the five endpoint base URLs with BASE_URL
   (dev: http://localhost:8080, prod: https://<DOMAIN>). Keep /api/... paths.
3. Verify: grep -r "godetour.dev" node_modules/@swmansion/react-native-detour
   → zero matches. package.json author line is metadata — ignore.
4. Generate: npx patch-package @swmansion/react-native-detour.
5. Survive fresh installs: add "postinstall": "patch-package" to package.json.
6. Report: changed files + patch file path.
```

Example (2.3.1 only, not a pin): [example/patch/](example/patch/).

## 4. Wire app

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
- First launch → match-link → matched link shown.
- Server logs errors only. Clicks/installs: portal analytics.
- Base URL by target:
  - Android emulator: `http://10.0.2.2:8080`
  - Device, trusted LAN dev: `-p 8080:8080`, host LAN IP
  - Prod: `https://<DOMAIN>`

## Credits

- [dub.sh](https://dub.sh): server logic from their link funnel.
- [Detour matching docs](https://detour.swmansion.com/docs/platform/architecture/matching/): weights, threshold, window.
- [@swmansion/react-native-detour](https://github.com/software-mansion-labs/react-native-detour): the SDK.
- [kinu](https://github.com/developit/kinu): portal UI.

[DECISIONS.md](DECISIONS.md) · [CAVEATS.md](CAVEATS.md)
