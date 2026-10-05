# Patch: SDK → self-hosted

SDK **2.3.1** only. 5 constants: `https://godetour.dev` → `http://localhost:8080`.
Prod: your domain. Other versions: README step 3 prompt.

## Why

SDK `Config`: no base URL. Patch = only way.

## Changes

5 constants × 2 copies (`src/*.ts` Metro, `lib/module/*.js` others) = 10 files:

| Endpoint | Constant | File (`src/` .ts, `lib/module/` .js) |
|---|---|---|
| match-link | `API_URL` | `links/api/getDeferredLink` |
| resolve-short | `API_URL` | `links/api/resolveShortLink` |
| universal-link-click | `API_URL` | `links/api/sendUniversalLinkClick` |
| analytics event | `EVENT_API_URL` | `analytics/api/events` |
| analytics retention | `RETENTION_API_URL` | `analytics/api/retention` |

Untouched:

- Host matcher (`*.godetour.link`): set in your `+native-intent.tsx`.
- `package.json` author line: metadata; grep still hits it.

## Regenerate

Each SDK release:

```sh
npm init -y
npm i @swmansion/react-native-detour@2.3.1 patch-package
# edit the 10 files above: https://godetour.dev -> <your base URL>
npx patch-package @swmansion/react-native-detour
# -> patches/@swmansion+react-native-detour+2.3.1.patch
```
