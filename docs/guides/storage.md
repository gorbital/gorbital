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

A module gets the store as `gorbital.Deps.Storage`, a `storage.Store`, alongside the pool and the logger:

```go
Routes: func(r *gorbital.Router, d gorbital.Deps) {
	svc := usecase.NewService(repository.NewStore(d.DB), d.Storage, d.Logger)
	delivery.Register(r, svc)
},
```

`Deps.Storage` is nil unless the app configures storage, so a module registers the same routes either way and uses the store only when handling a request. In a v0.1 app the store is `app.storage`, built in `internal/app/storage.go`, and passed to the module the same way.

The methods are the same wherever the store came from:

```go
obj, err := store.Put(ctx, "invoices/2026/inv_42.pdf", body, size, storage.PutOptions{ContentType: "application/pdf", Metadata: map[string]string{"invoice": "inv_42"}})
r, obj, err := store.Get(ctx, key)   // io.ReadCloser and the object
page, err := store.List(ctx, storage.ListOptions{Prefix: "invoices/2026/"})
url, err := store.SignedURL(ctx, key, http.MethodGet, time.Hour)
```

### Where the store comes from

In an app on [`gorbital.Main`](main-go.md), `main.go` supplies it:

| Option | When |
|---|---|
| Nothing | `STORAGE_DRIVER=local`, development's default: `gorbital.New` opens the local driver and serves its signed links itself |
| `gorbital.WithStorageFunc(open)` | The S3-compatible drivers, whose client gorbital doesn't import. `open` receives the loaded `gorbital.Config` and builds the store from `cfg.Storage`; returning a nil store and a nil error keeps the built-in choice, so one function serves every `STORAGE_DRIVER`. A Full app is created with that function in `cmd/api/storage.go` (`internal/app/storage.go` in a v0.1 app), and `orb add storage` fills in the `STORAGE_*` variables it reads |
| `gorbital.WithStorage(s)` | A store you already have, such as a fake in a test |

An error from `open` fails `New` as a configuration error, before anything connects.

Keys are paths: 1 to 1024 characters, segments without `.` or `..`, no leading slash (`storage.ValidKey`). Object stores have no directories: `List` folds keys at the next slash into `Prefixes`, and an empty directory exists through the marker `<prefix>/.keep` (`storage.DirectoryMarker`), hidden in one-level listings and shown in recursive ones so a folder can be emptied. `storage.Move` copies and deletes, since stores have no rename.

Signed URLs let a browser download (GET) or upload (PUT) one object until they expire (a second to 7 days, an hour by default) without the app in the middle. With the local driver the app serves them itself at `/storage/<key>?exp=&method=&sig=`, signed with `STORAGE_SIGNING_KEY` (random at each start when empty, so set it for links that must survive a restart); the S3 drivers return the service's presigned URLs.

## Operators and the Dev Portal

`GET /ops/storage` describes the store and whether it answers; `GET /ops/storage/objects?prefix=&recursive=&cursor=&limit=` lists a page; `GET /ops/storage/object?key=` describes one, `GET /ops/storage/object/content?key=` downloads it, `PUT /ops/storage/object?key=` uploads the request body, `DELETE /ops/storage/object?key=` removes it, `POST /ops/storage/object/move` renames, `POST /ops/storage/directories` makes a folder, `POST /ops/storage/signed-url` makes a link ([ops API](ops-api.md)). Reads need `ops.storage.read` (`ops_viewer` has it), writes `ops.storage.write` (`platform_admin`); every write is audited (`storage.object.uploaded`, `storage.object.deleted`, `storage.object.moved`, `storage.directory.created`, `storage.signed_url.created`).

With the `logs.archive.enabled` runtime setting on, the app stores each hour of its log records under `logs/<service>/<YYYY>/<MM>/<DD>/` as gzipped JSON Lines ([observability guide](observability.md#the-hourly-log-archive), [ADR-0079](../adr/0079-hourly-log-archive.md)); they are objects like any other, so a lifecycle rule on `logs/` is how they expire.

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
