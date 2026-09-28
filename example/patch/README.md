# Patch: repoint react-native-detour to a self-hosted server

Reference patch for **2.3.1** — repoints five hardcoded endpoint constants
from `https://godetour.dev` to `http://localhost:8080` (dev default; change
to deployed domain before shipping — recipe: repo `README.md`, section 3).

> [!NOTE]
> Reference ONLY, never a pin. Every SDK version ships a different layout —
> use the README's AI patch prompt for your version.

## Why

`Config` has **no base-URL field**. Five endpoint URLs hardcoded to
godetour.dev. Only way to point an app at a self-hosted server: patch the
installed package. (Upstream base-URL PR would retire it.)

## What it changes

Five constants, one line each, **10 files** — package ships both `src/*.ts`
(Expo/Metro) + `lib/module/*.js` (other bundlers). Both copies patched:

| Endpoint | Constant | src/ | lib/module/ |
|---|---|---|---|
| match-link | `API_URL` | `links/api/getDeferredLink.ts` | `links/api/getDeferredLink.js` |
| resolve-short | `API_URL` | `links/api/resolveShortLink.ts` | `links/api/resolveShortLink.js` |
| universal-link-click | `API_URL` | `links/api/sendUniversalLinkClick.ts` | `links/api/sendUniversalLinkClick.js` |
| analytics event | `EVENT_API_URL` | `analytics/api/events.ts` | `analytics/api/events.js` |
| analytics retention | `RETENTION_API_URL` | `analytics/api/retention.ts` | `analytics/api/retention.js` |

NOT patched (deliberately):

- Host matcher (`*.godetour.link` default) — per-app config; `+native-intent.tsx`
  lists the self-host domain (repo README, section 3)
- `package.json` author line (`https://godetour.dev/`) — metadata; grep still
  shows it

## Regenerate

On every upstream SDK release (no stability statement; patch may not apply to
newer versions):

```sh
npm init -y
npm i @swmansion/react-native-detour@2.3.1 patch-package
# edit the 10 files above: https://godetour.dev -> <your base URL>
npx patch-package @swmansion/react-native-detour
# -> patches/@swmansion+react-native-detour+2.3.1.patch
```