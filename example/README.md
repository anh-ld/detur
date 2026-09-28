# detur example app

Minimal Expo (SDK 57) app exercising the patched
`@swmansion/react-native-detour` against a self-hosted detur server.

Exercises:

- **match-link** — `DetourProvider` runs `getDeferredLink` on first launch;
  matched link shown on home screen, no-match (server 404 → SDK null) shown as
  organic install.
- **resolve-short + universal-link-click** — `app/+native-intent.tsx` host
  matcher runs both for every matched self-host link (resolve mode).
- **analytics/event + retention** — `DetourAnalytics.logEvent` /
  `logRetention` buttons on home screen.

## Run

1. Start the detur server locally (default `http://localhost:8080`).
2. Create an app in the portal → copy `appID` + `apiKey`.
3. `cp .env.example .env` → fill in `EXPO_PUBLIC_DETOUR_APP_ID`,
   `EXPO_PUBLIC_DETOUR_API_KEY`.
4. Install + apply patch:
   ```sh
   npm i
   # patch already in place via postinstall (patch-package)
   ```
   Patch file: copy `../patch/@swmansion+react-native-detour+2.3.1.patch`
   into this project's `patches/` if not present.
5. `npx expo start` → run on simulator/device.

## Notes

- Base URL defaults to `http://localhost:8080` (patch). Simulator reaches the
  host machine's localhost; a **physical device** needs the server's LAN IP —
  edit `patches/@swmansion+react-native-detour+2.3.1.patch`
  (`localhost:8080` → `192.168.x.x:8080`), reinstall, re-apply.
- Deferred flow needs a clean install (SDK marks first entrance in storage):
  reinstall app between test runs.
- Universal/App links require HTTPS + `.well-known` files — the server serves
  them; dev on `localhost` skips this path (scheme/intent testing only).