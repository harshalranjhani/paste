# 05: Multi-file tree paste

**What to build:** An authenticated user creates one paste containing many nested text files (web multi-file add/picker and `POST /api/v1/pastes/bundle`). Creation is atomic. The viewer shows a left tree and right code pane with lazy file loading, keyboard/accessible navigation, and a mobile drawer. ZIP download preserves relative hierarchy and never includes server paths. Path traversal and malformed paths are rejected. Password protection from ticket 04 applies to tree, file, raw, and ZIP. Binary/non-UTF-8 rejected. No web directory-picker API required.

**Blocked by:** 03 — Single-file unlisted paste; 04 — Password-protected paste

**Status:** done

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [x] Authenticated multi-file/bundle create preserves nested POSIX-relative paths under one URL
- [x] Failed mid-upload leaves no partial paste
- [x] Duplicate/traversal/absolute/Windows/UNC/NUL/over-depth paths are rejected
- [x] Tree viewer loads metadata first, then selected file bodies; single-file layout still omits empty tree
- [x] Keyboard navigation and basic tree ARIA work; mobile uses drawer/compact picker
- [x] ZIP streams a safe archive with correct hierarchy; password gate applies
- [x] Size/file-count limits enforced while reading the request (authenticated defaults from spec)
- [x] Unauthenticated bundle/multi-file create is rejected
- [x] Covered by HTTP-seam tests including path attack matrix and ZIP safety
