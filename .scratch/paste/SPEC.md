# Spec: Minimal Self-Hosted Multi-File Pastebin (MVP)

**Status:** ready-for-agent  
**Tracker:** local (`.scratch/paste/`)  
**Source:** `PLAN.md` + grill decisions (2026-10-06)

---

## Problem Statement

Developers need a self-hosted way to share a single code file or an entire nested folder tree as **one** unlisted link—without standing up Git, a cloud drive, a social paste site, or a zero-knowledge secret vault. Existing tools either flatten files, require Git, invent public feeds, or optimize for secrets rather than readable code trees. Operators want something tiny: one Docker Compose stack, invite-only accounts, strong privacy-by-non-discovery, and a CLI that uploads a directory in one shot.

## Solution

Ship a minimal Go + SQLite pastebin where every paste is the same model (`Paste` → `1..N PasteFiles` with POSIX-relative paths). Authenticated users create single-file or multi-file pastes via the web, HTTP API, or `pbin` CLI. Unauthenticated visitors can open unlisted paste URLs they already have. All pastes expire (default and max 90 days), may be password-protected, are never listed or indexed, and are immutable after creation. MVP deliberately excludes anonymous creation, burn-after-read, and browser directory upload.

## User Stories

1. As an operator, I want to bring the app up with Docker Compose and a single data volume, so that deployment stays operationally trivial.
2. As an operator, I want a `/healthz` endpoint that checks process and database health without auth, so that my reverse proxy and orchestrator can probe the service.
3. As the first visitor on a fresh instance, I want `/setup` to create the first admin account, so that the instance is owned without a seeded password.
4. As an operator, I want bootstrap setup to be permanently disabled after the first admin exists, so that strangers cannot mint a second admin.
5. As the system, I want first-admin creation to be transaction-safe under concurrent requests, so that two setup posts cannot both succeed.
6. As an admin, I want to create single-use invite links that expire (default 7 days), so that I can onboard users without open registration.
7. As an admin, I want invite tokens stored hashed and shown as a URL once, so that a database leak does not yield usable invites.
8. As an admin, I want optional email constraints on invites without requiring SMTP, so that I can restrict who redeems a link while still sharing it manually.
9. As an admin, I want to revoke unused invites and see used/expired state, so that I can manage onboarding.
10. As an invitee, I want to redeem a valid invite once to create a normal user account, so that I can start creating pastes.
11. As an invitee, I want expired or already-used invites to fail clearly, so that I know to ask for a new link.
12. As any visitor, I want `/register` and open signup to be unavailable, so that the instance stays invite-only.
13. As a user, I want to log in with username and password (Argon2id), so that I can access create and management surfaces.
14. As a user, I want secure HttpOnly session cookies with rotation on login, so that session theft and fixation risks are reduced.
15. As a user, I want logout to invalidate the server-side session, so that shared machines do not keep me signed in.
16. As a browser user, I want CSRF protection on state-changing form/API cookie calls, so that other sites cannot act as me.
17. As an unauthenticated visitor, I want `/` to explain that signing in is required to create pastes, so that I am not shown a fake editor.
18. As an unauthenticated visitor, I want to open `/p/{id}` links I was given, so that recipients do not need accounts.
19. As a signed-in user, I want `/new` to create a single-file paste from an editor, so that quick snippets are easy.
20. As a signed-in user, I want to add multiple files (picker / add-file) with relative paths on `/new`, so that small trees can be built in the browser without directory upload APIs.
21. As a signed-in user, I want folder/directory drag-drop upload deferred, so that MVP does not depend on fragile browser directory APIs.
22. As a signed-in user, I want to choose expiry presets up to 90 days (default 90 days), so that pastes do not live forever.
23. As a signed-in user, I want requests for expiry above the configured max to be rejected, so that clients learn the real contract.
24. As a signed-in user, I want optional password protection on a paste, so that possessing the URL alone is not enough.
25. As a signed-in user, I want pastes to be unlisted (“anyone with the link”), so that sharing stays intentional.
26. As a signed-in user, I want a clear explanation that the paste will not appear in feeds or search, so that “public link” is not misunderstood.
27. As a signed-in user, I want syntax highlighting based on filename/extension for the selected file, so that code is readable.
28. As a signed-in user, I want line numbers in the viewer, so that I can refer to specific lines in chat.
29. As a signed-in user, I want to copy the paste URL and copy the current file contents, so that handoff is fast.
30. As a signed-in user, I want raw/plain-text access to individual files when authorized, so that curl and tooling work.
31. As a signed-in user, I want to download one file or a ZIP of the whole paste preserving paths, so that recipients can take the tree offline.
32. As a recipient of a single-file paste, I want a viewer without an empty tree sidebar, so that the UI stays minimal.
33. As a recipient of a multi-file paste, I want a left file tree and right code pane, so that I can browse the folder like a tiny editor.
34. As a recipient, I want the tree to load metadata first and fetch file bodies on selection, so that large pastes stay fast.
35. As a recipient, I want keyboard navigation in the tree, so that the viewer is usable without a mouse.
36. As a recipient on mobile, I want the tree as a drawer/compact picker, so that the layout still works on small screens.
37. As a recipient of a password-protected paste, I want filenames and contents hidden until I unlock, so that the URL alone does not leak structure.
38. As a recipient, I want a wrong password to fail without revealing whether specific files exist, so that probing is harder.
39. As a recipient, I want unlock to grant a short-lived paste-access session (session cookie, max 1 hour server-side), so that I can browse/ZIP without re-entering the password every click.
40. As a recipient, I want raw and ZIP endpoints to enforce the same password gate, so that APIs cannot bypass the UI.
41. As any client, I want expired pastes to be inaccessible immediately at request time (410), so that a slow cleanup worker cannot leak content.
42. As any client, I want unknown paste IDs to return 404, so that enumeration gets a consistent negative.
43. As an operator, I want expired paste rows and file bodies deleted by a periodic in-process cleanup, so that disk/DB actually shrink.
44. As a signed-in user, I want to delete my own pastes, so that I can revoke a share early.
45. As an admin, I want to delete any paste by exact ID after seeing safe metadata only, so that I can moderate without a content browser.
46. As a signed-in user, I want a My Pastes page listing my pastes (title, created, expires, files, protection, delete), so that I can manage what I have shared.
47. As a signed-in user, I want Personal Access Tokens (`pb_` prefix, shown once, hashed at rest, revocable, optional expiry, scopes `paste:create|read|delete`), so that CLI/API use does not need my password.
48. As a signed-in user, I want `last_used_at` on tokens, so that I can spot stale credentials.
49. As an admin, I want to be unable to retrieve anyone’s token plaintext, so that admin power does not equal credential export.
50. As a CLI user, I want `pbin auth login|status|logout` against a server URL and token, so that configuration is explicit.
51. As a CLI user, I want credentials in OS keychain when practical, else `0600` config, so that tokens are not world-readable.
52. As a CLI user, I want `pbin create` from stdin, a file, or a directory, so that all common share flows work.
53. As a CLI user, I want directory uploads to preserve nested relative paths in one paste URL, so that folder shares stay coherent.
54. As a CLI user, I want the CLI to ignore `.git/`, honor `.gitignore` when present, and honor `.pasteignore`, so that junk and secrets are less likely to upload by accident.
55. As a CLI user, I want an interactive pre-upload summary (file count, bytes, expiry, visibility), so that I can abort before sharing too much.
56. As a CLI user, I want a warning for obvious sensitive filenames (`.env`, keys, pem) without hard-blocking, so that intentional secret sharing still works.
57. As a CLI user, I want successful create to print only the URL on stdout (details on stderr), so that shell scripting is easy.
58. As a CLI user, I want `--json` output and `--expires` / `--password` (prompt or stdin) flags, so that automation and humans both work.
59. As a CLI user, I want symlinks rejected/ignored by default, so that uploads cannot escape the chosen directory.
60. As a CLI user, I want `pbin delete <id>` for pastes I own, so that cleanup does not require the browser.
61. As an API client, I want versioned `/api/v1` endpoints for create, bundle, meta, file, raw, ZIP, and delete, so that integrations stay stable.
62. As an API client, I want atomic multi-file create (all-or-nothing), so that partial trees never exist.
63. As an API client, I want path traversal and malformed paths rejected server-side, so that ZIP and storage stay safe.
64. As an API client, I want non-UTF-8 and binary uploads rejected in MVP, so that the product stays a text/code pastebin.
65. As an API client, I want consistent JSON errors with small `{error:{code,message}}` shapes, so that clients can branch on codes.
66. As an unauthenticated client, I want create endpoints to require auth in MVP, so that anonymous spam is not a v1 concern.
67. As a search engine or crawler, I want paste responses to carry noindex robots meta and `X-Robots-Tag`, so that unlisted pastes are not indexed.
68. As an operator, I want a restrictive `robots.txt` and no paste IDs in sitemaps, so that discovery stays closed.
69. As an operator, I want high-entropy base62 `public_id` values (≥ ~96 bits / 16 random bytes), so that unlisted URLs are not practically enumerable.
70. As an operator, I want configurable limits via environment (TTL, sizes, file counts, trusted proxies, secrets), so that I can tune without code changes.
71. As an operator, I want runtime admin settings UI deferred, so that MVP admin stays invites + moderation-by-id + My Pastes only.
72. As an operator, I want structured logs without paste bodies, passwords, or token plaintext, so that logs are safe to retain.
73. As an operator, I want the app to run as non-root, log to stdout/stderr, honor `BASE_URL`, and work behind Traefik/nginx/Caddy, so that homelab reverse-proxy setups work.
74. As a user, I want light/dark UI from system preference with a minimal terminal/editor feel, so that the tool stays calm and fast.
75. As a user with accessibility needs, I want labeled controls, focus states, and ARIA on the file tree, so that the viewer is keyboard-accessible.
76. As a developer of this project, I want automated tests at the HTTP seam (and CLI against that server) covering auth, tree upload, password, expiry, ZIP, privacy headers, and path attacks, so that regressions are caught at the product boundary.
77. As a future operator, I want a documented post-MVP path for single-file burn, optional anonymous create, web folder upload, and multi-file burn leases, so that scope creep does not redefine v1 done.

