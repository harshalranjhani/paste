# PLAN.md — Minimal Self-Hosted Multi-File Pastebin

> Implementation plan for an AI coding agent.
>
> Goal: build a minimal, fast, self-hosted Pastebin/Gist-style application that handles **single-file pastes and full folder trees as the same first-class object**, with a clean web UI, an authenticated CLI/API for folder uploads, expiry, password protection, burn-after-read, invite-only accounts, and Docker deployment.

---

## 1. Product Definition

Build a self-hosted pastebin for developers.

The product must feel like a **Pastebin**, not a file manager, cloud drive, Git forge, or collaboration suite.

A paste may contain:

```text
Single-file paste
└── script.py
```

or:

```text
Multi-file / folder paste
├── SKILL.md
├── evals/
│   └── evals.json
└── references/
    ├── reading-replies.md
    ├── supplier-email.md
    └── tb1-test-a-date.md
```

Both are the **same underlying Paste model**.

Every paste has exactly one shareable URL.

The application must prioritize:

1. Minimal UI.
2. Very low operational complexity.
3. Fast page loads.
4. Strong privacy defaults.
5. Excellent developer UX.
6. Correct folder/tree handling.
7. Safe expiry and burn-after-read semantics.
8. No public paste discovery.

---

## 2. Non-Negotiable Requirements

The following requirements are mandatory.

### Paste capabilities

- Create a paste containing one text/code file.
- Create a paste containing multiple text/code files.
- Upload an entire directory recursively while preserving relative paths.
- Nested directories must be supported.
- A folder paste must be represented by **one paste URL**, not one paste per file.
- Download an individual file.
- Download the complete paste/folder as a ZIP preserving directory structure.
- Copy current file contents.
- Copy paste URL.
- Syntax highlighting based primarily on filename/extension.
- Display line numbers.
- Support raw/plain-text access to individual files where access rules allow it.

### Folder upload

Folder upload must work through:

- the authenticated CLI; and
- the authenticated HTTP API.

The authenticated web UI should also support folder upload where the browser supports it.

Anonymous users must never be able to create folder/multi-file pastes.

### Expiry

- Every paste must expire.
- Default expiry: **90 days / 3 months**.
- Maximum normal expiry: **90 days / 3 months**.
- Users may choose shorter expiry periods.
- There is no `never` option in the normal UI.
- Recommended presets:
  - 10 minutes
  - 1 hour
  - 1 day
  - 7 days
  - 30 days
  - 90 days
- `expires_at` must always be stored as an absolute UTC timestamp.
- Expired content must be actually deleted, not merely hidden.

The maximum lifetime should be configurable by the administrator, but the shipped/default configuration must remain 90 days.

### Access modes

All pastes are **unlisted by design**.

The UI may use the friendly phrase **Public link**, but this means:

> Anyone possessing the URL can view the paste without signing in.

It must **not** mean the paste appears in a public feed, search page, sitemap, user profile, recent-pastes page, trending page, or browse page.

Supported protection modes:

1. **Public link / unlisted**
   - Default.
   - Anyone with the high-entropy URL can read it.
   - Never discoverable through the application.

2. **Password protected**
   - Unlisted.
   - Viewer must provide the password before file names or contents are revealed.
   - Password may be combined with burn-after-read.

3. **Burn after read once**
   - Unlisted.
   - First deliberate reveal consumes the paste for everyone else.
   - Must work with single-file and multi-file pastes.
   - Must not be accidentally consumed by link previews, crawlers, antivirus URL scanners, or unfurl bots.

No public/indexed visibility mode should exist in v1.

---

## 3. Product Principles

### 3.1 Minimal over feature-rich

Do not build:

- social feeds;
- likes;
- comments;
- forks;
- organizations;
- teams;
- public profiles;
- trending;
- paste discovery;
- URL shortening;
- QR-code generators;
- chat;
- collaboration;
- live editing;
- Git hosting;
- revision history;
- rich WYSIWYG editors;
- remote URL fetching;
- analytics dashboards;
- AI features;
- ads;
- telemetry.

If a feature does not materially improve **creating, securely sharing, reading, or deleting a paste**, it probably does not belong in v1.

### 3.2 Server-rendered or lightweight frontend

Prefer:

- server-rendered HTML;
- progressive enhancement;
- small amounts of vanilla JavaScript or an equivalently lightweight approach.

Avoid a large client-side SPA unless the implementation agent can clearly justify why it is simpler overall.

The application should remain usable on slow connections.

### 3.3 One data model

Do not create separate concepts for:

- text paste;
- file paste;
- multi-file paste;
- folder paste.

Use:

```text
Paste
└── 1..N PasteFiles
```

A single text paste is simply a Paste with one PasteFile.

This is an important architectural constraint.

---

## 4. User Roles

There are only three meaningful roles.

| Role | Capabilities |
|---|---|
| Anonymous | View accessible pastes; create up to 5 single-file pastes per rolling 24 hours |
| User | Everything anonymous can do, plus folder/multi-file upload, CLI/API tokens, own-paste management |
| Admin | Everything a user can do, plus invitations, user management, instance configuration and moderation |

Do not introduce more roles in v1.

---

## 5. Anonymous User Policy

Anonymous creation is deliberately constrained.

### Mandatory rules

An anonymous client may:

- create a maximum of **5 pastes per rolling 24-hour period**;
- upload exactly **one file per paste**;
- create text/code pastes only;
- choose an allowed expiry up to 90 days;
- enable password protection;
- enable burn-after-read.

An anonymous client may not:

- upload a folder;
- upload multiple files;
- use the folder/bundle API;
- obtain API tokens;
- edit a paste after creation;
- list other pastes;
- access any discovery endpoint.

These rules must be enforced **server-side**. Hiding buttons in the UI is not sufficient.

### Anonymous rate-limit identity

Do not permanently store raw IP addresses merely to enforce the five-pastes-per-day rule.

Recommended approach:

