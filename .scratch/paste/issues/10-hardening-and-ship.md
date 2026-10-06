# 10: Hardening and ship checklist

**What to build:** The MVP is operator-ready: in-process cleanup deletes expired pastes and stale sessions/invites; security headers and CSP are in place; `robots.txt` is restrictive; README documents deploy, backup/restore, CLI setup, and reverse-proxy/`BASE_URL` behavior. Spec acceptance scenarios for the in-scope MVP are green at the HTTP and CLI seams (no anonymous create, no burn, no web directory upload).

**Blocked by:** 04 — Password-protected paste; 05 — Multi-file tree paste; 06 — My Pastes and moderation; 09 — pbin directory upload

**Status:** ready-for-agent

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [ ] Periodic in-process cleanup removes expired paste data and other expired rows per spec
- [ ] Strict CSP and related security headers are present on app responses
- [ ] Restrictive `robots.txt`; paste IDs never appear in a sitemap
- [ ] README covers Compose deploy, backup/restore of `/data`, CLI auth, and reverse proxy notes
- [ ] Graceful SIGTERM shutdown works
- [ ] MVP acceptance scenarios from the spec pass (invite-only, single-file, password, folder via CLI, no public discovery)
- [ ] Out-of-scope items (anonymous create, burn, web folder upload) remain unimplemented and undocumented as required
