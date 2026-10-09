---
name: detur
description: Wire a React Native app to a self-hosted detur server (drop-in backend for @swmansion/react-native-detour). Use to set up, integrate, or fix detur / self-hosted Detour deep links (deferred, universal, install attribution) in an Expo Router or React Navigation app. Re-runnable after SDK upgrade or target change.
---

# detur

`@swmansion/react-native-detour` hardcodes 5 `https://godetour.dev/api/...` URLs. Patch them to your detur server, persist via `patch-package`, wire routing, set env vars, verify, and report.

Rules:
- Detect first; change only what is missing (re-run on a finished app = zero changes).
- Unknown setup or unexpected shape → stop and report. Never guess.
- Never touch the server. Probe `/health` and direct user to portal for settings.

Fallback: [README steps 3–4](https://github.com/anh-ld/detur#3-patch-sdk).

## 1. Detect

Inspect project before modifying. Stop if setup is unsupported, SDK is missing, or endpoint count ≠ 10:

| Check | Target / Rule |
|---|---|
| Setup | `expo-router` in `package.json` → Expo Router. `@react-navigation/native` + `NavigationContainer` → React Navigation. Neither → stop. |
| SDK | `node_modules/@swmansion/react-native-detour/package.json` `version`. |
| Lockfile | `package-lock.json` / `yarn.lock` / `pnpm-lock.yaml` (required by `patch-package`). |
| Patch | `patches/@swmansion+react-native-detour+*.patch`: target URL, applied vs unapplied. |
| Endpoints | Count `https://godetour.dev/api/` in SDK `src/` and `lib/module/` (expected: 10). |
| Persistence | `patch-package` in `devDependencies` + `postinstall` script in `package.json`. |
| Wiring | Expo: `+native-intent.tsx` + `_layout.tsx`. React Nav: root `DetourProvider` + linking prefixes. |
| Env | `EXPO_PUBLIC_DETOUR_APP_ID`, `EXPO_PUBLIC_DETOUR_API_KEY` set and gitignored. |
| App IDs | iOS bundle ID, Android package (`app.json`, `app.config.*`, native files). |

## 2. Target + server

Ask: *"Run where? iOS simulator, Android emulator, physical device, or production?"*

| Target | Patched Base URL | Host `/health` Probe |
|---|---|---|
| iOS simulator | `http://localhost:8080` | `http://localhost:8080/health` |
| Android emulator | `http://10.0.2.2:8080` | `http://localhost:8080/health` (10.0.2.2 = emulator alias) |
| Device | `http://<LAN_IP>:8080` | `http://localhost:8080/health` (`ipconfig getifaddr en0` / `hostname -I`) |
| Production | `https://<DOMAIN>` | `https://<DOMAIN>/health` |

Probe with `curl -fsS <probe>`. Stop if response is not `ok`.

## 3. Patch SDK

Skip if SDK files already contain this base URL and patch file matches.

1. Unapplied patch exists → run `npx patch-package` and re-verify.
2. Patch has different base URL → `npx patch-package --reverse` (reinstall SDK if it fails).
3. Verify exactly 10 occurrences of `https://godetour.dev/api/` in `src/` + `lib/module/` (2 copies each of `getDeferredLink`, `resolveShortLink`, `sendUniversalLinkClick`, `events`, `retention`). Any other count → stop.
4. `patch-package` missing → install as dev dependency.
5. Replace `https://godetour.dev` with base URL across all 10 files (keep `/api/...`, ignore author line).
6. Run `npx patch-package @swmansion/react-native-detour` and ensure `"postinstall": "patch-package"` in `package.json`.

## 4. Wire

### Expo Router

`app/+native-intent.tsx` (chain into existing handler if present):
```tsx
import { createDetourNativeIntentHandler } from "@swmansion/react-native-detour/expo-router";

const hosts = (process.env.EXPO_PUBLIC_DETOUR_HOSTS ?? "localhost")
  .split(",")
  .map((h) => h.trim())
  .filter(Boolean);

export const redirectSystemPath = createDetourNativeIntentHandler({
  fallbackPath: "",
  hosts,
  config: {
    apiKey: process.env.EXPO_PUBLIC_DETOUR_API_KEY ?? "",
    appID: process.env.EXPO_PUBLIC_DETOUR_APP_ID ?? "",
    timeoutMs: 1200,
  },
});
```

`app/_layout.tsx` (wrap root in `DetourProvider` for first-launch deferred match):
```tsx
import { DetourProvider } from "@swmansion/react-native-detour";

const detourConfig = {
  appID: process.env.EXPO_PUBLIC_DETOUR_APP_ID ?? "",
  apiKey: process.env.EXPO_PUBLIC_DETOUR_API_KEY ?? "",
  linkProcessingMode: "deferred-only", // +native-intent handles active links
} as const;

// <DetourProvider config={detourConfig}> ...root... </DetourProvider>
```

### React Navigation

Wrap `DetourProvider` around `NavigationContainer` and merge prefixes:
```tsx
import { DetourProvider, Detour, DETOUR_LINKING_PREFIX } from "@swmansion/react-native-detour";

const linking = {
  prefixes: [DETOUR_LINKING_PREFIX, ...existingPrefixes],
  config: existingConfig,
  getInitialURL: Detour.getInitialURL,
  subscribe: (listener) => Detour.addEventListener("url", ({ url }) => listener(url)).remove,
};
```

## 5. Env

1. Missing App ID or API key → ask user: *"Paste detur App ID and API key from portal (app settings)."*
2. Write to project env file (`.env.local` or `.env`):
   ```sh
   EXPO_PUBLIC_DETOUR_APP_ID=...
   EXPO_PUBLIC_DETOUR_API_KEY=...
   EXPO_PUBLIC_DETOUR_HOSTS=localhost
   ```
   `EXPO_PUBLIC_DETOUR_HOSTS`: comma-separated link hosts (`localhost`, `10.0.2.2`, LAN IP, domain).
3. If env file is not gitignored → stop and warn. Never commit API keys.

## 6. Verify

All must pass:
- 0 `godetour.dev` in SDK `src/` and `lib/module/` (except `package.json` author).
- 10 files contain base URL.
- Patch file exists; `patch-package` in `devDependencies`; `postinstall` runs it.
- Wiring in place for detected router.
- App ID, API key, and hosts set in env and gitignored.
- Type-check passes (if project has a script).

## 7. Report

```text
detur wired.

Changed: <files>
Patch: patches/@swmansion+react-native-detour+<version>.patch
Base URL: <url> (re-run with prod target before release)

Portal Checklist (link domain trust):
- iOS app ID: <Apple Team ID>.<bundle ID>
- Android package: <package>
- Android cert fingerprint: from signing keystore

Universal / App links (prod only):
- iOS: app.json "ios.associatedDomains": ["applinks:<DOMAIN>"]
- Android: app.json "android.intentFilters": autoVerify, https, host <DOMAIN>

Test:
1. Delete app from device/simulator.
2. Open detur link in browser → store / fallback page.
3. Install and launch dev build → first launch opens linked screen.
4. Verify click & install in portal analytics.
```
If already configured: "detur already set up. Nothing changed."

## Stops

| When | Say |
|---|---|
| SDK missing | "Detour SDK not installed. Run `npm install @swmansion/react-native-detour` and re-run." |
| Unsupported setup | "Neither Expo Router nor React Navigation detected. Set up manually via README steps 3–4." |
| `/health` fails | "detur unreachable at `<probe>`. Ensure server is running on target host/port, then re-run." |
| Endpoints ≠ 10 | "SDK `<version>` has `<n>` endpoint matches instead of 10. Manual patch required (README step 3)." |
| `patch-package` fails | "`patch-package` install failed: `<error>`. Fix dependencies and re-run." |
| Env not gitignored | "`<file>` is not gitignored. Add it to `.gitignore` before writing API key." |