```text
rate_limit_key = HMAC-SHA256(server_secret, canonical_client_ip)
```

Store only the resulting keyed digest and event timestamps/counters.

Use a **rolling 24-hour window**.

The application must support a configurable trusted-proxy list/depth so the real client IP is derived only from proxy headers supplied by trusted reverse proxies.

Do not blindly trust `X-Forwarded-For`.

### Anonymous delete token

When an anonymous paste is created, generate a high-entropy delete token.

- Show/return it once.
- Store only a hash of the token.
- The creator can delete the paste before expiry using it.
- Losing the token is acceptable; the paste will still expire automatically.

This is useful without requiring anonymous users to create accounts.

---

## 6. Authentication and Account Creation

### 6.1 First-run bootstrap

When the database contains zero users:

1. `/setup` is enabled.
2. The first successfully created account automatically becomes **Admin**.
3. Once the first admin exists, bootstrap setup is permanently disabled.
4. `/setup` must no longer permit another account to be created.

The operation creating the first admin must be transaction-safe so two simultaneous requests cannot create two bootstrap admins.

### 6.2 No public registration

After initial setup:

- there is no open signup flow;
- there is no `/register` flow that anyone can use;
- normal users cannot invite people.

Only an Admin may create invitation links.

### 6.3 Invitations

Admin invitation links should:

- contain at least 128 bits of randomness;
- be stored hashed at rest;
- be single-use by default;
- expire;
- default expiry: 24 hours or 7 days;
- optionally contain a preselected email address, but email delivery must not be required;
- allow account creation even though public signup is disabled.

The simplest usable invitation UX is:

```text
Admin -> Create invite -> Copy invite URL -> send manually
```

SMTP must not be required to operate the application.

### 6.4 Password storage

Use a modern password hashing algorithm.

Preferred:

- Argon2id.

Use sensible current parameters and make them configurable/migratable.

Never encrypt or reversibly store account passwords.

### 6.5 Sessions

Browser sessions must use:

- `HttpOnly`;
- `Secure` when HTTPS;
- `SameSite=Lax` or stricter;
- sufficiently random session IDs;
- expiration;
- server-side invalidation on logout;
- session rotation after login or privilege changes.

State-changing browser endpoints require CSRF protection.

### 6.6 2FA

TOTP 2FA is useful, especially for the admin, but it is **not required for initial MVP** unless the implementation is straightforward.

If implemented:

- recovery codes must be one-time use;
- secrets must be encrypted at rest;
- an admin cannot view another user's TOTP secret.

---

## 7. API Tokens

Authenticated users need Personal Access Tokens for CLI/API use.

### Token behavior

- Token values must have at least 256 bits of entropy.
- Use a recognizable prefix such as `pb_`.
- Show the plaintext token exactly once.
- Store only a cryptographic hash of the token.
- Allow token revocation.
- Allow optional token expiration.
- Record `last_used_at`.
- Users can only manage their own tokens.
- Admins should not be able to retrieve token plaintext.

### Minimal scopes

Use only scopes that are actually needed:

```text
paste:create
paste:read
paste:delete
```

Do not build a complicated OAuth-style scope system.

A CLI token used only to upload folders should generally need `paste:create`.

---

## 8. Paste Data Model

Suggested logical schema:

### `users`

```text
id
username
password_hash
role                  # user | admin
created_at
updated_at
```

### `sessions`

```text
id
user_id
token_hash
created_at
expires_at
last_seen_at
```

### `api_tokens`

```text
id
user_id
name
token_hash
scopes
created_at
expires_at nullable
last_used_at nullable
revoked_at nullable
```

### `invites`

```text
id
created_by_admin_id
token_hash
email nullable
created_at
expires_at
used_at nullable
used_by_user_id nullable
```

### `pastes`

```text
id                    # internal DB id
public_id             # random unguessable URL id
owner_user_id nullable
title nullable
protection_mode       # open | password
password_hash nullable
burn_after_read bool
burned_at nullable
created_at
expires_at
delete_token_hash nullable
total_files
total_bytes
```

Do not store a `public=true` discoverability flag. Pastes are unlisted by product definition.

### `paste_files`

```text
id
paste_id
path                  # normalized POSIX relative path
display_name
mime_type
language nullable
size_bytes
content               # representation chosen by implementation
created_at
```

Important constraints:

- `(paste_id, path)` must be unique.
- Paths are case-sensitive unless there is a strong reason otherwise.
- Empty directories do not need to be stored in v1.
- Directories are inferred from file paths.

### `anonymous_rate_events` or equivalent

```text
id
ip_hmac
created_at
```

A bucket/counter model is also acceptable if it correctly implements the rolling 24-hour policy.

### `burn_sessions` if needed

See the burn-after-read section below.

---

## 9. File and Path Rules

Folder support is security-sensitive.

Every uploaded path must be normalized server-side.

### Reject

- absolute paths;
- `..` traversal segments;
- `.` path segments after normalization where inappropriate;
- NUL bytes;
- Windows drive roots such as `C:\`;
- UNC paths;
- paths beginning with `/`;
- duplicate normalized paths;
- paths that normalize outside the virtual paste root;
- pathological depth;
- oversized names.

Internally normalize to POSIX-style `/` separators regardless of the uploader OS.

For example:

```text
references\supplier-email.md
```

uploaded from Windows should become:

```text
references/supplier-email.md
```

### Recommended default limits

These should be administrator-configurable.

Anonymous:

```text
files per paste:       1
max file size:         1 MiB
max paste total:       1 MiB
pastes / 24 hours:     5
```

Authenticated:

```text
files per paste:       500
max file size:         2 MiB
max paste total:       25 MiB
max path depth:        20
max path length:       512 bytes
```

This is a pastebin, not bulk object storage.

Reject uploads that exceed limits rather than trying to become a general-purpose file host.

### Text/code focus

v1 should primarily accept UTF-8 text/code files.

Binary files should either:

1. be rejected; or
2. be accepted only as opaque downloadable attachments without inline rendering.

**Prefer rejecting arbitrary binary files in MVP** to keep the product a pastebin and reduce attack surface.

The implementation agent may allow a very small set of safe previewable binary types later, but that is not required.

---

## 10. Folder Upload Semantics

### 10.1 CLI is the canonical folder-upload experience

The official CLI should recursively walk the supplied directory and send one logical paste containing all accepted files.

Example:

```bash
pbin create ./trellis-buyer
```

returns:

```text
https://paste.example.com/p/AbCDef123456
```

The remote paste must preserve:

```text
trellis-buyer/
├── SKILL.md
├── evals/
│   └── evals.json
└── references/
    └── supplier-email.md
