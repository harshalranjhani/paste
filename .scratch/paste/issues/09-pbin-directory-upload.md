# 09: pbin directory upload

**What to build:** `pbin create ./directory` recursively uploads accepted text files as one multi-file paste via the bundle API, preserving nested paths under a single URL. The CLI always ignores `.git/`, honors `.gitignore` when present and `.pasteignore`, skips symlinks, shows an interactive pre-upload summary, and warns on obvious sensitive filenames without hard-blocking. The resulting link opens in the tree viewer and ZIP round-trips the hierarchy.

**Blocked by:** 05 — Multi-file tree paste; 08 — pbin stdin and file

**Status:** done

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [x] Directory create produces one paste URL whose tree matches the uploaded relative paths
- [x] `.git/` is ignored; `.gitignore` and `.pasteignore` are honored when present
- [x] Symlinks are not followed/uploaded by default
- [x] Interactive runs show a file-count/bytes/expiry/visibility summary before upload
- [x] Sensitive-name warnings appear without blocking the upload
- [x] ZIP download from the resulting paste recreates the hierarchy
- [x] Limits and UTF-8/path rules still enforced server-side regardless of CLI claims
- [x] Covered by CLI-against-test-server tests
