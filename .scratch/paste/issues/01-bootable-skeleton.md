# 01: Bootable skeleton

**What to build:** An operator can `docker compose up` a fresh instance that serves an unauthenticated `/healthz` proving the process and SQLite database are reachable. Config, migrations, and a persistent `/data` volume work. An HTTP test harness boots the app against a temporary SQLite database. `ARCHITECTURE.md` records the frozen MVP stack choices (Go, SQLite, bodies in DB, Chroma, `pbin`, etc.).

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [ ] Docker Compose brings up the app with a documented data volume
- [ ] `GET /healthz` returns success when process and DB are healthy, without auth
- [ ] Migrations apply deterministically on startup
- [ ] App respects core env config (`BASE_URL`, listen addr, database path, secrets placeholders)
- [ ] HTTP tests can start the app against a temp SQLite DB
- [ ] `ARCHITECTURE.md` documents chosen language, DB, content storage, highlighting, and CLI packaging
- [ ] Process runs as non-root in the image where practical; logs go to stdout/stderr
