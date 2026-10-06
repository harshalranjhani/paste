# 03: Single-file unlisted paste

**What to build:** A signed-in user creates a single UTF-8 text/code paste via `/new` and `POST /api/v1/pastes`, getting one high-entropy unlisted URL. Recipients open `/p/{id}` without an account and see highlighted content with line numbers (no empty tree sidebar). Raw download, copy URL/contents, owner delete, and expiry (default and max 90 days; over-max rejected) work. Unknown IDs return 404; expired pastes return 410 at request time. Paste responses are noindex. Unauthenticated create is rejected. Binary/non-UTF-8 uploads are rejected.

**Blocked by:** 02 — Invite-only accounts

**Status:** done

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [x] Authenticated create works for exactly one text file via web and API
- [x] Unauthenticated create is rejected server-side
- [x] `public_id` has ≥ ~96 bits entropy (16 random bytes, base62)
- [x] Viewer shows Chroma highlighting + line numbers without a useless tree pane
- [x] Raw endpoint serves `text/plain` with nosniff and noindex-related protections
- [x] Default expiry is 90 days; max is 90 days; above max is hard-rejected
- [x] Expired paste cannot be read (410) even before cleanup runs; unknown ID is 404
- [x] Owner can delete their paste
- [x] Paste pages emit `X-Robots-Tag` / robots meta; no public listing routes exist
- [x] Non-UTF-8 / binary bodies are rejected
- [x] Pastes are immutable after create (no edit API/UI)
- [x] Covered by HTTP-seam tests
