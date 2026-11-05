# AGENTS.md

## Replies

- Verdict first. 1–3 lines.

## Engineering

- Fewest deps.
- Integration tests only. Fewest, max coverage.
- Done = DECISIONS, CAVEATS, README updated.

### Go

- Stdlib only. No frameworks, ORMs, cgo.
- `internal/<domain>`. No `pkg/`, `utils`.
- Tests: real SQLite + `httptest`. No mocks.

### Portal UI

Seline-style. Tokens in `portal/src/theme.css`; no raw hex.

- Stone neutrals + one blue. Red = destructive only.
- Warm canvas, white cards, 1px borders. Shadows rare.
- Headings weight 400. Body 14px.
- One filled button per view.
- Tables beside Cards, never inside.

## Deps & tooling

- Go 1.27, Bun 1.4.4 + `bun.lock`.
- Exact pins. Patch via `bun patch`.

## Docs

- Telegraphic. Answer first.
- HTML: light mode.
- README: what, why, how.
- Edits match neighbor length.
