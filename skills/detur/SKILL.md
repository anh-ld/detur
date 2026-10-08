---
name: detur
description: Wire a React Native app to a self-hosted detur server (drop-in backend for @swmansion/react-native-detour). Use to set up, integrate, or fix detur / self-hosted Detour deep links (deferred, universal, install attribution) in an Expo Router or React Navigation app. Re-runnable after SDK upgrade or target change.
---

# detur

SDK hardcodes 5 `https://godetour.dev/api/...` URLs, no base URL option. Patch them, persist patch, wire app, set env, verify, report.

Rules:

- Detect first. Change only what's missing. Re-run on a done app = no changes.
- Unknown shape → stop, say what you found. Never guess.
- Never touch the server. Check `/health`, tell user what to set in portal.

Manual fallback: [README](https://github.com/anh-ld/detur#3-patch-sdk) steps 3–4.

## 1. Detect

Before any change. Track done vs missing.

| Check | Look at |
|---|---|
| Setup | `expo-router` in `package.json` → Expo Router. `@react-navigation/native` + `NavigationContainer` → React Navigation. Neither → unsupported. |
| SDK | `node_modules/@swmansion/react-native-detour/package.json` `version` |
| Lockfile | `package-lock.json` / `yarn.lock` / `pnpm-lock.yaml`. `patch-package` needs one. |
| Patch | `patches/@swmansion+react-native-detour+*.patch`: base URL inside; applied (SDK files hold it) or not (still `godetour.dev`) |
| Endpoints | Unpatched SDK: count `https://godetour.dev/api/` in `src/` + `lib/module/` (step 3) |
| Persistence | `patch-package` in `devDependencies`; `postinstall` runs it |
| Wiring | Expo Router: `app/+native-intent.tsx` with `createDetourNativeIntentHandler` + `DetourProvider` in `app/_layout.tsx`. React Navigation: `DetourProvider` around app, `DETOUR_LINKING_PREFIX` in `linking` |
| Env | `.env`, `.env.local`, `.env.example`, `app.config.*`: naming (Expo: `EXPO_PUBLIC_*`), app ID + API key set? |
| Secrets | Env file gitignored? |
| App IDs | iOS bundle ID, Android package: `app.json` / `app.config.*`, else `ios/*.xcodeproj`, `android/app/build.gradle` |

Stops, first match wins: unsupported setup → SDK missing → endpoint count wrong. All before any change or credential prompt.

## 2. Target + server

Ask: "Run where? iOS simulator, Android emulator, device, or production?"

| Target | Base URL (patched in) | `/health` probe (from host) |
|---|---|---|
| iOS simulator | `http://localhost:8080` | `http://localhost:8080/health` |
| Android emulator | `http://10.0.2.2:8080` | `http://localhost:8080/health`. 10.0.2.2 = emulator-only alias |
| Device | `http://<LAN IP>:8080` | same |
| Production | `https://<DOMAIN>` | same |

- LAN IP: `ipconfig getifaddr en0` (macOS), `hostname -I` (Linux).
- Non-default port/domain → ask.
- `curl -fsS <probe>`. Not `ok` → stop.

## 3. Patch SDK

Skip if SDK files already hold this base URL and patch file matches.

1. Patch file exists, not applied (fresh clone, no postinstall) → `npx patch-package`, re-check.
2. Applied patch has another base URL → `npx patch-package --reverse`. Fails → reinstall SDK. Files must say `godetour.dev` again before counting.
3. Count `https://godetour.dev/api/` in `src/` + `lib/module/`. SDK 2.3.1: exactly 10 (5 endpoints × 2 copies):

   | Endpoint | File (`src/**.ts`, `lib/module/**.js`) |
   |---|---|
   | `/api/link/match-link` | `links/api/getDeferredLink` |
   | `/api/link/resolve-short` | `links/api/resolveShortLink` |
   | `/api/link/universal-link-click` | `links/api/sendUniversalLinkClick` |
   | `/api/analytics/event` | `analytics/api/events` |
   | `/api/analytics/retention` | `analytics/api/retention` |

   Anything else → stop. Patch nothing.
4. `patch-package` missing → install as dev dep with app's package manager. Before editing `node_modules`: installs can restore SDK files. No lockfile → install creates one. Install fails → stop.
5. Replace `https://godetour.dev` → base URL in the 10 files. Keep `/api/...`. Ignore `package.json` author line.
6. `npx patch-package @swmansion/react-native-detour` (needs network: fetches clean SDK). No `postinstall` → add `"postinstall": "patch-package"`. Existing → append `&& patch-package`.

## 4. Wire

Add only what's missing. Match app style.

### Expo Router

`app/+native-intent.tsx`: links that open the app.

```tsx
import { createDetourNativeIntentHandler } from "@swmansion/react-native-detour/expo-router";

const hosts = (process.env.EXPO_PUBLIC_DETOUR_HOSTS ?? "localhost")
  .split(",")
  .map((host) => host.trim())
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

Existing `redirectSystemPath` → chain into it, keep its logic.

`app/_layout.tsx`: wrap root in `DetourProvider`. Runs first-launch deferred match. Without it: no install match.

```tsx
import { DetourProvider } from "@swmansion/react-native-detour";

const detourConfig = {
  appID: process.env.EXPO_PUBLIC_DETOUR_APP_ID ?? "",
  apiKey: process.env.EXPO_PUBLIC_DETOUR_API_KEY ?? "",
  linkProcessingMode: "deferred-only", // +native-intent handles the rest
} as const;

// <DetourProvider config={detourConfig}> ...existing root... </DetourProvider>
```

### React Navigation

`DetourProvider` above `NavigationContainer` (default mode: deferred + universal + scheme). Feed links to navigator:

```tsx
import { DetourProvider, Detour, DETOUR_LINKING_PREFIX } from "@swmansion/react-native-detour";

const linking = {
  prefixes: [DETOUR_LINKING_PREFIX, ...existingPrefixes],
  config: existingConfig,
  getInitialURL: Detour.getInitialURL,
  subscribe: (listener) => Detour.addEventListener("url", ({ url }) => listener(url)).remove,
};
```

Merge into existing `linking`, don't replace. `appID` / `apiKey` from app's env convention.

## 5. Env

App ID or API key missing → ask: "Paste detur app ID + API key. Portal → app. API key shown once, at creation."

Both set → keep. Re-ask only if user says they changed.

Write to app's env file, its naming. None → create `.env`, follow `.env.example` if present. Defaults:

```sh
EXPO_PUBLIC_DETOUR_APP_ID=...
EXPO_PUBLIC_DETOUR_API_KEY=...
EXPO_PUBLIC_DETOUR_HOSTS=localhost
```

`EXPO_PUBLIC_DETOUR_HOSTS`: comma-separated link hosts the app opens. Set to base URL host for target (`localhost`, `10.0.2.2`, LAN IP, domain). Keep other hosts (e.g. short-link domain).

Not gitignored → stop, ask first. Never commit the key. Never echo it.

## 6. Verify

All must pass:

- 0 `godetour.dev` in SDK `src/` + `lib/module/` (`package.json` author line excluded)
- 10 files hold base URL
- Patch file exists; `patch-package` dev dep; `postinstall` runs it
- Step 4 wiring in place for detected setup
- Env has app ID, API key, hosts; gitignored
- Type-check passes, if project has a script

## 7. Report

```text
detur wired.

Changed: <files>
Patch: patches/@swmansion+react-native-detour+<version>.patch
Base URL: <url>. Release build → re-run with production target first.

Portal → app settings (link domain trust):
- iOS app ID: <TEAMID>.<bundle ID>. Team ID: Apple Developer → Membership
- Android package: <package>
- Android SHA-256 cert fingerprint: from signing key

<production only>
App config, else links open in browser:
- iOS: "associatedDomains": ["applinks:<DOMAIN>"] under "ios"
- Android: "intentFilters" under "android": autoVerify, https, host <DOMAIN>
- Bare: Associated Domains entitlement + AndroidManifest intent filter

Test:
1. Delete app from device.
2. Open a detur link in browser → store / fallback page.
3. Install + open dev build.
4. First launch → linked screen.
5. Portal analytics → click + install.
```

Nothing changed → "detur already set up. Nothing changed."

## Stops

Say what failed, why, next step. Change nothing else.

| When | Say |
|---|---|
| SDK missing | "Detour SDK not installed. Install `@swmansion/react-native-detour`, re-run." |
| Unsupported setup | "No Expo Router or React Navigation: no SDK entry point. Nothing changed. Patch + wire by hand: README steps 3–4." |
| `/health` fails | "detur unreachable at `<probe>`. Server down, or wrong host/port for target. Fix, re-run." |
| Endpoints ≠ 10 | "SDK `<version>` doesn't match the 5 known endpoints. Found `<n>`: `<list>`. Nothing patched. Patch by hand (README step 3) or update this skill." |
| `patch-package` install fails | "`patch-package` install failed: `<error>`. SDK unchanged. Fix install (often peer deps), re-run." |
| Env not gitignored | "`<file>` not gitignored: API key could be committed. Add to `.gitignore`, or say write anyway." |
