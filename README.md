# detur

detur is a self-hosted backend for `@swmansion/react-native-detour` and a
drop-in replacement for the hosted godetour.dev. It runs deferred deep links on
your own infrastructure: a user taps a short link, installs the app, and on
first launch the app opens the link's destination.

## Why detur?

- There are no hosted pricing tiers or click limits. You run the server and
  keep the data.
- It is a drop-in replacement: it serves the same five SDK endpoints with the
  same matching, and the app points at your domain.
- It ships as one binary with Go, SQLite, and the portal. It needs no external
  services and makes no outbound calls.

## Quick start

### 1. Run the server

```sh
git clone <this-repo> && cd detur
docker build -t detur/detur:latest .
cp .env.example .env   # set DOMAIN, DB_PATH, RETENTION_HOURS, TRUST_PROXY
docker run -d --name detur --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -p 127.0.0.1:8081:8081 \
  -v detur-data:/data \
  --env-file .env \
  detur/detur:latest
```

| Env | Default |
|---|---|
| `DOMAIN` | `localhost` |
| `DB_PATH` | `/data/detur.db` |
| `RETENTION_HOURS` | `24` |
| `TRUST_PROXY` | `0` |

- The image is not published yet, so build it from source as above.
- To check that it is up, `GET /health` should return `200 ok`.
- Without Docker, run `cd server && go run ./cmd/detur`.
- In production, publish 8080 on loopback behind a TLS reverse proxy. Set
  `TRUST_PROXY=1` only when that proxy overwrites `X-Forwarded-For`. Otherwise
  direct callers can spoof the IP match signal.

### 2. Create an app in the portal

Open `http://127.0.0.1:8081`, then:

- Create an app. The portal shows the API key once, so copy it then.
- Add links (`/key` to a URL), with optional iOS, Android, and fallback URLs.
- Set the app details (iOS app ID, Android package, and certificate
  fingerprint). The server uses them to serve the well-known files.

### 3. Patch the SDK

The SDK hardcodes its five endpoint URLs to godetour.dev and has no `baseURL`
config field. The file layout changes between SDK versions, so give this
prompt to your AI agent and it will patch whichever version you installed:

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

[example/patch/@swmansion+react-native-detour+2.3.1.patch](example/patch/@swmansion+react-native-detour+2.3.1.patch)
is a worked example for SDK 2.3.1 only. It shows what the patch looks like; it
is not a version pin, and your version may differ.

### 4. Wire up the app

In `+native-intent.tsx`, list your domain in the host matcher:

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

- Set `EXPO_PUBLIC_DETOUR_API_KEY` and `EXPO_PUBLIC_DETOUR_APP_ID` from step 2.
- Run the app. On first launch it calls match-link and shows the matched link.
- The server logs should show all five calls arriving.
- A full example app is in `example/expo-app/`.
- On the Android emulator, use `http://10.0.2.2:8080`. On a physical device,
  use the deployed domain. For development on a trusted LAN only, you can
  publish 8080 on the host interface (`-p 8080:8080`) and use the host's LAN
  IP. In production, use `https://<DOMAIN>` through the TLS proxy.

## Credits

- [dub.sh](https://dub.sh) (dubinc/dub): the server logic is cloned from their
  production deep-link funnel.
- Software Mansion: [@swmansion/react-native-detour](https://github.com/software-mansion-labs/react-native-detour), the SDK this server serves.
- [kinu](https://github.com/developit/kinu) (developit): the portal UI toolkit.

Known limits are listed in [CAVEATS.md](CAVEATS.md).