```

The root directory itself does not need to appear as an extra tree node if the paste title already represents it.

### 10.2 Do not upload folders as ZIP archives internally

The CLI should preferably build a manifest and stream individual files in a multipart request.

This avoids:

- zip-slip extraction bugs;
- decompression bombs;
- server-side temporary archive extraction;
- ambiguous path handling.

Suggested bundle protocol:

```text
POST /api/v1/pastes/bundle
Authorization: Bearer pb_...
Content-Type: multipart/form-data
```

Parts:

```text
metadata
manifest
file_0
file_1
...
```

`metadata` example:

```json
{
  "title": "trellis-buyer",
  "expires_in": "30d",
  "burn_after_read": false,
  "password_protected": false
}
```

`manifest` example:

```json
[
  {"part": "file_0", "path": "SKILL.md"},
  {"part": "file_1", "path": "evals/evals.json"},
  {"part": "file_2", "path": "references/supplier-email.md"}
]
```

The server must validate every path and size regardless of what the CLI claims.

### 10.3 Atomic creation

A multi-file paste must be created atomically.

Either:

- the complete valid paste exists; or
- no paste exists.

Do not leave partial folder pastes if the request fails halfway through.

### 10.4 Symlinks

The CLI should ignore/reject symlinks by default.

Do not follow symlinks in MVP.

This prevents accidental uploads outside the requested directory and avoids symlink loops.

### 10.5 Ignore behavior

For developer convenience:

- always ignore `.git/`;
- if a `.gitignore` exists, the CLI should preferably honor it;
- support a `.pasteignore` file for paste-specific exclusions;
- provide `--no-ignore` only if straightforward.

Avoid silently embedding huge build directories.

The CLI should print a pre-upload summary:

```text
42 files
813 KiB total
expires in 30 days
visibility: unlisted
```

before upload when interactive.

---

## 11. CLI

Ship an official CLI or a thin first-party client.

Avoid naming the executable `paste`, because Unix already has a standard `paste` command.

Placeholder binary name:

```text
pbin
```

The project may choose a better final name.

### Required commands

```bash
# Configure server/token
pbin auth login --server https://paste.example.com
pbin auth status
pbin auth logout

# stdin
echo "hello" | pbin create --name hello.txt

# one file
pbin create script.py

# directory
pbin create ./project

# current directory
pbin create .

# expiry
pbin create . --expires 7d

# burn after read
pbin create secret.txt --burn

# password (prompt securely; do not place secret in shell history)
pbin create secret.txt --password

