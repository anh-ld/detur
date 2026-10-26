# detur example app

Expo SDK 57 + patched `@swmansion/react-native-detour` → local detur. All 5 SDK calls:

- **match-link**: `DetourProvider` → `getDeferredLink` on first launch. Shows link, or organic (404 → null).
- **resolve-short, universal-link-click**: `app/+native-intent.tsx` host matcher (resolve mode).
- **event, retention**: home buttons → `DetourAnalytics.logEvent`, `logRetention`.

## Run

1. detur on `http://localhost:8080`.
2. Portal: create app, copy `appID` + `apiKey`.
3. `cp .env.example .env`, fill `EXPO_PUBLIC_DETOUR_APP_ID`, `EXPO_PUBLIC_DETOUR_API_KEY`.
4. `bun install` (Bun 1.4.4). Applies `patches/` via `patchedDependencies`.
5. `bun run ios` (Expo Go SDK 57) or `bun run start`.

## Notes

- Physical device: patch `localhost:8080` → LAN IP, reinstall.
- Deferred flow: clean install each run (SDK stores first launch).
- Universal/App Links need HTTPS + `.well-known`. `localhost` tests scheme/intent only.
