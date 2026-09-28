# detur — operator guide

Deploy, config, client patch, portal zero-trust, ops, security. Telegraphic on
purpose — read in order. Prereq: Docker + compose; no other services needed
(SQLite in-process).

## Deploy

```sh
cd docker
# edit .env.example first: DETUR_DOMAIN etc. (all defaults safe for localhost)
docker compose up -d --build
```

- SDK + pipeline: `http://localhost:8080`
- Portal: `http://127.0.0.1:8081` (host loopback only)
- Health: `GET /health` → `200 ok`
- Upgrade: `docker compose up -d --build` (image rebuild) or
  `docker compose pull && docker compose up -d`

### TLS (production requirement)

Bearer apiKey + device fingerprints must never go over plain http (R3/R4).
Set `DETUR_DOMAIN` to a real domain and terminate TLS in front of 8080.
Caddy — uncomment the `caddy` service in `docker/docker-compose.yml` and
create `docker/Caddyfile`:

```
links.yourdomain.com {
    reverse_proxy detur:8080
}
```

Then change detur's 8080 mapping in compose to `127.0.0.1:8080:8080` so only
caddy faces the network. Do NOT reverse-proxy the portal route publicly —
keep it loopback + Tailscale/Cloudflare Access (Portal & zero-trust).

Then point the client patch at `https://links.yourdomain.com` (Client patch).

### Custom domains

- One domain = `DETUR_DOMAIN`.
- More = `DETUR_EXTRA_DOMAINS=links.example.com,links.other.com`
  (comma-separated, same instance).
- Each custom domain: own TLS cert + client-side host-matcher entry
  (docs/patch.md, `+native-intent.tsx` `hosts` array).
- Short links, redirects, well-known files all served under every listed
  domain; unknown hosts → 404.

### Data

- SQLite file at `/data/detur.db` → named volume `detur-data`.
- Survives restarts + rebuilds.
- Backup: `docker compose exec detur cp /data/detur.db /data/backup.db`, then
  copy out. Restore: replace the file, restart. Stop the container first for a
  clean WAL checkpoint.

## Config

Env only — no config file. All vars, one line each (full comments in
`docker/.env.example`):

| Var | Default | Meaning |
|---|---|---|
| `DETUR_DOMAIN` | `localhost` | public domain: links, redirects, well-known |
| `DETUR_ADDR` | `:8080` | SDK + pipeline listen address |
| `DETUR_PORTAL_ADDR` | `0.0.0.0:8081` | portal listener (all-ifaces inside container; compose publishes loopback only) |
| `DETUR_PORTAL_DIR` | `/app/portal-dist` | portal static files (baked into image) |
| `DETUR_DB_PATH` | `/data/detur.db` | SQLite file (volume) |
| `DETUR_RETENTION_HOURS` | `24` | click + event retention floor, 1–8760 |
| `DETUR_EXTRA_DOMAINS` | empty | extra comma-separated domains |
| `DETUR_PORTAL_HOSTS` | empty | extra Host values the portal guard accepts (zero-trust tunnels) |
| `DETUR_TRUST_PROXY` | `0` | `1` = honor X-Forwarded-For (set only behind a trusted TLS proxy) |

Non-container deploys: set `DETUR_PORTAL_ADDR=127.0.0.1:8081` (strict loopback
bind).

## Client patch

Pointer: `docs/patch.md` — full steps. Summary:

1. `npm i @swmansion/react-native-detour@2.3.1` + `npm i -D patch-package`
2. COPY `patch/@swmansion+react-native-detour+2.3.1.patch` → app `patches/`
3. `npx patch-package` → applies to pinned SDK
4. REPOINT patch: `http://localhost:8080` → `https://<DETUR_DOMAIN>` (10
   occurrences) before shipping
5. HOST MATCHER: add the domain to `+native-intent.tsx` `hosts` array

App code untouched; patch re-applies via `postinstall`.

## Portal & zero-trust

Portal = no built-in auth by design (R19). Whoever reaches it can read/change
everything: apps, links, API keys, redirect targets.

Blast radius if exposed publicly:

- API-key exfiltration — keys SHA-256 hashed at rest and shown once at
  creation, but a live attacker reads new keys as they are minted
- Redirect retargeting — change a link's destination mid-campaign
- Link inventory + click data exposure

Boundary controls (pick one):

- Host loopback only (compose default) — single operator on the same machine
- Tailscale / Cloudflare Access in front of the portal route — recommended
  for shared/multi-operator access
- NEVER publish 8081 publicly; never reverse-proxy the portal without a
  zero-trust service in front

SDK endpoints (8080) stay public — device traffic; requests carry the SDK's
bearer key, the listener itself has no auth.

In-binary guard: portal requests with a non-loopback, non-configured
`Host`/`Origin` → 403. Same-origin UI + curl from the operator machine work.

## Ops

- Healthcheck: compose `healthcheck` runs busybox `wget` against
  `http://localhost:8080/health` inside the container — `docker compose ps`
  shows `healthy`. It lives in compose, not the Dockerfile, so the image
  carries no check policy (image stays runner-agnostic).
- Image base tradeoff: alpine (busybox) chosen over
  `gcr.io/distroless/static-debian12:nonroot` — distroless is a smaller
  surface but has no shell/curl/wget at all, so an in-container healthcheck
  is impossible; alpine costs ~5 MB + a shell and enables it. Runtime is
  genuinely non-root (uid 1000), static binary, no build tools.
- Logs: `docker compose logs -f detur`. Startup logs show domain + portal
  address.
- Purge janitor: expired clicks/events purged at startup + on read
  (retention = max(configured window, `DETUR_RETENTION_HOURS`)). No manual
  cleanup.
- Upgrades: back up the data volume before major version jumps (schema not
  versioned in v1).

## Security

- TLS mandatory in production (Deploy → TLS). Plain http = SDK keys +
  fingerprints in cleartext.
- API keys: SHA-256 hashed at rest; full key shown once at creation, masked
  after.
- Portal: no auth — bounded by the network controls above.
- No secrets in the image: env only, runtime config (R18).
- Image: static binary, non-root user, minimal alpine runtime — no build
  tools, no SSH; runtime stage installs nothing (all deps baked at build).