# delete an owned paste
pbin delete AbCDef123456
```

### Authentication UX

Simplest acceptable flow:

```bash
pbin auth login --server https://paste.example.com
Token: [hidden input]
```

Store credentials:

- preferably in the OS credential/keychain service if practical;
- otherwise in a config file with permission `0600`.

Do not require Git.

### Output behavior

By default successful creation should print **only the URL** so shell scripting is easy:

```bash
url="$(pbin create .)"
```

Human-readable detail may go to stderr.

Support `--json` for scripts.

### Password input

`--password` should prompt interactively.

Do not encourage:

```bash
--password supersecret
```

because shell history/process listings can expose it.

A `--password-stdin` option is acceptable for automation.

---

## 12. HTTP API

Version all public APIs:

```text
/api/v1/
```

### Required endpoints

Names may vary, but equivalent behavior must exist.

#### Create anonymous/authenticated single-file paste

```text
POST /api/v1/pastes
```

Support JSON for easy curl/CLI usage:

```json
{
  "filename": "hello.py",
  "content": "print('hello')\n",
  "title": "example",
  "expires_in": "7d",
  "burn_after_read": false,
  "password": null
}
```

Anonymous policy applies when no valid token/session is present.

#### Create authenticated bundle/folder paste

```text
POST /api/v1/pastes/bundle
Authorization: Bearer ...
```

Multipart manifest protocol as described above.

This endpoint must reject anonymous requests.

#### Metadata

```text
GET /api/v1/pastes/{public_id}/meta
```

Must not accidentally expose protected filenames/content.

For password-protected pastes, return only safe information such as:

```text
exists
password_required
burn_after_read
expires_at
```

#### File content

Use an immutable internal file identifier where possible:

```text
GET /api/v1/pastes/{public_id}/files/{file_id}
```

Do not construct filesystem paths directly from request path segments.

#### Raw

```text
GET /api/v1/pastes/{public_id}/files/{file_id}/raw
```

Return a safe text content type and applicable no-index headers.

#### ZIP

```text
GET /api/v1/pastes/{public_id}/archive.zip
```

Requirements:

- stream ZIP generation when possible;
- preserve relative folder structure;
- enforce paste access policy;
- never include server paths;
- use safe filenames;
- set `Content-Disposition: attachment`.

#### Delete

```text
DELETE /api/v1/pastes/{public_id}
```

Authorization via:

- owner account/token;
- admin;
- anonymous creator delete token.

### Error format

Use one small consistent JSON error shape:

```json
{
  "error": {
    "code": "folder_upload_requires_auth",
    "message": "Folder uploads require authentication."
  }
}
```

Avoid giant API envelopes.

---

## 13. Burn-After-Read Semantics

This must be designed deliberately.

A simple `GET` that immediately burns content is incorrect because:

- chat clients fetch links for previews;
- antivirus products scan URLs;
- browsers may prefetch;
- crawlers may request pages.

### Required flow

For a burn paste:

1. `GET /p/{id}` returns a safe landing page/metadata only.
2. It does **not** consume the paste.
3. The page clearly says that revealing it will consume it.
4. User explicitly clicks **Reveal**.
5. A state-changing `POST` performs the reveal.
6. Exactly one reveal succeeds globally.

This pattern is inspired by secret-sharing systems that deliberately separate metadata lookup from content retrieval so previews cannot consume one-time links.

### Multi-file burn pastes

A multi-file burn paste cannot delete itself after delivering only the first file or the recipient could never browse the tree.

Use a **single-consumer viewer lease**:

1. First successful reveal atomically marks the paste consumed.
2. It creates a random, short-lived viewer lease/session for that browser.
3. No other browser/session may reveal it.
4. The winning session may browse all files and download the ZIP during a short grace period, e.g. 10–15 minutes.
5. When the lease expires, paste files are hard-deleted.
6. Refreshing within the valid winning session should continue to work.
7. A second browser gets `410 Gone`.

This provides useful "read once" behavior for a folder while remaining resistant to preview bots.

For a single-file paste, the same model may be used for consistency.

### Concurrency

The reveal transition must be atomic.

A test with two simultaneous reveal requests must produce:

```text
1 success
1 gone/conflict
```

Never two successful reveals.

---

## 14. Password-Protected Pastes

Password protection is access control, not account authentication.

Requirements:

- hash paste passwords using Argon2id;
- never store plaintext passwords;
- rate-limit password attempts;
- use generic failure messages;
- do not reveal file names before successful password verification;
- issue a short-lived paste-access session after successful verification;
- paste-access sessions must not grant account privileges;
- the ZIP and raw endpoints must enforce the same authorization.

Password protection may be combined with burn-after-read.

For `password + burn`:

1. metadata page loads without burning;
2. user submits password;
3. only after successful password verification may the explicit burn/reveal operation occur.

A wrong password must never burn the paste.

---

## 15. Privacy and Discoverability

This is a strict requirement.

### No public discovery

Do not implement:

- `/recent`;
- `/public`;
- `/trending`;
- anonymous search;
- sitewide browse;
- public user paste listings;
- RSS feeds containing pastes;
- paste sitemap entries.

The homepage is a creation page, not a feed.

Authenticated users may have a private **My Pastes** page showing only their own pastes.

Admins may have a moderation page showing paste metadata and delete controls.

### Search-engine protection

Every paste-related response should include:

```text
X-Robots-Tag: noindex, nofollow, noarchive, nosnippet
```

Paste HTML should also include:

```html
<meta name="robots" content="noindex,nofollow,noarchive,nosnippet">
```

Provide a restrictive `robots.txt`.

Do not put paste IDs into `sitemap.xml`.

Do not expose paste URLs in global HTML pages.

Use high-entropy non-sequential IDs so unlisted pastes cannot reasonably be enumerated.

Recommended: at least ~96 bits of randomness.

---

## 16. Web UI

The UI should feel closer to a small terminal/editor tool than a cloud storage dashboard.

### 16.1 Visual principles

- very small header;
- no marketing hero section after setup;
- no sidebar unless viewing a multi-file paste;
- system font for general UI;
- monospace for code;
- responsive;
- accessible;
- light and dark themes using system preference;
- minimal animations;
- no giant cards;
- no excessive gradients;
- no dashboard bloat.

### 16.2 Create page

Default route:

```text
/
```

Anonymous view:

```text
┌────────────────────────────────────────────┐
│ filename: paste.txt                        │
├────────────────────────────────────────────┤
│                                            │
│  editor                                    │
│                                            │
├────────────────────────────────────────────┤
│ Expiry  [90 days]                          │
│ [ ] Password    [ ] Burn after read        │
│                               [Create]     │
└────────────────────────────────────────────┘
```

Authenticated users additionally get:

```text
[ Add file ] [ Upload files ] [ Upload folder ]
```

Anonymous users must not be shown an enabled folder upload control.

### 16.3 Web folder upload

For authenticated users:

- support drag/drop of directories where the browser exposes directory entries;
- provide an explicit **Upload folder** file-picker fallback using browser directory-selection support where available;
- preserve `webkitRelativePath` or equivalent relative paths;
- validate again on the server.

The UI must never imply folder upload succeeded until the server confirms the atomic paste creation.

### 16.4 Viewer: single file

For a one-file paste, avoid wasting space on an empty tree sidebar.

Layout:

```text
┌───────────────────────────────────────────────────┐
│ title         expires in 6d       copy  raw  ↓   │
├───────────────────────────────────────────────────┤
│  1  const hello = "world";                        │
│  2                                                │
└───────────────────────────────────────────────────┘
```

### 16.5 Viewer: folder / multi-file paste

Desktop layout should resemble a minimal editor/Neovim setup:

```text
┌──────────────────────────────────────────────────────────────┐
│ project-name       expires in 29d       copy URL   ZIP ↓    │
├────────────────────┬─────────────────────────────────────────┤
│ Files              │ references/supplier-email.md            │
│                    ├─────────────────────────────────────────┤
│ ▾ evals/           │  1  # Supplier Email                    │
│   evals.json       │  2                                      │
│ ▾ references/      │  3  ...                                 │
│   reading...md     │                                         │
│   supplier...md    │                                         │
│   tb1...md         │                                         │
│ SKILL.md           │                                         │
└────────────────────┴─────────────────────────────────────────┘
```

Requirements:

- collapsible directory nodes;
- currently selected file visibly highlighted;
- selected path shown above content;
- syntax highlighting;
- line numbers;
- scroll tree and file pane independently where useful;
- URL may optionally preserve selected file, e.g. query/hash state;
- keyboard navigation should work with standard arrows/Enter;
- mobile tree becomes a slide-over/drawer or compact file picker.

Do not turn this into a full IDE.

### 16.6 Viewer actions

Keep actions small:

- Copy URL
- Copy file
- Raw
- Download file
- Download ZIP
- Delete (owner/admin)
- expiry status

No social/share-menu bloat.

### 16.7 Markdown

Markdown source must always be viewable as text.

A sanitized **Preview** toggle is optional.

If implemented:

- raw HTML must be disabled or sanitized;
- JavaScript must never execute;
- remote active content must not run.

Markdown preview should not delay MVP.

---

## 17. Syntax Highlighting

Requirements:

- detect language from extension/name first;
- optional lightweight content detection fallback;
- plain text must always work;
- highlighting must never execute uploaded code;
- unsupported languages gracefully fall back to text.

Prefer a lightweight solution suitable to the chosen backend/frontend stack.

Do not ship hundreds of language bundles to the client if only a small subset is necessary.

---

## 18. ZIP Downloads

Folder/multi-file pastes need first-class ZIP export.

Requirements:

- endpoint downloads one archive containing the complete paste tree;
- preserve relative paths exactly after normalization;
- stream ZIP rather than creating a permanent duplicate archive;
- sanitize the archive root/title;
- never expose internal database IDs or server filesystem paths;
- password/burn authorization applies;
- expired pastes return `410 Gone` or `404` consistently;
- ZIP request does not extend paste expiry.

Recommended archive:

```text
trellis-buyer.zip
└── trellis-buyer/
    ├── SKILL.md
    ├── evals/
    │   └── evals.json
    └── references/
        └── supplier-email.md