## Implementation Decisions

### Product / threat model
- This is a **code pastebin**: server stores plaintext content. Not zero-knowledge. Do not claim PrivateBin-style deniability.
- Privacy means: unlisted URLs, no discovery, noindex, expiry, optional password access control, invite-only creation.
- One data model only: `Paste` with `1..N PasteFiles`. No separate “folder paste” type.
- Pastes are **immutable** after create (create/delete only; no edit, no revision history).
- All pastes unlisted; no public/recent/trending/search routes.

### MVP scope cuts (explicit)
- **No anonymous paste creation** in v1 (view-by-link still allowed).
- **No burn-after-read** in v1 (first post-MVP item: single-file burn).
- **No web directory upload** in v1 (CLI + API bundle are canonical for trees; web may multi-select files).
- **No binary / non-UTF-8** uploads in v1 (reject).
- Thin admin only: invites, My Pastes, locate-by-id delete. Limits via env, not admin settings UI.
- TOTP, markdown preview, line permalinks deferred.

### Stack
- **Language:** Go for server and `pbin` (sibling binaries, same module).
- **DB:** SQLite in a persistent `/data` volume; single app process; in-process cleanup ticker.
- **File bodies:** stored in SQLite (`paste_files.content`), not object storage.
- **Highlighting:** server-side Chroma for the selected file only.
- **UI:** server-rendered HTML + small progressive JS; no SPA framework requirement.

