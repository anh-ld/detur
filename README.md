# detur

Self-hosted, drop-in replacement for godetour.dev — the cloud backend behind
`@swmansion/react-native-detour`. A Go + SQLite server answers the SDK's five
API calls, runs the deferred deep-link pipeline (short links → click recording
→ store redirect → first-launch matching → install attribution), hosts the
`.well-known` Universal/App-link files, and ships a thin kinu portal that
manages apps, links and API keys and reads back click/install analytics. One
binary, one container, SQLite file, nothing phones home (R18).

## Quick start

```sh
cd docker
docker compose up -d --build
```

- SDK + pipeline: `http://localhost:8080` (health: `GET /health`)
- Portal: `http://127.0.0.1:8081` (host loopback only — no auth, see guide)
- Create an app in the portal → get `apiKey` + `appID` → point your RN app
  there (patch story below)
- SQLite file lives in the `detur-data` volume; survives restarts/rebuilds
- Config: edit `docker/.env.example` before first up

Production = terminate TLS in front of 8080 and set `DETUR_DOMAIN` to a real
domain. Everything else: `docs/guide.md`.

## What the server does

Five SDK endpoints (godetour.dev-compatible shapes, R1–R4):

- `POST /v1/match-link` — first-launch fingerprint → destination, or 404
- `POST /v1/resolve-short` — short URL → destination
- `POST /v1/universal-link-click` — existing-user click report
- `POST /v1/events` — analytics events
- `POST /v1/retention` — analytics retention

Browser pipeline (R9–R11): `GET /{key}` short links record click + fingerprint
then 302 to App Store / Play Store (Android keeps the Play click-ID) or the
desktop fallback; `/.well-known/apple-app-site-association` + `assetlinks.json`
served per app; custom domains via `DETUR_EXTRA_DOMAINS`.

Matching (R5–R8): deterministic clickId first, then probabilistic scoring
(threshold 850, window 15 min, both configurable) — same contract as
godetour.dev. No match → 404 → organic install. Fail-open on backend errors.

Portal: apps + links CRUD, per-link threshold/window, API keys, click/install
readout (organic / non-organic). Separate listener, no built-in auth — access
control is a zero-trust boundary (R19, KTD5).

## Client patch

The SDK hardcodes godetour.dev — no base-URL field. `patch/` carries a
patch-package patch (pinned to SDK 2.3.1) repointing the five endpoint
constants to your server; app code untouched. Apply steps, base-URL change,
host matcher: `docs/patch.md`.

## Guide

Deploy, config, TLS, portal exposure, ops, security: `docs/guide.md`.

## Stack

- Go server, stdlib routing, pure-Go SQLite (modernc.org/sqlite) — static
  binary, no cgo
- Kinu portal (Preact + Vite), baked into the image, served by the binary
- Docker: multi-stage build, non-root runtime (uid 1000), alpine

## Status: v1

Working: five SDK endpoints, deferred matching, click pipeline, well-known
hosting, custom domains, portal, Docker deploy.

Deferred (not in v1): Platform API (management API), webhooks, billing /
click-limit enforcement, multi-tenant + SSO, full analytics (30-day views,
smart banners, custom redirect pages), native iOS / Android / Flutter patches
(RN only). An upstream PR adding a base-URL config to the SDK would retire
the patch.