```

For burn-after-read, only the winning reveal session can download the ZIP during its lease.

---

## 19. Expiry and Cleanup

Expiration must be enforced in two places.

### Request-time enforcement

Every paste fetch checks:

```text
now >= expires_at
```

If expired:

- refuse access immediately;
- return `410 Gone` or the chosen consistent response;
- enqueue/delete data.

This prevents a slow cleanup worker from making expired data accessible.

### Background cleanup

Run a lightweight periodic cleanup task, for example every 5 minutes:

- delete expired paste files;
- delete expired paste rows;
- delete expired burn leases;
- delete used/expired invitations after retention period;
- delete expired sessions;
- prune anonymous rate-limit events older than 24 hours.

Do not require Redis/Celery/Sidekiq merely for cleanup.

The application process itself can run this scheduler in a single-node deployment.

If multiple replicas are supported later, introduce a DB advisory lock or equivalent before cleanup.

---

## 20. Database Choice

The implementation agent may choose **SQLite or PostgreSQL**.

Do not support both merely for checkbox value if doing so adds significant abstraction/bloat.

### Decision guidance

Prefer SQLite when:

- the expected deployment is one application container;
- writes are modest;
- simplicity and backup ease matter most.

Prefer PostgreSQL when:

- concurrent writes are expected to be significant;
- multiple application replicas are an immediate requirement;
- database-level concurrency makes burn/reveal transitions easier for the chosen stack.

Whichever is chosen must correctly support:

- atomic burn transitions;
- transactions;
- uniqueness constraints;
- expiry queries;
- invite consumption;
- first-admin bootstrap race protection.

### Storage approach

For v1, storing text/code contents in the database is acceptable and may be simpler than maintaining a second object-storage subsystem.

Avoid S3/MinIO as an MVP dependency unless the implementation agent can justify it.

No Redis is required for MVP.

---

## 21. Docker Deployment

The finished application must be production-deployable with Docker Compose.

Minimum expected shape:

### SQLite-style deployment

```text
app container
└── persistent /data volume
```

### PostgreSQL-style deployment

```text
app container
└── database container + persistent DB volume
```

The implementation agent decides which architecture to ship.

### Required environment/config concepts

Exact names may differ:

```text
BASE_URL=https://paste.example.com
LISTEN_ADDR=0.0.0.0:8080
DATA_DIR=/data

SESSION_SECRET=...
RATE_LIMIT_SECRET=...

DEFAULT_PASTE_TTL=2160h
MAX_PASTE_TTL=2160h

ANON_MAX_PASTES_24H=5
ANON_MAX_FILES_PER_PASTE=1
ANON_MAX_TOTAL_BYTES=1048576

AUTH_MAX_FILES_PER_PASTE=500
AUTH_MAX_FILE_BYTES=2097152
AUTH_MAX_TOTAL_BYTES=26214400

