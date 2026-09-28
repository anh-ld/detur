# Patch: repoint react-native-detour to a self-hosted server

`@swmansion+react-native-detour+2.3.1.patch` — a patch-package patch that repoints the
five hardcoded SDK endpoint constants from `https://godetour.dev` to
`http://localhost:8080` (the local-server default; change it to your deployed
domain before shipping — see `docs/patch.md`).

## Why it exists

`react-native-detour`'s `Config` type has **no base-URL field**. All five endpoint
URLs are hardcoded to godetour.dev deep inside the API modules, so the only way to
point an app at a self-hosted server is to patch the installed package. This patch
is that patch. (An upstream PR adding a base-URL config field would retire it.)

## What it changes

Pinned version: **2.3.1** (scoped package `@swmansion/react-native-detour`).

Five constants, one line each, in **10 files** — the package ships both TypeScript
source and compiled output, and the package `exports` map resolves the
`react-native`/`source` conditions to `src/*.ts` (what Expo/Metro runs) while the
`default` condition falls back to `lib/module/*.js` (other bundlers). Both copies
are patched so every resolution path is covered:

| Endpoint | Constant | src/ file | lib/module/ file |
|---|---|---|---|
| match-link | `API_URL` | `src/links/api/getDeferredLink.ts` | `lib/module/links/api/getDeferredLink.js` |
| resolve-short | `API_URL` | `src/links/api/resolveShortLink.ts` | `lib/module/links/api/resolveShortLink.js` |
| universal-link-click | `API_URL` | `src/links/api/sendUniversalLinkClick.ts` | `lib/module/links/api/sendUniversalLinkClick.js` |
| analytics event | `EVENT_API_URL` | `src/analytics/api/events.ts` | `lib/module/analytics/api/events.js` |
| analytics retention | `RETENTION_API_URL` | `src/analytics/api/retention.ts` | `lib/module/analytics/api/retention.js` |

Deliberately NOT patched:

- The native-intent handler's default host matcher (`*.godetour.link`). The host
  matcher is per-app configuration — the app's `+native-intent.tsx` must list the
  self-host domain (see the example app and `docs/patch.md`).
- `package.json`'s author line (`https://godetour.dev/`) — metadata, not an
  endpoint. A `grep godetour.dev` over the installed package will still show it.

## How to regenerate

The patch is generated from a clean install of the pinned version:

```sh
npm init -y
npm i @swmansion/react-native-detour@2.3.1 patch-package
# edit the 10 files above: https://godetour.dev -> <your base URL>
npx patch-package @swmansion/react-native-detour
# -> patches/@swmansion+react-native-detour+2.3.1.patch
```

Regenerate on every upstream SDK release (the contract carries no stability
statement; the patch is pinned to 2.3.1 and may not apply to newer versions).