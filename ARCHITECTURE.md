# Architecture

Frozen MVP stack choices for this pastebin. Product behavior lives in `.scratch/paste/SPEC.md`; this file records implementation choices only.

## Language and binaries

- **Server:** Go (`cmd/pastebin`)
- **CLI:** Go sibling binary `pbin` (same module; not yet implemented)
- **Module:** `github.com/harshalranjhani/paste`

## Persistence

- **Database:** SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- **Deployment shape:** single app process + one persistent `/data` volume
- **Default path:** `DATABASE_PATH=/data/paste.sqlite` (or `DATA_DIR/paste.sqlite`)
- **Migrations:** ordered SQL migrations applied deterministically on startup (`schema_migrations`)

## Paste content storage

- File bodies are stored in SQLite (`paste_files.content`), not object storage
- One paste model only: `Paste` → `1..N PasteFiles` with POSIX-relative paths

## Highlighting

- Server-side [Chroma](https://github.com/alecthomas/chroma) for the selected file only (wired when paste viewing lands)

## Auth / sessions

- Passwords: Argon2id (`golang.org/x/crypto/argon2`)
- First admin via `/setup` (BEGIN IMMEDIATE; permanently disabled afterward)
- Further accounts only via admin invite links (`/admin/invites`, redeem at `/invite/{token}`)
- Invite tokens: ≥128 bits entropy, shown once, SHA-256 hashed at rest, default TTL 7 days
- Browser sessions: server-side rows + HttpOnly `session` cookie; CSRF via non-HttpOnly `csrf` cookie / form / `X-CSRF-Token`
- Personal access tokens: `pb_` prefix, SHA-256 hashed at rest, show-once, optional expiry, scopes `paste:create|read|delete`, `last_used_at`; Bearer auth skips cookie CSRF
- Config: `SESSION_SECRET` reserved for future signing needs

## UI

- Server-rendered HTML + small progressive JS
- No SPA framework requirement

## CLI packaging

- Binary name: `pbin`
- Distributed as a release binary alongside the server image/build
- Talks to versioned `/api/v1` over HTTP

## Deployment

- Docker multi-stage image, process runs as non-root user `paste`
- `docker compose up` mounts named volume `paste-data` at `/data`
- Logs to stdout/stderr (structured JSON from the server process)
- Health: unauthenticated `GET /healthz` (process + DB ping)
- Core env: `BASE_URL`, `LISTEN_ADDR`, `DATA_DIR` / `DATABASE_PATH`, `SESSION_SECRET`

## Explicit non-choices for MVP

- No PostgreSQL, Redis, S3/MinIO, or background-job broker
- No anonymous paste creation
- No burn-after-read
- No web directory upload (CLI + API bundle are canonical for trees)