### Identity & auth
- Roles: `user`, `admin` only (anonymous is not a create role in MVP).
- First user via `/setup` → admin; then invites only.
- Passwords: Argon2id.
- Sessions: server-side, HttpOnly, Secure when HTTPS, SameSite=Lax or stricter, rotatable, invalidatable.
- CSRF on cookie-authenticated mutations; Bearer token API calls do not use cookie CSRF.
- Paste passwords: Argon2id; rate-limited attempts; generic errors; unlock issues paste-access session (session cookie + **1h max** server-side), not an account session.

### IDs, HTTP semantics, expiry
- `public_id`: 16 bytes CSPRNG → base62.
- Unknown paste → **404**; expired (and later burned) → **410**.
- Default and max TTL: 90 days (admin-configurable via env; shipped default 90d).
- Expiry above max → **hard reject** (no silent clamp).
- `expires_at` stored as absolute UTC; request-time enforcement + background delete.

### Paths & uploads
- Normalize to POSIX `/`; reject absolute paths, `..`, drive letters, UNC, NUL, duplicates, over-depth/length.
- Authenticated limits (env-tunable): up to 500 files, 2 MiB/file, 25 MiB total, depth 20, path length 512 bytes.
- Bundle API: multipart manifest + files; validate server-side; atomic transaction.
- CLI ignores `.git/`, honors `.gitignore` and `.pasteignore`; rejects/skips symlinks by default.

### Routes (behavioral; names may vary slightly)
- Web: `/`, `/login`, `/setup`, `/invite/{token}`, `/new`, `/p/{id}`, `/me/pastes`, `/settings/tokens`, `/admin/invites` (+ minimal user/paste moderation as needed).
- API: `/api/v1/pastes`, `/pastes/bundle`, `/pastes/{id}/meta`, `/files/{file_id}`, `/raw`, `/archive.zip`, `DELETE /pastes/{id}`, `/me/pastes`, `/tokens`, `/admin/invites`.
- No burn/reveal routes in MVP.

