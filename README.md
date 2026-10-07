# paste

Minimal self-hosted multi-file pastebin (Gist-style trees, invite-only, CLI).

## Quick start (Compose)

```bash
cp .env.example .env
# Set SESSION_SECRET to a long random value before any real deployment.
docker compose up --build -d
curl -fsS http://localhost:8080/healthz
```

Open the `BASE_URL` from `.env`, complete `/setup` as the first admin, then invite users from `/admin/invites`.

SQLite and app data persist in the Docker named volume `paste-data` mounted at `/data`.

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `BASE_URL` | `http://localhost:8080` | Public origin for generated links (invite URLs, paste URLs). Must match what browsers and the CLI use. |
| `LISTEN_ADDR` | `0.0.0.0:8080` | Bind address inside the process/container |
| `DATA_DIR` | `/data` | Persistent data directory |
| `DATABASE_PATH` | `$DATA_DIR/paste.sqlite` | SQLite file path |
| `SESSION_SECRET` | _(required)_ | Replace the compose default before production use |

Optional host port mapping: `PASTE_PORT` (default `8080`) in `.env`.

## Reverse proxy and `BASE_URL`

Terminate TLS at Traefik, Caddy, nginx, or similar and proxy to the container on port 8080.

Set `BASE_URL` to the **public** origin clients use (scheme + host, no trailing slash), for example `https://paste.example.com`. The app uses `BASE_URL` for invite and paste links; it does not invent the public URL from the incoming `Host` header.

Example (Caddy):

```
paste.example.com {
    reverse_proxy paste:8080
}
```

With that proxy, compose env should include `BASE_URL=https://paste.example.com`.

## Backup and restore

All durable state lives under `/data` (SQLite database by default).

**Backup** (while the stack is stopped, or after a brief pause for a consistent copy):

```bash
docker compose stop paste
docker run --rm -v paste-data:/data -v "$PWD:/backup" alpine \
  tar czf /backup/paste-data-$(date -u +%Y%m%dT%H%M%SZ).tar.gz -C /data .
docker compose start paste
```

**Restore:**

```bash
docker compose stop paste
docker run --rm -v paste-data:/data -v "$PWD:/backup" alpine \
  sh -c 'rm -rf /data/* && tar xzf /backup/paste-data-YYYYMMDDThhmmssZ.tar.gz -C /data'
docker compose start paste
```

Copy the SQLite file alone only if you understand WAL sidecars; preferring a full `/data` archive is safer.

## CLI (`pbin`)

Install from any directory, without cloning the repository (requires [Go](https://go.dev/dl/) 1.26 or newer):

```bash
go install github.com/harshalranjhani/paste/cmd/pbin@latest
```

The repository must be **public** for this command to work without GitHub credentials. Go installs `pbin` into `GOBIN`, or `$(go env GOPATH)/bin` by default. Add that directory to your `PATH` if your shell cannot find `pbin`. To install a specific release, replace `@latest` with its tag, such as `@v0.1.0`.

Without Go, download the archive for your machine from [GitHub Releases](https://github.com/harshalranjhani/paste/releases/latest). Extract `pbin` (or `pbin.exe` on Windows) and put it in a directory on your `PATH`. Downloads cover Linux, macOS (`darwin`), and Windows, each for Intel/AMD (`amd64`) and ARM (`arm64`). Each release includes `checksums.txt` for SHA-256 verification.

Authenticate with a Personal Access Token from `/settings/tokens` (scopes `paste:create`, `paste:read`, `paste:delete` as needed):

```bash
pbin auth login --server https://paste.example.com
# prompts for the token (hidden); stores credentials in a 0600 config file
pbin auth status
pbin create ./path/to/file.go
pbin create ./path/to/directory   # multi-file tree via bundle API
pbin delete <paste-id>
pbin auth logout
```

Successful `create` prints only the paste URL on stdout. Config directory defaults to the OS config path; override with `PBIN_CONFIG_DIR`.

## Releasing the CLI

Push a semantic version tag to run [.github/workflows/release.yml](.github/workflows/release.yml):

```bash
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin v0.2.0
```

The workflow runs the Go suite, builds six archives with [scripts/build-cli-release.sh](scripts/build-cli-release.sh), and publishes them with checksums and generated release notes. Tags with a suffix such as `v0.2.0-beta.1` create prereleases. Use a new tag for each version. To build the archives locally, run `bash scripts/build-cli-release.sh v0.1.0` from the repository root (requires Go, Bash, Python 3, tar, and sha256sum).

## Development

```bash
go test ./...
go run ./cmd/pastebin
```

HTTP tests boot the app against a temporary SQLite database via `internal/apptest`.

The UI uses Tailwind CSS with Go-rendered HTML and a small progressive JavaScript file. It starts in dark mode; the header toggle remembers your light/dark preference in the browser. The file viewer fills the window, with independently scrolling files and code, larger text, and a fullscreen control where the browser supports it. Chroma highlights its full language catalog with matching light/dark styles; use the syntax selector to override detection for ambiguous filenames. Unknown formats remain readable as plain text.

Assets are embedded in the Go binary; no CDN or Node process is needed at runtime. The compiled stylesheet is checked in so normal Go builds work directly. After changing templates or styles, rebuild it before restarting the server:

```bash
npm ci
npm run build:css
# Or keep CSS rebuilding while editing:
npm run watch:css
```

Docker builds compile the stylesheet automatically in a separate Node build stage. The runtime image still contains only the Go server.

## Ops notes

- Process listens for `SIGTERM`/`SIGINT` and shuts down the HTTP server gracefully.
- Container runs as non-root user `paste`.
- Logs go to stdout/stderr (JSON). No telemetry.
- In-process cleanup periodically deletes expired pastes (and file bodies), sessions, paste-access sessions, and expired invites.

## Docs

| Doc | Purpose |
|-----|---------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Frozen MVP stack choices |
| [PLAN.md](PLAN.md) | Original long-form product contract |
| [.scratch/paste/SPEC.md](.scratch/paste/SPEC.md) | MVP spec (wins over PLAN where they differ) |
| [.scratch/paste/issues/](.scratch/paste/issues/) | Tracer-bullet tickets |

## MVP freeze (short)

- Go + SQLite, single process, Docker Compose
- Auth required to create; nested folder = one unlisted URL
- Folder upload via API + `pbin` CLI (browser directory upload is out of scope for MVP)
- Password + expiry; burn-after-read and anonymous create are out of scope for MVP
