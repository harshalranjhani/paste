# 04: Password-protected paste

**What to build:** A creator can require a password on a paste. Recipients see a lock screen with no filenames or contents until they unlock. Successful unlock grants a short-lived paste-access session (browser session cookie, hard-capped at 1 hour server-side) that is not an account session. Wrong passwords fail generically and are rate-limited. Meta, file, and raw endpoints cannot bypass the gate.

**Blocked by:** 03 — Single-file unlisted paste

**Status:** done

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [x] Password is hashed with Argon2id; plaintext is never stored
- [x] Locked paste reveals no file names or bodies
- [x] Correct password unlocks; incorrect password fails with a generic message
- [x] Unlock attempts are rate-limited
- [x] Paste-access session is session-scoped in the browser and expires within 1 hour server-side
- [x] Paste-access session does not grant account privileges
- [x] Raw (and meta) endpoints enforce the same authorization as the viewer
- [x] Covered by HTTP-seam tests
