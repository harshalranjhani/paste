# 04: Password-protected paste

**What to build:** A creator can require a password on a paste. Recipients see a lock screen with no filenames or contents until they unlock. Successful unlock grants a short-lived paste-access session (browser session cookie, hard-capped at 1 hour server-side) that is not an account session. Wrong passwords fail generically and are rate-limited. Meta, file, and raw endpoints cannot bypass the gate.

**Blocked by:** 03 — Single-file unlisted paste

**Status:** ready-for-agent

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [ ] Password is hashed with Argon2id; plaintext is never stored
- [ ] Locked paste reveals no file names or bodies
- [ ] Correct password unlocks; incorrect password fails with a generic message
- [ ] Unlock attempts are rate-limited
- [ ] Paste-access session is session-scoped in the browser and expires within 1 hour server-side
- [ ] Paste-access session does not grant account privileges
- [ ] Raw (and meta) endpoints enforce the same authorization as the viewer
- [ ] Covered by HTTP-seam tests
