# 07: Personal access tokens

**What to build:** A signed-in user creates and revokes Personal Access Tokens for API use. Tokens use a `pb_` prefix, are shown plaintext once, stored hashed, support optional expiry and scopes `paste:create`, `paste:read`, `paste:delete`, and record `last_used_at`. Bearer tokens can exercise paste APIs within scope. Admins cannot retrieve token plaintext later.

**Blocked by:** 02 — Invite-only accounts; 03 — Single-file unlisted paste

**Status:** ready-for-agent

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [ ] User can create a token and sees plaintext exactly once
- [ ] Database stores only a hash; later UI never shows plaintext
- [ ] User can revoke their own tokens; revoked and expired tokens fail
- [ ] Missing/wrong scope fails appropriately
- [ ] Successful API use updates `last_used_at`
- [ ] Bearer auth can create/read/delete pastes according to scopes without cookie CSRF
- [ ] Admin cannot read another user’s token plaintext
- [ ] Covered by HTTP-seam tests
