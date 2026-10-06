# paste

Minimal self-hosted multi-file pastebin (Gist-style trees, invite-only, CLI).

This repository currently holds the product contract and agent-ready work breakdown. Implementation has not started.

## Docs

| Doc | Purpose |
|-----|---------|
| [PLAN.md](PLAN.md) | Original long-form product contract |
| [.scratch/paste/SPEC.md](.scratch/paste/SPEC.md) | MVP spec (wins over PLAN where they differ) |
| [.scratch/paste/issues/](.scratch/paste/issues/) | Tracer-bullet tickets (`ready-for-agent`) |

## MVP freeze (short)

- Go + SQLite, single process, Docker Compose
- Auth required to create; nested folder = one unlisted URL
- Folder upload via API + `pbin` CLI (no web directory upload in MVP)
- Password + expiry; no burn-after-read; no anonymous create in MVP

## Ticket order

Start at **01 — Bootable skeleton**. See issue files for blocking edges.
