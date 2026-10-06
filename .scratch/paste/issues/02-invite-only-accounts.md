# 02: Invite-only accounts

**What to build:** On a fresh database, `/setup` creates the first admin exactly once (safe under concurrent requests). After that, open signup is impossible. Users log in and out with secure server-side sessions and CSRF on cookie mutations. An admin creates single-use, expiring invite links (default 7 days, hashed at rest, optional email constraint, no SMTP required); an invitee redeems one to become a normal user. `/` tells unsigned visitors to sign in to create pastes.

**Blocked by:** 01 — Bootable skeleton

**Status:** ready-for-agent

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [ ] First successful `/setup` creates an admin; setup then permanently refuses new accounts
- [ ] Concurrent setup requests cannot create two bootstrap admins
- [ ] Login uses Argon2id; sessions are HttpOnly, rotatable on login, invalidatable on logout
- [ ] State-changing cookie-authenticated requests require CSRF protection
- [ ] Admin can create/revoke invites; tokens are shown once and stored hashed
- [ ] Invite default TTL is 7 days; expired and reused invites fail clearly
- [ ] Invite redemption creates a normal user; optional email constraint is enforced when set
- [ ] No public `/register` or open signup path exists
- [ ] Unauthenticated `/` prompts sign-in rather than showing a create editor
- [ ] Covered by HTTP-seam tests (bootstrap race, invite lifecycle, session/CSRF)