TRUSTED_PROXIES=...
```

If PostgreSQL is selected:

```text
DATABASE_URL=postgres://...
```

If SQLite is selected:

```text
DATABASE_PATH=/data/pastebin.sqlite
```

### Container requirements

- run as a non-root user where practical;
- support graceful SIGTERM shutdown;
- include `/healthz`;
- health endpoint must not require authentication;
- migrations must be deterministic;
- persistent files must live only in documented mounted paths;
- logs go to stdout/stderr;
- no telemetry or phone-home behavior;
- work correctly behind Traefik/nginx/Caddy;
- respect forwarded HTTPS scheme only from trusted proxies.

### Reverse proxy

The application should work with TLS terminated at a reverse proxy.

It must correctly generate external URLs from explicit `BASE_URL`, not by blindly trusting incoming Host headers.

---

## 22. Health and Observability

Keep this minimal.

Required:

```text
GET /healthz
```

Returns success when:

- process is healthy;
- database can be reached.

Optional:

```text
GET /readyz
```

No elaborate observability stack is required.

Use structured logs where practical.

Log:

- startup/version;
- migration results;
- login successes/failures at a safe level;
- admin actions;
- paste creation metadata such as size/count, but **not paste contents**;
- cleanup counts;
- rate-limit events.

Never log:

- paste body;
- paste password;
- account password;
- API token plaintext;
- invitation token plaintext;
- delete token plaintext.

---

## 23. Security Requirements

### Uploaded content is always untrusted

Never serve uploaded HTML in a way that lets it execute in the application origin.

For raw source:

```text
Content-Type: text/plain; charset=utf-8
X-Content-Type-Options: nosniff
```

For downloadable content:

```text
Content-Disposition: attachment
```

Use a strict Content Security Policy.

Do not execute or iframe user-provided code.

### Server-side request forgery

The app has no reason to fetch user-supplied URLs.

Do not implement URL previews or server-side remote imports in MVP.

That removes an entire SSRF class.

### CSRF

All browser state mutations need CSRF protection.

Bearer-token API calls do not need cookie-CSRF semantics if they do not also accept browser session authentication unintentionally.

### Brute force

Rate-limit:

- login attempts;
- password-protected paste unlock attempts;
- invite redemption attempts;
- token-sensitive endpoints.

### Path traversal

The folder API must have dedicated tests for:

```text
../secret
../../etc/passwd
/foo
C:\foo
\\server\share
a/../../../b
a//b
a/./b
NUL-containing names
duplicate normalized paths
```

### Resource exhaustion

Enforce limits while streaming, not only after buffering the whole request.

Abort as soon as:

- total bytes exceed quota;
- file count exceeds quota;
- individual file exceeds quota;
- path count/depth exceeds quota.

### Secrets in CLI uploads

A useful lightweight safety feature:

Before uploading a directory interactively, warn if obvious sensitive filenames are included, such as:

```text
.env
.env.*
id_rsa
id_ed25519
*.pem
*.key
```

Do not block them outright because secret sharing may be intentional.

Support a non-interactive override.

---

## 24. HTTP Response Behavior

Recommended status conventions:

```text
200 OK              normal fetch
201 Created         paste created
204 No Content      delete success
400 Bad Request     malformed request/path
401 Unauthorized    login/token required
403 Forbidden       valid identity lacks permission
404 Not Found       unknown/unavailable ID where hiding existence is desirable
409 Conflict        one-time reveal lost race, duplicate state transition
410 Gone            known paste expired/burned, if product chooses to reveal this state
413 Payload Too Large
429 Too Many Requests
```

Choose whether expired/burned IDs return 404 or 410 and use the choice consistently.

---

## 25. Suggested Routes

Web:

```text
/                       create paste
/login                  login
/setup                  first-admin bootstrap only
/invite/{token}         invite redemption
/p/{id}                 paste viewer
/me/pastes              authenticated user's own pastes
/settings/tokens        personal API tokens
/admin                  admin
/admin/users            users
/admin/invites          invitation links
```

There must be no public paste listing route.

API:

```text
/api/v1/pastes
/api/v1/pastes/bundle
/api/v1/pastes/{id}/meta
/api/v1/pastes/{id}/files/{file_id}
/api/v1/pastes/{id}/files/{file_id}/raw
/api/v1/pastes/{id}/archive.zip
/api/v1/pastes/{id}/reveal
/api/v1/pastes/{id}
/api/v1/me/pastes
/api/v1/tokens
/api/v1/admin/invites
```

The implementation agent may adjust naming while retaining behavior.

---

## 26. Creation Responses

A normal authenticated API creation response should be small:

```json
{
  "id": "AbCDef123456",
  "url": "https://paste.example.com/p/AbCDef123456",
  "expires_at": "2027-01-04T12:00:00Z",
  "files": 14,
  "bytes": 183240
}
```

Anonymous creation additionally returns a delete token exactly once:

```json
{
  "id": "AbCDef123456",
  "url": "https://paste.example.com/p/AbCDef123456",
  "delete_token": "pd_...",
  "expires_at": "2027-01-04T12:00:00Z"
}
```

The web UI should strongly encourage the anonymous creator to save the delete link/token if they may want early deletion.

---

## 27. Authenticated "My Pastes"

A signed-in user may see their own pastes.

Keep it deliberately simple:

```text
Title | Created | Expires | Files | Protection | Delete
```

Do not add engagement metrics.

Optional:

- sort newest/oldest;
- search only the user's own titles/file paths.

Do not expose view counts unless there is a clear need.

---

## 28. Admin Interface

Admin panel should be compact.

Required:

### Users

- list users;
- disable/delete a user;
- promote/demote only if needed;
- prevent accidental deletion of the last admin.

### Invitations

- create invite;
- optional email constraint;
- expiry;
- revoke unused invite;
- show used/expired state.

Only admins can create invites.

### Paste moderation

Admin may:

- locate a paste by exact ID;
- see safe metadata;
- delete it.

Do not build a public content browser disguised as an admin feature.

### Instance settings

Only settings that are useful at runtime:

- default/max expiry;
- anonymous creation enabled;
- anonymous 5/day limit;
- size/file limits;
- optional maintenance banner.

Infrastructure secrets remain environment configuration, not editable through the web UI.

---

## 29. Accessibility

The minimal UI still needs proper accessibility.

Required:

- keyboard-operable tree;
- visible focus states;
- semantic buttons;
- labels for form controls;
- sufficient contrast;
- `aria-expanded` on tree nodes;
- `aria-current` or equivalent on selected file;
- no information conveyed only by color.

---

## 30. Performance Targets

This is a small self-hosted tool.

Reasonable targets on a modest homelab server:

- server startup: a few seconds or less;
- initial HTML + essential CSS/JS: keep small;
- no multi-megabyte JS bundle;
- normal paste view should not load every file body eagerly;
- load tree metadata first, then selected file;
- ZIP creation should stream;
- upload processing should stream where framework permits.

Exception: burn-after-read may need special access/session behavior as described earlier.

---

## 31. Recommended Implementation Order

### Phase 1 — skeleton

- chosen backend stack;
- DB migrations;
- Docker image/Compose;
- health endpoint;
- base layout;
- first-admin setup;
- login/logout;
- invite-only account creation.

### Phase 2 — single-file paste

- paste schema;
- anonymous single-file create;
- authenticated single-file create;
- 5-per-24h anonymous limiter;
- expiry;
- password protection;
- paste viewer;
- syntax highlighting;
- delete token;
- noindex/privacy headers.

### Phase 3 — multi-file/folder model

- `paste_files` path model;
- multi-file API;
- tree viewer;
- selected-file lazy loading;
- folder upload in authenticated web UI;
- ZIP streaming;
- path traversal hardening.

### Phase 4 — API tokens + CLI

- PAT creation/revocation;
- `pbin auth`;
- stdin/file uploads;
- recursive directory walker;
- ignore rules;
- multipart manifest upload;
- machine-readable output.

### Phase 5 — burn after read

- explicit Reveal UI;
- atomic consume operation;
- scanner-safe metadata flow;
- viewer lease for multi-file pastes;
- race-condition tests.

### Phase 6 — polish and hardening

- mobile tree UX;
- accessibility;
- security headers/CSP;
- brute-force limits;
- cleanup worker;
- admin moderation;
- backup documentation;
- end-to-end tests.

Do not start with social/polish features before core paste semantics are correct.

---

## 32. Required Automated Tests

### Paste creation

- anonymous can create one file;
- anonymous second file is rejected;
- anonymous bundle endpoint is rejected;
- authenticated user can create one file;
- authenticated user can create nested folder tree;
- duplicate paths are rejected;
- empty paste rejected.

### Anonymous rate limit

- first 5 creations in rolling 24h succeed;
- sixth fails with 429;
- raw IP is not stored;
- events older than 24h stop counting;
- proxy headers from untrusted peers are ignored.

### Expiry

- default is 90 days;
- shorter expiry works;
- value above max is rejected/clamped according to documented behavior;
- expired paste cannot be read even before cleanup worker runs;
- cleanup removes expired content.

### Password

- correct password unlocks;
- incorrect password fails;
- filenames remain hidden while locked;
- ZIP/raw endpoints cannot bypass password;
- password hash is not plaintext.

### Burn after read

- initial page GET does not burn;
- metadata/API preview does not burn;
- wrong password does not burn;
- first explicit reveal wins;
- second independent reveal fails;
- concurrent reveal race allows exactly one winner;
- winning lease can navigate files during grace period;
- losing browser cannot fetch file endpoints;
- files are deleted after lease expiry.

### Tree/path handling

Test rejection of traversal and malformed paths.

Test:

```text
src/index.ts
src/lib/foo.ts
README.md
```

renders as the correct tree.

### ZIP

- archive contains all files;
- hierarchy preserved;
- no absolute paths;
- no traversal names;
- password rules apply;
- burn lease rules apply.

### Auth

- first user becomes admin;
- second unauthenticated signup cannot occur;
- admin can create invite;
- user cannot create invite;
- expired/reused invite fails;
- last admin cannot be accidentally removed if that would lock the instance.

### Tokens

- PAT is shown once;
- DB contains hash, not token;
- revoked token fails;
- expired token fails;
- missing scope fails.

### UI/privacy

- no public list page;
- paste pages emit `X-Robots-Tag`;
- robots directives are present;
- sitemap contains no paste IDs.

---

## 33. End-to-End Acceptance Scenarios

The build is not done until these workflows pass.

### Scenario A — anonymous normal paste

```text
Open /
paste text
choose 1 day
Create
copy URL
recipient opens URL
recipient sees highlighted content
paste disappears after 1 day
```

Anonymous user has no account.

### Scenario B — anonymous sixth paste

```text
same anonymous client creates five pastes within 24h
sixth creation -> HTTP 429 + useful message
```

### Scenario C — authenticated folder paste from CLI

```bash
pbin auth login --server https://paste.example.com
pbin create ./trellis-buyer --expires 30d
```

Output:

```text
https://paste.example.com/p/AbCDef123456
```

Opening URL displays:

```text
SKILL.md
evals/
  evals.json
