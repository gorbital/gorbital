# ADR-0075: File storage

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0008, ADR-0029, ADR-0051, ADR-0066

## Context

Phase 10 of the [Dev Portal roadmap](../dev-portal-roadmap.md) is the Storage screen: buckets and driver status, a file browser (upload, download, delete, move, make directory, preview), signed URLs with an expiry and object metadata, and a guard against changing a production bucket by mistake. There was no storage in the framework: apps that keep files wrote their own client.

## Options

### Where the code lives

| Option | Verdict |
|---|---|
| A helper in the Dev Portal only | Rejected: the portal browses what the app stores; without a store in the app there is nothing to browse |
| **A library module, `gorbital.dev/modules/storage`: a `Store` interface (put, get, stat, delete, copy, list with prefixes, signed URLs) with two drivers, `local` (files under a directory, HMAC-signed links served by the app) and `s3` (Amazon S3, DigitalOcean Spaces, Cloudflare R2 and MinIO through the MinIO client, presigned URLs). The Full apps wire it (`storage.go`) and serve it to operators (`/ops/storage…`); the portal builds on those** | **Chosen**: the module stands on its own, as the roadmap asks, and the portal is its first UI |

### The drivers

| Option | Verdict |
|---|---|
| One driver per service | Rejected: S3, Spaces, R2 and MinIO speak the same API; four drivers would be four copies |
| **`local` and `s3`, with `STORAGE_DRIVER` naming the service (`s3`, `spaces`, `r2`, `minio`) for defaults (the endpoint from the region for S3 and Spaces, path-style addressing for MinIO) and for operators to see** | **Chosen** |
| The AWS SDK | Rejected in favour of `github.com/minio/minio-go/v7`: a fraction of the size, one client for every service |

### Development

| Option | Verdict |
|---|---|
| MinIO in `compose.yaml` for everyone | Rejected as the default: a container for something a directory does; `orb add storage --driver minio` adds it for those who want S3 semantics locally |
| **`local` by default: `.orb/storage` (gitignored), objects under `objects/<key>` and a sidecar under `meta/<key>.json` (content type, ETag, metadata); signed URLs are `<public URL>/storage/<key>?exp=&method=&sig=`, HMAC-signed with `STORAGE_SIGNING_KEY` (random per start when empty) and served by the app at `/storage/`. Production refuses `local`** | **Chosen** |

### Directories

Object stores have no directories. Listings fold keys at the next slash into `prefixes`; an empty directory exists through a hidden marker object, `<prefix>/.keep`, which listings hide. Both drivers do the same.

## Decision

| Piece | Decision |
|---|---|
| Module | `storage.Store`, `Object`, `Page`, `ListOptions`, `PutOptions`, `Info`, `Move`, `ValidKey` (1 to 1024 bytes, no `.`/`..`/empty segment, no leading slash, no control characters), `ContentTypeFor`, `ClampExpiry` (1 s to 7 days, default an hour); `local.New(root, WithSigner(secret, baseURL))` with `Handler`; `s3.New(Config)` |
| App | `STORAGE_DRIVER` (`local` in development, required otherwise), `STORAGE_LOCAL_DIR`, `STORAGE_ENDPOINT`, `STORAGE_REGION`, `STORAGE_BUCKET`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`, `STORAGE_PUBLIC_URL`, `STORAGE_PATH_STYLE`, `STORAGE_SIGNING_KEY`; `App.storage`; local signed links at `/storage/` |
| Operators (`Ops: storage`) | `GET /ops/storage` (driver, bucket, endpoint, `local`, status), `GET /ops/storage/objects?prefix=&recursive=&cursor=&limit=`, `GET /ops/storage/object?key=`, `GET /ops/storage/object/content?key=` (download), `PUT /ops/storage/object?key=` (the body is the object), `DELETE /ops/storage/object?key=`, `POST /ops/storage/object/move`, `POST /ops/storage/directories`, `POST /ops/storage/signed-url` (`key`, `method` GET or PUT, `expiry_seconds`); permissions `ops.storage.read` (`ops_viewer`, `platform_admin`) and `ops.storage.write` (`platform_admin`); audit `storage.object.uploaded`, `storage.object.deleted`, `storage.object.moved`, `storage.directory.created`, `storage.signed_url.created`; errors `storage_off` (404), `storage_object_not_found` (404), `invalid_storage_key` (422), `storage_unavailable` (503) |
| CLI | `orb add storage --driver local\|s3\|spaces\|r2\|minio [--endpoint] [--region] [--bucket] [--access-key] [--public-url]` rewrites the `storage` block of `.env.example` and sets the values in `.env` (the secret key by prompt or by hand); `minio` also adds the MinIO service to `compose.yaml`, which `orb dev` then starts |
| The portal | The Storage screen on `/ops/storage…`: the driver card, a file browser with upload, download, delete, move, new folder, image and PDF preview, metadata and signed URLs; a red banner and read-only mode when `local` is false, until unlocked for the session |

## Why

- One interface, two drivers: every S3-compatible service through one tested path, and a directory for development that needs nothing running.
- The operators' API is the portal's contract, so the same browser works against a production bucket, with the guard.
- Keys are validated in one place, so no driver can be asked for `../`.

## Trade-offs

- Uploads through `/ops/storage/object` are read into memory (the app's body limit applies); large files use a signed PUT URL instead.
- Local signed URLs stop working when the app restarts without `STORAGE_SIGNING_KEY`; set it for links that must survive.
- Listing on disk walks the directory: fine for development sizes.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)): new row for storage: keys validated, local links signed and served only for GET and PUT with their own signature, the operators' API behind `ops.storage.*`, `local` refused in production.
- Upgrade notes: Full apps gain `storage.go`, the ops endpoints, the permissions in `platform_admin` and `ops_viewer`, the env block, and the module in `go.mod`; nothing to configure in development.
- Guides: [storage](../guides/storage.md) (new), [environment variables](../guides/environment-variables.md), [ops API](../guides/ops-api.md), [CLI](../guides/cli.md), [Dev Portal](../guides/dev-portal.md), [services and libraries](../guides/services-and-libraries.md).

## Implementation notes (2026-09-17)

`modules/storage` tests (keys and helpers; the local driver end to end including signed URLs through the handler; the S3 driver against `GORBITAL_TEST_S3_*`, skipped otherwise), `TestOpsStorage` in both Full apps, `TestAddStorage` in the CLI.
