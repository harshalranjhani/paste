# 10: Hardening and ship checklist

**What to build:** The MVP is operator-ready: in-process cleanup deletes expired pastes and stale sessions/invites; security headers and CSP are in place; `robots.txt` is restrictive; README documents deploy, backup/restore, CLI setup, and reverse-proxy/`BASE_URL` behavior. Spec acceptance scenarios for the in-scope MVP are green at the HTTP and CLI seams (no anonymous create, no burn, no web directory upload).

**Blocked by:** 04 — Password-protected paste; 05 — Multi-file tree paste; 06 — My Pastes and moderation; 09 — pbin directory upload

**Status:** done

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [x] Periodic in-process cleanup removes expired paste data and other expired rows per spec
- [x] Strict CSP and related security headers are present on app responses
- [x] Restrictive `robots.txt`; paste IDs never appear in a sitemap
- [x] README covers Compose deploy, backup/restore of `/data`, CLI auth, and reverse proxy notes
- [x] Graceful SIGTERM shutdown works
- [x] MVP acceptance scenarios from the spec pass (invite-only, single-file, password, folder via CLI, no public discovery)
- [x] Out-of-scope items (anonymous create, burn, web folder upload) remain unimplemented and undocumented as required