### CLI
- Binary name: `pbin`.
- Auth login with hidden token prompt; URL-only stdout on success; `--json` supported.

### Security headers / content serving
- Raw: `text/plain; charset=utf-8`, `nosniff`, noindex.
- Downloads: `Content-Disposition: attachment`.
- Strict CSP; never execute uploaded HTML/JS in-origin.
- No server-side fetch of user URLs (no SSRF feature surface).

### Deployment
- Docker Compose: app + `/data` volume.
- Required config concepts: `BASE_URL`, listen addr, `DATA_DIR`/`DATABASE_PATH`, session/rate secrets (rate secret may wait until anonymous exists), TTL and size limits, `TRUSTED_PROXIES`.
- Non-root, SIGTERM-graceful, migrations deterministic, no telemetry.

### Implementation order
1. Skeleton: Go + SQLite + Docker + health + setup/login/invites  
2. Single-file paste: create/view/raw/delete + password + expiry + Chroma + noindex  
3. Multi-file: path model + tree viewer + ZIP + path tests  
4. PATs + `pbin` (stdin/file/dir + bundle + ignores)  
5. Thin admin/My Pastes + hardening (CSRF, headers, cleanup, e2e)

### Post-MVP queue (not this spec’s done criteria)
1. Single-file burn-after-read (scanner-safe reveal)  
2. Optional anonymous create (HMAC rate limit, delete tokens)  
3. Web folder upload  
4. Multi-file burn viewer lease  
5. TOTP / markdown preview / line permalinks  

## Testing Decisions

### What makes a good test
- Assert **external behavior** only: HTTP status/headers/body, CLI stdout/stderr/exit codes, DB-visible effects only insofar as the client can observe them (e.g. second create fails, unlock works).
- Do not test private function structure, ORM shapes, or Chroma internals.
- Prefer the highest seam; avoid duplicate coverage at lower layers unless the HTTP seam cannot express the case cleanly.

### Seams (agreed)
1. **Primary:** black-box HTTP against the app with a temporary SQLite database (web + `/api/v1`).
2. **CLI:** invoke `pbin` against that same test server.
3. **Optional pure path helper tests** only if they keep traversal matrices clearer than HTTP-only fixtures.

### Modules / areas to cover at those seams
- Bootstrap race and invite-only lockout after first admin  
- Invite create/redeem/expire/reuse  
- Session login/logout + CSRF on cookie mutations  
- Single-file and multi-file create (auth required)  
- Anonymous/unauthed create rejected  
- Path traversal and malformed path rejection  
- UTF-8/binary rejection  
- Password gate on UI, meta, file, raw, ZIP  
- Expiry presets, over-max reject, request-time 410 before cleanup  
- Cleanup deletes expired content  
- ZIP hierarchy safety (no absolute/traversal names)  
- Privacy: no listing routes; noindex headers; robots behavior  
- Token show-once, hash-at-rest, revoke, scope checks  
- CLI: stdin/file/dir, ignores, symlink skip, URL-only stdout  

### Prior art
- Greenfield: no existing test suite. Establish HTTP helper fixtures (test server + temp DB + auth client) as the shared harness from the first slice.

## Out of Scope

- Anonymous creation, burn-after-read (all forms), web directory upload  
- Client-side/zero-knowledge encryption  
- Git hosting, revisions, collaborative editing, comments, likes, forks  
- Public feeds, search, profiles, trending, sitemaps of pastes  
- OAuth/OIDC, SMTP-required invites, multi-replica Postgres (unless later forced)  
- S3/MinIO, Redis, Celery/Sidekiq, analytics, telemetry, AI features  
- Binary attachments, image/video galleries  
- In-place edit of pastes  
- Full admin instance-settings UI  
- TOTP, markdown preview, QR codes, PWA, URL shortener  

## Further Notes

- Original long-form product contract remains in `PLAN.md`; where it conflicts with this spec, **this spec wins** for MVP (notably anonymous-off, burn deferred, SQLite+Go chosen, web folder deferred).
- Write `ARCHITECTURE.md` at the start of implementation recording stack choices already frozen here.
- Highest-risk areas to lock with tests early: path validation, first-admin race, invite consumption race, password authorization consistency across endpoints, ZIP path safety, trusted proxy handling (even if anonymous create is later).
- Definition of done for this spec = MVP checklist implied by user stories and implementation order phases 1–5; post-MVP queue explicitly excluded.
