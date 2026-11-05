---
name: detur
description: Wire an app to a self-hosted detur server, a drop-in backend for every Detour SDK (React Native, iOS, Android, Flutter). Use to set up, integrate, or fix self-hosted Detour deep links. Re-runnable after SDK upgrade or target change.
---

# detur

Every Detour SDK hardcodes 5 `https://godetour.dev/api/...` URLs. Swap the host for detur, keep paths, persist, wire, report.

Rules: detect first; change only what is missing; unexpected shape → stop and report; never touch the server.

## 1. Detect

| SDK | Signal | URLs in (hits) | Persist via |
|---|---|---|---|
| React Native | `@swmansion/react-native-detour` | `src/` + `lib/module/` (10) | `patch-package` + `postinstall`; Bun: `bun patch` |
| iOS | `Detour` pod / SPM | `DetourConstants.swift` (5) | Fork → SPM URL or `pod 'Detour', :git =>` |
| Android | `com.swmansion.detour:detour-sdk` | `api/*ApiClient.kt` (5) | Fork → `includeBuild` substituting `detour-sdk` |
| Flutter | `detour_flutter_plugin` | iOS + Android SDKs (5 + 5) | Both: `ios/Podfile`, `android/settings.gradle` |

No SDK, hit count off, or native SDK without a fork path → stop and ask.

## 2. Target

Ask: simulator, Android emulator, device, or prod? Base URL: `http://localhost:8080` · `http://10.0.2.2:8080` · `http://<LAN_IP>:8080` · `https://<DOMAIN>`. `curl -fsS <host>/health` must return `ok`, else stop.

## 3. Patch

Replace `https://godetour.dev` with the base URL in endpoint URLs only (skip metadata), then persist per the table. RN: patch both copies; reverse an old patch with a different URL first. Android `settings.gradle(.kts)`:

```kotlin
includeBuild("<android-detour>") {
    dependencySubstitution {
        substitute(module("com.swmansion.detour:detour-sdk")).using(project(":detour-sdk"))
    }
}
```

## 4. Wire

Follow the SDK's [Detour docs](https://detour.swmansion.com/docs) with detur `appID` + `apiKey` and your domain as link host:

- **Expo Router:** copy [`+native-intent.tsx` and `_layout.tsx`](https://github.com/anh-ld/detur/tree/main/example/expo-app/app); hosts from `EXPO_PUBLIC_DETOUR_HOSTS`.
- **React Navigation:** wrap `NavigationContainer` in `DetourProvider`; add `DETOUR_LINKING_PREFIX`, `Detour.getInitialURL`, `Detour.addEventListener("url")` to `linking`.
- **iOS:** `applinks:<DOMAIN>`. **Android:** `autoVerify` https intent filter, `host="<DOMAIN>"`. **Flutter:** both.

Credentials: ask for missing ones; store in a gitignored file (`.env`, xcconfig, `local.properties`, `--dart-define-from-file`). Not gitignored → stop.

## 5. Verify + report

Check: `godetour.dev` only in metadata; change survives clean install; link host matches target; build passes.

Report: changed files, persistence, base URL (re-run for prod), portal checklist (iOS `<TeamID>.<bundleID>`, Android package + cert fingerprint), and test: delete app → open link → install → first launch opens linked screen → check portal analytics.

Nothing to change → "detur already set up."
