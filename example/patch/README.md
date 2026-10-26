# Patch: SDK → self-hosted

SDK **2.3.1** only. 5 constants: `https://godetour.dev` → `http://localhost:8080`.
Prod: your domain. Other versions: README step 3 prompt.

## Why

SDK `Config`: no base URL. Patch = only way.

## Changes

5 constants + clipboard URL guard × 2 copies (`src/*.ts` Metro, `lib/module/*.js` others) = 12 files:

| Feature | Change | File (`src/` .ts, `lib/module/` .js) |
|---|---|---|
| match-link | `API_URL` | `links/api/getDeferredLink` |
| resolve-short | `API_URL` | `links/api/resolveShortLink` |
| universal-link-click | `API_URL` | `links/api/sendUniversalLinkClick` |
| analytics event | `EVENT_API_URL` | `analytics/api/events` |
| analytics retention | `RETENTION_API_URL` | `analytics/api/retention` |
| iOS paste guard | `hasUrlAsync` / `hasWebURL` guard | `links/utils/fingerprint` |

Untouched:

- Host matcher (`*.godetour.link`): set in your `+native-intent.tsx`.
- `package.json` author line: metadata; grep still hits it.

## Formats

- Bun app: [`example/expo-app/patches/`](../expo-app/patches/), wired via `patchedDependencies` (`bun patch`).
- npm/yarn app: this folder's patch-package file + `"postinstall": "patch-package"`.

Same 12-file change, different path roots.

## Regenerate

Each SDK release (Bun 1.4.4):

```sh
bun init -y
bun add @swmansion/react-native-detour@2.3.1
bun patch @swmansion/react-native-detour
# edit the 12 files above: https://godetour.dev -> <your base URL> + paste guard in links/utils/fingerprint (copy from current patch)
bun patch --commit node_modules/@swmansion/react-native-detour
# -> patches/@swmansion%2Freact-native-detour@2.3.1.patch + patchedDependencies
```

patch-package file: needs an npm/yarn lockfile (no Bun support). Same edits, then `npx patch-package @swmansion/react-native-detour`.
