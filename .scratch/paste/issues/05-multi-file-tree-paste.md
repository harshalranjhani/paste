# 05: Multi-file tree paste

**What to build:** An authenticated user creates one paste containing many nested text files (web multi-file add/picker and `POST /api/v1/pastes/bundle`). Creation is atomic. The viewer shows a left tree and right code pane with lazy file loading, keyboard/accessible navigation, and a mobile drawer. ZIP download preserves relative hierarchy and never includes server paths. Path traversal and malformed paths are rejected. Password protection from ticket 04 applies to tree, file, raw, and ZIP. Binary/non-UTF-8 rejected. No web directory-picker API required.

**Blocked by:** 03 — Single-file unlisted paste; 04 — Password-protected paste

**Status:** ready-for-agent

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [ ] Authenticated multi-file/bundle create preserves nested POSIX-relative paths under one URL
- [ ] Failed mid-upload leaves no partial paste
- [ ] Duplicate/traversal/absolute/Windows/UNC/NUL/over-depth paths are rejected
- [ ] Tree viewer loads metadata first, then selected file bodies; single-file layout still omits empty tree
- [ ] Keyboard navigation and basic tree ARIA work; mobile uses drawer/compact picker
- [ ] ZIP streams a safe archive with correct hierarchy; password gate applies
- [ ] Size/file-count limits enforced while reading the request (authenticated defaults from spec)
- [ ] Unauthenticated bundle/multi-file create is rejected
- [ ] Covered by HTTP-seam tests including path attack matrix and ZIP safety
