# detur example app

Minimal Expo (SDK 57) app running the patched `@swmansion/react-native-detour`
against a self-hosted detur server. Covers all five SDK calls:

- **match-link**: `DetourProvider` runs `getDeferredLink` on first launch.
  Home screen shows the matched link, or an organic install on no match
  (server returns 404, SDK returns null).
- **resolve-short and universal-link-click**: host matcher in
  `app/+native-intent.tsx` runs both for every self-hosted link it matches
  (resolve mode).
- **analytics event and retention**: home screen buttons call
  `DetourAnalytics.logEvent` and `logRetention`.

## Run

1. Start detur locally (default `http://localhost:8080`).
2. Create an app in the portal; copy its `appID` and `apiKey`.
3. `cp .env.example .env`, fill in `EXPO_PUBLIC_DETOUR_APP_ID` and
   `EXPO_PUBLIC_DETOUR_API_KEY`.
4. Install dependencies (also applies the patch):
   ```sh
   npm i
   # patch already in place via postinstall (patch-package)
   ```
   Patch file missing from `patches/`? Copy
   `../patch/@swmansion+react-native-detour+2.3.1.patch` into it.
5. `npx expo start`, open on a simulator or device.

## Notes

- Patch sets base URL to `http://localhost:8080`. Simulator reaches the
  host's localhost; a **physical device** needs the server's LAN IP. Edit
  `patches/@swmansion+react-native-detour+2.3.1.patch` (`localhost:8080` →
  `192.168.x.x:8080`), then reinstall to re-apply.
- Deferred flow needs a clean install: SDK records first launch in storage.
  Reinstall between test runs.
- Universal Links and App Links need HTTPS + `.well-known` files. Server
  serves them, but dev on `localhost` skips this path and tests only scheme
  and intent links.
