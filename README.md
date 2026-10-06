# paste

Minimal self-hosted multi-file pastebin (Gist-style trees, invite-only, CLI).

## Quick start

```bash
cp .env.example .env
docker compose up --build
curl -fsS http://localhost:8080/healthz
```

SQLite and app data persist in the Docker named volume `paste-data` mounted at `/data`.

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `BASE_URL` | `http://localhost:8080` | Public origin for generated links |
| `LISTEN_ADDR` | `0.0.0.0:8080` | Bind address inside the process/container |
| `DATA_DIR` | `/data` | Persistent data directory |
| `DATABASE_PATH` | `$DATA_DIR/paste.sqlite` | SQLite file path |
| `SESSION_SECRET` | _(required later for auth)_ | Session signing secret placeholder |

## Development

```bash
go test ./...
go run ./cmd/pastebin
```

HTTP tests boot the app against a temporary SQLite database via `internal/apptest`.

## Docs

| Doc | Purpose |
|-----|---------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Frozen MVP stack choices |
| [PLAN.md](PLAN.md) | Original long-form product contract |
| [.scratch/paste/SPEC.md](.scratch/paste/SPEC.md) | MVP spec (wins over PLAN where they differ) |
| [.scratch/paste/issues/](.scratch/paste/issues/) | Tracer-bullet tickets |

## MVP freeze (short)

- Go + SQLite, single process, Docker Compose
- Auth required to create; nested folder = one unlisted URL
- Folder upload via API + `pbin` CLI (no web directory upload in MVP)
- Password + expiry; no burn-after-read; no anonymous create in MVP
