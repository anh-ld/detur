# detur

- Self-hosted backend for `@swmansion/react-native-detour` — drop-in
  replacement for hosted godetour.dev
- Deferred deep links on your infra: short link → click → install → first
  launch → matched destination

## Why detur?

- NO hosted pricing tiers / click limits — own server, own data
- Drop-in — same 5 SDK endpoints, same matching; app points at your domain
- One binary: Go + SQLite + portal. No external services, nothing phones home

## Quick start — 4 steps to a running app

### 1 · Run the server

```sh
docker run -d --name detur \
  -p 8080:8080 \
  -p 127.0.0.1:8081:8081 \
  -v detur-data:/data \
  -e DOMAIN=localhost \
  -e ADDR=:8080 \
  -e DB_PATH=/data/detur.db \
  -e RETENTION_HOURS=24 \
  detur/detur:latest
```

- Image not published yet → build once:
  `git clone <this-repo> && cd detur && docker build -f docker/Dockerfile -t detur/detur:latest .`
- Check: `GET /health` → `200 ok`
- No Docker? `cd server && go run ./cmd/detur`
- Prod: HTTPS in front of 8080 + `-e TRUST_PROXY=1`

### 2 · Portal — create app, get key

Open `http://127.0.0.1:8081`:

- CREATE app → API key shown ONCE → copy
- ADD links (`/key` → URL) + iOS/Android/fallback
- SET app details (iOS app ID, Android package + cert fingerprint) → well-known files

### 3 · Patch the lib

SDK hardcodes 5 endpoint URLs to godetour.dev — no `baseURL` config field.
Version-dependent → hand this prompt to your AI agent, it patches whatever
version you installed:

```text
PATCH PROMPT — paste to your AI agent:
You are patching @swmansion/react-native-detour in this app to point at a
self-hosted detur server. The SDK hardcodes its five API endpoint URLs to
https://godetour.dev. Do this:

1. Find the installed package: node_modules/@swmansion/react-native-detour.
   The five endpoint constants live in src/links/api/* and
   src/analytics/api/* (TypeScript source) AND lib/module/*.js (compiled) —
   patch BOTH copies.
2. Replace the base URL in all five constants with BASE_URL
   (dev: http://localhost:8080, prod: https://<DOMAIN>). Keep the
   /api/... paths unchanged.
3. Verify: grep -r "godetour.dev" node_modules/@swmansion/react-native-detour
   — code must show zero matches (package.json author line is metadata,
   ignore it).
4. Generate the patch: npx patch-package @swmansion/react-native-detour
5. Make it survive fresh installs: add "postinstall": "patch-package" to
   package.json.
6. Report back: the exact files and constants you changed, and the patch
   file path.
```

- [example/patch/@swmansion+react-native-detour+2.3.1.patch](example/patch/@swmansion+react-native-detour+2.3.1.patch)
  = the 2.3.1 worked reference ONLY — shows the shape, never a pin; your
  version may differ

### 4 · Apply the lib

Wire the app (`+native-intent.tsx` — host matcher lists your domain):

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

- SET `EXPO_PUBLIC_DETOUR_API_KEY` + `EXPO_PUBLIC_DETOUR_APP_ID` (from step 2)
- RUN app → first launch calls match-link → matched link shown
- CHECK server logs → five calls arrive
- Full example app: `example/expo-app/`
- Devices: Android emulator → `http://10.0.2.2:8080`; physical device → LAN IP
  or deployed domain; prod → `https://<DOMAIN>`

## Credits

- [dub.sh](https://dub.sh) (dubinc/dub) — server logic cloned from their
  production deep-link funnel
- Software Mansion — [@swmansion/react-native-detour](https://github.com/software-mansion-labs/react-native-detour), the SDK this server serves
- [kinu](https://github.com/developit/kinu) (developit) — portal UI toolkit

Caveats & limits: [CAVEATS.md](CAVEATS.md)