references/
  reading-replies.md
  supplier-email.md
```

Clicking each file changes the right-hand code panel.

ZIP download recreates the hierarchy.

### Scenario D — anonymous folder attempt

Anonymous request with two files or nested paths:

```text
403/401 folder_upload_requires_auth
```

It must not partially create a paste.

### Scenario E — password folder paste

Recipient opens URL.

Before password:

- no filenames;
- no source;
- no ZIP.

After correct password:

- tree appears;
- files are readable;
- ZIP works.

### Scenario F — burn-after-read folder

Recipient A opens link:

```text
"This paste will be destroyed after you reveal it."
[Reveal]
```

Recipient A clicks Reveal.

A receives the tree and a short viewer lease.

Recipient B then attempts the same URL:

```text
Paste has already been consumed.
```

A can browse the folder for the lease duration.

After lease expiry the stored contents are deleted.

### Scenario G — invite-only account

Fresh install:

```text
first account -> admin
```

Then:

```text
/register -> unavailable
```

Admin creates invite URL.

Invitee uses it once and becomes normal user.

The link cannot be used again.

---

## 34. Explicit Non-Goals for v1

Do not implement these unless all required functionality is finished and tested:

- Git clone/push support;
- revisions/history;
- collaborative editing;
- comments;
- likes;
- forks;
- public feed;
- public search;
- organizations;
- teams;
- OAuth provider matrix;
- custom domains per user;
- URL shortener;
- QR codes;
- PWA/offline mode;
- email delivery dependency;
- image galleries;
- video/audio preview;
- general cloud storage;
- S3-compatible storage;
- Redis;
- Elasticsearch;
- background-job infrastructure;
- AI language detection;
- analytics;
- telemetry.

This list exists specifically to protect the project from feature creep.

---

## 35. Nice-to-Have Features After MVP

Only consider these after all acceptance criteria pass.

### High value / low bloat

- optional TOTP for admin;
- line permalink anchors such as `#L20-L30`;
- sanitized Markdown preview;
- keyboard shortcut to toggle file tree;
- copy current file path;
- optional paste title;
- configurable branded instance name/favicon;
- `.pasteignore`;
- shell completion for CLI;
- one-command CLI installer/release binaries.

### Defer unless needed

- OAuth/OIDC;
- multiple admin roles;
- object storage;
- version history;
- API webhooks;
- metrics endpoint.

