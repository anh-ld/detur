# Patch: repoint react-native-detour to a self-hosted server

Reference patch for SDK **2.3.1**. Changes five hardcoded endpoint constants
from `https://godetour.dev` to `http://localhost:8080` (dev default). Switch
to your deployed domain before shipping; recipe in repo `README.md`, step 3.

> [!NOTE]
> Reference only, not a version pin. File layout differs per SDK version; use
> the README's AI patch prompt for yours.

## Why

SDK `Config` has **no base-URL field**; its five endpoint URLs are hardcoded
to godetour.dev. Patching the installed package is the only way to point an
app at a self-hosted server. An upstream base-URL option would make the patch
unnecessary.

## What it changes

Five constants, one line each, across **10 files**. Package ships both
`src/*.ts` (Expo/Metro) and `lib/module/*.js` (other bundlers); patch changes
both copies:

| Endpoint | Constant | src/ | lib/module/ |
|---|---|---|---|
| match-link | `API_URL` | `links/api/getDeferredLink.ts` | `links/api/getDeferredLink.js` |
| resolve-short | `API_URL` | `links/api/resolveShortLink.ts` | `links/api/resolveShortLink.js` |
| universal-link-click | `API_URL` | `links/api/sendUniversalLinkClick.ts` | `links/api/sendUniversalLinkClick.js` |
| analytics event | `EVENT_API_URL` | `analytics/api/events.ts` | `analytics/api/events.js` |
| analytics retention | `RETENTION_API_URL` | `analytics/api/retention.ts` | `analytics/api/retention.js` |

Left unpatched on purpose:

- Host matcher (default `*.godetour.link`): per-app config. Your
  `+native-intent.tsx` lists the self-hosted domain (repo README, step 4).
- `package.json` author line (`https://godetour.dev/`): metadata, so grep
  still finds it.

## Regenerate

Regenerate on every upstream SDK release. SDK makes no stability promise;
patch may not apply to newer versions:

```sh
npm init -y
npm i @swmansion/react-native-detour@2.3.1 patch-package
# edit the 10 files above: https://godetour.dev -> <your base URL>
npx patch-package @swmansion/react-native-detour
# -> patches/@swmansion+react-native-detour+2.3.1.patch
```
