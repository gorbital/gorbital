# File storage

How a Full preset app keeps files ([ADR-0075](../adr/0075-file-storage.md)): a bucket of objects behind `gorbital.dev/modules/storage`, on the local disk in development and in an S3-compatible service in production, browsed from the Dev Portal's Storage screen and the operators' API.

## Drivers

| Driver | `STORAGE_DRIVER` | Where files go | When |
|---|---|---|---|
| Local disk | `local` (the development default) | `STORAGE_LOCAL_DIR` (`.orb/storage`, gitignored): `objects/<key>` and a sidecar `meta/<key>.json` | Development. Production refuses it |
| Amazon S3 | `s3` | The bucket in `STORAGE_REGION`; the endpoint is derived | Production |
| DigitalOcean Spaces | `spaces` | `<region>.digitaloceanspaces.com` | Production |
| Cloudflare R2 | `r2` | `STORAGE_ENDPOINT` (`<account>.r2.cloudflarestorage.com`) | Production |
| MinIO | `minio` | `STORAGE_ENDPOINT` (`127.0.0.1:9000` in Docker), path-style | S3 semantics on your machine |

Choose with `orb add storage`:

```bash
orb add storage                                   # asks; local by default
orb add storage --driver s3 --region eu-west-1 --bucket acme-files --access-key AKIA…
orb add storage --driver r2 --endpoint <account>.r2.cloudflarestorage.com --bucket acme-files --access-key …
orb add storage --driver minio                    # adds MinIO to compose.yaml; orb dev starts it
```

It rewrites the `storage` block of `.env.example`, sets the values in `.env` and, for MinIO, adds the service to `compose.yaml`. Put `STORAGE_SECRET_KEY` in `.env` yourself (MinIO's is `minioadmin`), and create the bucket once (MinIO's console is at http://127.0.0.1:9001).

## In code

```go
// app.storage is a storage.Store (internal/app/storage.go).
obj, err := a.storage.Put(ctx, "invoices/2026/inv_42.pdf", body, size, storage.PutOptions{ContentType: "application/pdf", Metadata: map[string]string{"invoice": "inv_42"}})
r, obj, err := a.storage.Get(ctx, key)   // io.ReadCloser and the object
page, err := a.storage.List(ctx, storage.ListOptions{Prefix: "invoices/2026/"})
url, err := a.storage.SignedURL(ctx, key, http.MethodGet, time.Hour)
```

Keys are paths: 1 to 1024 characters, segments without `.` or `..`, no leading slash (`storage.ValidKey`). Object stores have no directories: `List` folds keys at the next slash into `Prefixes`, and an empty directory exists through the hidden marker `<prefix>/.keep` (`storage.DirectoryMarker`). `storage.Move` copies and deletes, since stores have no rename.

Signed URLs let a browser download (GET) or upload (PUT) one object until they expire (a second to 7 days, an hour by default) without the app in the middle. With the local driver the app serves them itself at `/storage/<key>?exp=&method=&sig=`, signed with `STORAGE_SIGNING_KEY` (random at each start when empty, so set it for links that must survive a restart); the S3 drivers return the service's presigned URLs.

## Operators and the Dev Portal

`GET /ops/storage` describes the store and whether it answers; `GET /ops/storage/objects?prefix=&recursive=&cursor=&limit=` lists a page; `GET /ops/storage/object?key=` describes one, `GET /ops/storage/object/content?key=` downloads it, `PUT /ops/storage/object?key=` uploads the request body, `DELETE /ops/storage/object?key=` removes it, `POST /ops/storage/object/move` renames, `POST /ops/storage/directories` makes a folder, `POST /ops/storage/signed-url` makes a link ([ops API](ops-api.md)). Reads need `ops.storage.read` (`ops_viewer` has it), writes `ops.storage.write` (`platform_admin`); every write is audited (`storage.object.uploaded`, `storage.object.deleted`, `storage.object.moved`, `storage.directory.created`, `storage.signed_url.created`).

The Dev Portal's Storage screen is a file browser on those endpoints ([Dev Portal guide](dev-portal.md)): folders, upload, download, delete, move, new folder, image and PDF preview, metadata, signed URLs. When the store isn't `local` it shows a red banner and stays read-only until you unlock it for the session: the same browser works against a production bucket, on purpose.

## Environment reference

| Variable | Default | Notes |
|---|---|---|
| `STORAGE_DRIVER` | `local` | `local`, `s3`, `spaces`, `r2` or `minio`; production refuses `local` |
| `STORAGE_LOCAL_DIR` | `.orb/storage` | The local driver's directory |
| `STORAGE_ENDPOINT` | derived for `s3` and `spaces` | The service's host, no scheme |
| `STORAGE_REGION` | none | Required for `s3` and `spaces` without an endpoint |
| `STORAGE_BUCKET` | none | Required for the S3 drivers |
| `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY` | none | Required for the S3 drivers |
| `STORAGE_PUBLIC_URL` | none | Where objects are reachable when the bucket is public |
| `STORAGE_PATH_STYLE` | `true` for `minio` | Bucket in the path rather than the host |
| `STORAGE_SIGNING_KEY` | random per start | Signs local links |

## Troubleshooting

| Symptom | Fix |
|---|---|
| 404 `storage_off` | The app has no store: `STORAGE_DRIVER` is empty in production, or the config failed; see the app's start error |
| 503 `storage_unavailable` | The service didn't answer: wrong endpoint, bucket, keys or region; `GET /ops/storage` shows the reason |
| 422 `invalid_storage_key` | The key has `..`, an empty segment or a leading slash |
| A local signed link is 403 after a restart | Set `STORAGE_SIGNING_KEY` |
