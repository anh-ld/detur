# Patch: repoint react-native-detour to a self-hosted server

This is a reference patch for SDK **2.3.1**. It changes five hardcoded
endpoint constants from `https://godetour.dev` to `http://localhost:8080`, the
dev default. Change it to your deployed domain before shipping; the repo
`README.md`, step 3, has the recipe.

> [!NOTE]
> This patch is a reference only, not a version pin. Each SDK version ships a
> different file layout, so use the README's AI patch prompt for your version.

## Why

The SDK's `Config` has **no base-URL field**, and its five endpoint URLs are
hardcoded to godetour.dev. Patching the installed package is the only way to
point an app at a self-hosted server. An upstream base-URL option would make
the patch unnecessary.

## What it changes

It changes five constants, one line each, across **10 files**. The package
ships both `src/*.ts` (used by Expo/Metro) and `lib/module/*.js` (used by
other bundlers), and the patch changes both copies:

| Endpoint | Constant | src/ | lib/module/ |
|---|---|---|---|
| match-link | `API_URL` | `links/api/getDeferredLink.ts` | `links/api/getDeferredLink.js` |
| resolve-short | `API_URL` | `links/api/resolveShortLink.ts` | `links/api/resolveShortLink.js` |
| universal-link-click | `API_URL` | `links/api/sendUniversalLinkClick.ts` | `links/api/sendUniversalLinkClick.js` |
| analytics event | `EVENT_API_URL` | `analytics/api/events.ts` | `analytics/api/events.js` |
| analytics retention | `RETENTION_API_URL` | `analytics/api/retention.ts` | `analytics/api/retention.js` |

Two things are deliberately left unpatched:

- The host matcher (default `*.godetour.link`) is per-app config. Your
  `+native-intent.tsx` lists the self-hosted domain instead (repo README,
  step 4).
- The `package.json` author line (`https://godetour.dev/`) is metadata, so a
  grep still finds it.

## Regenerate

Regenerate the patch on every upstream SDK release. The SDK makes no stability
promise, so the patch may not apply to newer versions:

```sh
npm init -y
npm i @swmansion/react-native-detour@2.3.1 patch-package
# edit the 10 files above: https://godetour.dev -> <your base URL>
npx patch-package @swmansion/react-native-detour
# -> patches/@swmansion+react-native-detour+2.3.1.patch
```