---

## 36. Suggested UX Copy

Keep language simple.

### Default access

```text
Anyone with the link can view this paste.
It will not be listed publicly.
```

### Password

```text
Require a password
```

### Burn

```text
Burn after first reveal
The first person who reveals this paste gets temporary access.
After that, the link cannot be opened again.
```

### Expiry

```text
Expires after
[ 90 days ▼ ]
```

### Anonymous folder restriction

```text
Sign in to upload folders or multiple files.
Anonymous pastes are limited to one file.
```

### Rate limit

```text
Anonymous uploads are limited to 5 pastes per 24 hours.
Sign in to continue uploading.
```

---

## 37. Configuration Defaults

Recommended shipped defaults:

```text
Default paste expiry                90 days
Maximum paste expiry                90 days

Anonymous creation                  enabled
Anonymous pastes / rolling 24h      5
Anonymous files / paste             1
Anonymous max bytes / paste         1 MiB

Authenticated files / paste         500
Authenticated max file bytes        2 MiB
Authenticated max total bytes       25 MiB

Maximum path depth                  20
Maximum normalized path length      512 bytes

Burn viewer lease                   15 minutes

Public signup                       disabled
Admin invitations                   enabled

Paste discovery                     disabled/nonexistent
Search-engine indexing              disabled
Telemetry                           disabled
```

The admin should be able to tune operational limits, but secure/minimal defaults matter.

---

## 38. Definition of Done

The project is complete when:

- [ ] Docker Compose brings up a fresh instance.
- [ ] First user becomes admin.
- [ ] Open signup is impossible afterward.
- [ ] Only admin can create invite links.
- [ ] Invite links are expiring and single-use.
- [ ] Anonymous users can create at most five pastes in a rolling 24h.
- [ ] Anonymous pastes are single-file only.
- [ ] Anonymous folder/multi-file uploads are rejected server-side.
- [ ] Authenticated web/API/CLI folder upload preserves nested paths.
- [ ] A folder is one paste with one URL.
- [ ] Default/max normal expiry is 90 days.
- [ ] Expired data is deleted.
- [ ] Password protection covers tree, files, raw endpoints, and ZIP.
- [ ] Burn-after-read is scanner-safe and atomic.
- [ ] Folder viewer has a clean left tree and right code pane.
- [ ] Single-file viewer does not waste space on an unnecessary tree.
- [ ] Folder ZIP preserves hierarchy.
- [ ] All pastes are unlisted and non-indexable.
- [ ] There is no public feed/search/trending page.
- [ ] API tokens are hashed and revocable.
- [ ] CLI can upload stdin, a file, and a recursive folder.
- [ ] CLI folder upload requires authentication.
- [ ] Path traversal tests pass.
- [ ] Security headers and CSRF protections are in place.
- [ ] No telemetry or unnecessary external dependencies exist.
- [ ] Automated tests cover the acceptance scenarios above.
- [ ] README documents deployment, backup, restore, CLI setup, and reverse-proxy configuration.

---

## 39. Research-Informed Design Notes

The requirements above intentionally borrow proven ideas from existing self-hosted paste/secret tools without inheriting their unrelated features.

### PrivateBin

Useful ideas:

- expiration;
- password protection;
- burn-after-read;
- `robots.txt` / anti-indexing posture;
- privacy-first default behavior.

Sources:

- https://privatebin.info/
- https://github.com/PrivateBin/PrivateBin

### Shhh

Useful ideas:

- explicit reveal so link-preview bots do not consume burn links;
- atomic read limits;
- invitations;
- closed registration;
- admin-controlled quotas/rate limits;
- TOTP as an optional hardening feature.

Source:

- https://github.com/thoda-dev/shhh

### OpenGist

Useful ideas:

- first created user becomes admin;
- signup can be disabled;
- invitation URLs;
- scoped Personal Access Tokens;
- ZIP download;
- code-centric UI.

Important difference:

This project must support real nested paths in one paste; it must not inherit OpenGist's flat-file limitation.

Sources:

- https://opengist.io/docs/
- https://opengist.io/docs/api
- https://opengist.io/docs/configuration/admin-panel.html

### CasPaste

Useful ideas:

- one service can support single-file and multi-file Gist-style pastes;
- lightweight native CLI;
- API-token workflow;
- expiration and burn flags;
- keep paste tooling developer-friendly.

Source:

- https://github.com/webappsgo/caspaste

### MSK Paste

Useful idea:

- privacy-preserving rate limiting using keyed hashes rather than persisting raw client IPs.

Source:

- https://github.com/MSK-Scripts/msk-paste

### MicroBin / BitVault

Useful ideas:

- very small operational footprint;
- single-binary/self-contained deployment mentality;
- avoid unnecessary infrastructure for a pastebin.

Sources:

- https://github.com/szabodanika/microbin
- https://github.com/thethingexe/bitvault

---

## 40. Instruction to the Implementation Agent

Treat this file as the product contract.

When there is ambiguity:

1. choose the smaller implementation;
2. protect privacy by default;
3. preserve the one-paste/one-tree model;
4. do not weaken server-side authorization;
5. do not add discoverability;
6. do not add infrastructure without a concrete need;
7. do not add features merely because comparable products have them.

Before implementation, write a short `ARCHITECTURE.md` that records:

- chosen language/framework;
- SQLite **or** PostgreSQL choice and rationale;
- exact storage representation for `paste_files.content`;
- burn-after-read transaction/lease design;
- authentication/session library choice;
- syntax-highlighting approach;
- CLI language/packaging;
- multipart bundle-upload format;
- limits selected from this plan.

Then implement in the phased order above.

The highest-risk areas are:

1. nested path validation;
2. anonymous rate-limit enforcement;
3. first-admin bootstrap race;
4. invitation consumption race;
5. password authorization consistency;
6. burn-after-read concurrency;
7. ZIP path safety;
8. trusted proxy/IP handling.

Write tests for those before polishing the UI.
