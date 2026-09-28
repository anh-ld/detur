# Patch: point react-native-detour at a self-hosted server

Apply to your RN app. Repoints the SDK's five hardcoded godetour.dev endpoint
constants to your own server. App code untouched.

## Prerequisites

- Node + npm
- patch file: `patch/@swmansion+react-native-detour+2.3.1.patch` (from this repo)
- Running self-hosted server (this repo) — get `apiKey` + `appID` from the portal
- SDK pinned: **@swmansion/react-native-detour@2.3.1** (patch does not apply to
  other versions)

## Steps

1. INSTALL pinned SDK
   ```sh
   npm i @swmansion/react-native-detour@2.3.1
   ```
2. INSTALL patch-package (dev dep)
   ```sh
   npm i -D patch-package
   ```
3. PLACE patch file
   ```sh
   mkdir -p patches
   cp <path>/@swmansion+react-native-detour+2.3.1.patch patches/
   ```
4. APPLY (no package arg — that form creates patches, it does not apply)
   ```sh
   npx patch-package
   ```
   → "Applying patches... @swmansion/react-native-detour@2.3.1 ✔"
5. HOOK postinstall (patch re-applies on fresh installs)
   ```json
   "scripts": { "postinstall": "patch-package" }
   ```
6. RUN
   ```sh
   npx expo start
   ```
   CI: use `npx patch-package --error-on-fail` (exit 1 on apply failure).

## Change base URL

Patch defaults to `http://localhost:8080` (local-server dev). Before shipping,
repoint to deployed domain:

1. EDIT `patches/@swmansion+react-native-detour+2.3.1.patch`
   - replace all `http://localhost:8080` → `https://YOUR.DOMAIN`
   - 10 occurrences (5 endpoints × source + compiled copies)
2. REINSTALL clean package (patch applies on top)
   ```sh
   npm i @swmansion/react-native-detour@2.3.1
   npx patch-package
   ```

## Verify

- APPLIED files repointed — expect zero matches in code:
  ```sh
  grep -r godetour.dev node_modules/@swmansion/react-native-detour --include='*.ts' --include='*.js'
  ```
  → no output.
- KNOWN remaining hit: `package.json` author line (`https://godetour.dev/`) —
  metadata, not an endpoint. Safe to ignore.
- IDEMPOTENT re-run: `npx patch-package` again → ✔ no-op success (already-applied
  detected), exit 0. Full re-apply = reinstall node_modules first.
- RUNTIME check: watch server logs while launching app — five calls arrive at
  your domain (match-link, resolve-short, universal-link-click, event,
  retention).

## Expo host-matcher note

`+native-intent.tsx` (expo-router native intent) matches link hosts to decide
which URLs the app handles. SDK default matcher = `*.godetour.link` — self-host
links never match.

FIX in app, not patch:

```tsx
// app/+native-intent.tsx
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

See `example/app/+native-intent.tsx` for the full wiring.