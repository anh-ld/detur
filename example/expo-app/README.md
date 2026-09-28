# detur example app

A minimal Expo (SDK 57) app that exercises the patched
`@swmansion/react-native-detour` against a self-hosted detur server. It covers
all five SDK calls:

- **match-link**: `DetourProvider` runs `getDeferredLink` on first launch. The
  home screen shows the matched link, or shows an organic install when there
  is no match (the server returns 404 and the SDK returns null).
- **resolve-short and universal-link-click**: the host matcher in
  `app/+native-intent.tsx` runs both for every self-hosted link it matches
  (resolve mode).
- **analytics event and retention**: buttons on the home screen call
  `DetourAnalytics.logEvent` and `logRetention`.

## Run

1. Start the detur server locally (default `http://localhost:8080`).
2. Create an app in the portal and copy its `appID` and `apiKey`.
3. Run `cp .env.example .env`, then fill in `EXPO_PUBLIC_DETOUR_APP_ID` and
   `EXPO_PUBLIC_DETOUR_API_KEY`.
4. Install dependencies, which also applies the patch:
   ```sh
   npm i
   # patch already in place via postinstall (patch-package)
   ```
   If this project's `patches/` folder does not have the patch file, copy
   `../patch/@swmansion+react-native-detour+2.3.1.patch` into it.
5. Run `npx expo start` and open the app on a simulator or device.

## Notes

- The patch sets the base URL to `http://localhost:8080`. A simulator reaches
  the host machine's localhost, but a **physical device** needs the server's
  LAN IP. Edit `patches/@swmansion+react-native-detour+2.3.1.patch` to change
  `localhost:8080` to `192.168.x.x:8080`, then reinstall so the patch is
  applied again.
- The deferred flow needs a clean install, because the SDK records the first
  launch in storage. Reinstall the app between test runs.
- Universal Links and App Links require HTTPS and the `.well-known` files. The
  server serves those files, but dev on `localhost` skips this path and only
  tests scheme and intent links.
