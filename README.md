# HHO — Household Objects

HHO is a **self-hosted home inventory and organization system**. You run it on your own
hardware (a Raspberry Pi, a NAS, a home server), and it helps a household track what it owns:
what's in the garage, which box has the holiday lights, whether the drill is still under
warranty. No cloud account, no subscription, no external service required to operate it.

HHO ships as two coupled products :

- **HHO Server** ( this one repository, `server/`) — a single Go binary / container. SQLite storage, an embedded
  Vue 3 web UI, and a documented public REST API. This is the whole product for a browser-only
  household.
- **HHO Android app** (`https://github.com/ankek/household-objects-android/android-app/`) — native Kotlin + Jetpack Compose, offline-first,
  barcode-driven stock control for standing in front of a shelf with no signal. Syncs to the
  server. *(Not yet functional — see [Project status](#project-status) below.)*

Licensed **AGPL-3.0-only**; see [License](#license).

## Project status

This project is under development — the HHO Server MVP

## Quickstart (server)

Requires Docker.

```bash
docker compose up -d
```

That's it — the committed [`compose.yaml`](compose.yaml) pulls a pinned multi-arch image
(`linux/amd64` + `linux/arm64`), creates a named data volume, and starts the server with every
setting at its working default (no `.env` file needed). Within about a minute the container
reports healthy; visit the web UI and complete the one-time registration to create your first
user and household group — every registration after that first one is disabled unless you opt in
(see below).

For anything beyond this — the bind-mount alternative and its `chown 65532:65532` step, the full
configuration reference, reverse-proxy/TLS notes, and upgrading — see the
[deploy guide](docs/deploying.md), and the [backup and restore runbook](docs/backup-restore.md)
for protecting your data.

### Configuration

The server starts with **zero required configuration** — every environment variable below is
optional and already shown at its default in `compose.yaml`:

| Variable | Default | Purpose |
|---|---|---|
| `HHO_DATA_DIR` | `/data` | Where the database, attachments, and backups live |
| `HHO_ADDR` | `:7745` | Listen address inside the container |
| `HHO_REGISTRATION_OPEN` | `false` | Allow a *second* household to self-register (the first ever registration is always allowed) |
| `HHO_TRUST_PROXY_HEADERS` | `false` | Derive the rate-limit key from `X-Forwarded-For` — only behind a trusted reverse proxy |
| `HHO_MAX_ATTACHMENT_SIZE_BYTES` | `26214400` (25 MiB) | Per-upload size ceiling |
| `HHO_ATTACHMENT_RECLAIM_INTERVAL_SECONDS` | `3600` | How often orphaned attachments are swept |
| `HHO_MIN_CLIENT_VERSION` | unset | Reject API clients below this version |
| `HHO_BACKUP_INTERVAL_SECONDS` | `86400` | Scheduled local backup cadence |
| `HHO_BACKUP_RETENTION_COUNT` | `7` | Scheduled backups kept before pruning |

The data directory has a fixed layout (`db/`, `attachments/`, `backups/`, `tmp/`) created
automatically inside your one mounted volume.

## Backup and restore

```bash
docker compose exec hho hho backup --output /data/backups/manual.tar.gz
```

produces one archive containing both the database snapshot and the attachments tree — safe to
run against a live server. The server also takes one automatically every 24 hours, keeping the
last 7, and an authenticated owner can pull the same archive from `GET /api/v1/backup` — the
**Download backup** link on the web UI's Settings screen. Restoring is CLI-only.

Restoring is a CLI operation against a **stopped** server, and never deletes what it replaces. The
full procedure — including getting backups off the box, which scheduled backups alone do not do —
is in the [backup and restore runbook](docs/backup-restore.md).

## Architecture

- **Server** — Go 1.25, `chi` router, SQLite via the CGO-free `modernc.org/sqlite` driver,
  `sqlc`-generated data access, `goose` migrations, structured logging. Builds as a single static
  binary with no external services (no Postgres, Redis, S3, or required reverse proxy). CI-gated
  at <50 MB idle RSS and a <40 MB container image.
- **Web UI** — Vue 3 (Composition API) + Pinia + Vite, built and embedded directly into the Go
  binary via `embed.FS`. There is no separate frontend deployment.
- **Android app** — Kotlin, Jetpack Compose, Material 3, Hilt for DI (package `dev.hho.android`).
  Designed offline-first around a durable local mirror and outbox; not yet implemented beyond its
  build scaffold.
- **API contract** — [`server/api/openapi.yaml`](server/api/openapi.yaml) is the single source of
  truth both the server and the Android client are built against; the running server also serves
  it at `/api/v1/openapi.yaml`. CI fails on drift between the document and the implementation.
- **Sync protocol** — the [sync protocol spec](docs/sync-protocol.md) defines the device-to-server
  replication contract (`/sync/pull`, `/sync/push`) that Phase 3/4 Android sync builds against;
  both routes are declared and mounted today but answer `501 Not Implemented` until then.

## License

HHO and all components are licensed under the **GNU Affero General Public License v3.0 only** (`AGPL-3.0-only`) — see
[`LICENSE`](LICENSE). The AGPL's network-use clause (section 13) is the deliberate reason it was
chosen over plain GPL: if you run a modified version of HHO as a network service, you must offer
its source to your users.
