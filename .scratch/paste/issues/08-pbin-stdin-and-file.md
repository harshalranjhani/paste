# 08: pbin stdin and file

**What to build:** The official `pbin` CLI authenticates to a server with a PAT, creates single-file pastes from stdin or a file path, prints only the URL on stdout by default, supports `--json` / `--expires` / password prompt (or password-stdin), and can delete an owned paste. Credentials prefer OS keychain when practical, otherwise a `0600` config file.

**Blocked by:** 07 — Personal access tokens

**Status:** done

**Parent:** [.scratch/paste/SPEC.md](../SPEC.md)

- [x] `pbin auth login|status|logout` works against a configurable server URL
- [x] `echo hi | pbin create --name hello.txt` and `pbin create file.py` return a working paste URL
- [x] Successful create prints only the URL on stdout; human detail may go to stderr
- [x] `--json`, `--expires`, and interactive `--password` / `--password-stdin` work
- [x] `pbin delete <id>` deletes an owned paste
- [x] Credentials are not stored world-readable
- [x] Covered by CLI-against-test-server tests
