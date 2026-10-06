# 06: My Pastes and moderation

**What to build:** A signed-in user sees only their own pastes on My Pastes (title, created, expires, files, protection, delete) and can delete them. An admin can look up a paste by exact public ID, see safe metadata only, and delete it. There is still no browse/feed/search of all pastes.

**Blocked by:** 03 — Single-file unlisted paste

**Status:** ready-for-agent

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [ ] `/me/pastes` lists only the current user’s pastes with the agreed columns
- [ ] Owner can delete from My Pastes
- [ ] Admin can locate by exact ID, view safe metadata, and delete
- [ ] Admin moderation does not expose a sitewide content browser or discovery UI
- [ ] Users cannot list or delete others’ pastes
- [ ] Covered by HTTP-seam tests
