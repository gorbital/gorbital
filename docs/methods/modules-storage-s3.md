# modules/storage/s3

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/storage/s3"
```

Package s3 stores objects in an S3-compatible service: Amazon S3, DigitalOcean Spaces, Cloudflare R2 or MinIO (ADR-0075), through the MinIO client. Signed URLs are the service's presigned URLs.

Stability: experimental (ADR-0054).

## Contents

- Types:
  - [`Config`](#Config)
  - [`Store`](#Store): [`New`](#New), [`Store.Copy`](#Store.Copy), [`Store.Delete`](#Store.Delete), [`Store.Get`](#Store.Get), [`Store.Info`](#Store.Info), [`Store.List`](#Store.List), [`Store.Ping`](#Store.Ping), [`Store.Put`](#Store.Put), [`Store.SignedURL`](#Store.SignedURL), [`Store.Stat`](#Store.Stat)

## Types

<a id="Config"></a>
<a id="Config.Driver"></a>
<a id="Config.Endpoint"></a>
<a id="Config.Region"></a>
<a id="Config.Bucket"></a>
<a id="Config.AccessKey"></a>
<a id="Config.SecretKey"></a>
<a id="Config.UseSSL"></a>
<a id="Config.PathStyle"></a>
<a id="Config.PublicURL"></a>

### type Config

```go
type Config struct {
	// Driver names the service for operators: s3, spaces, r2 or minio.
	Driver string
	// Endpoint is the service's host, without a scheme: s3.amazonaws.com,
	// nyc3.digitaloceanspaces.com, <account>.r2.cloudflarestorage.com,
	// 127.0.0.1:9000 for MinIO.
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey config.Secret
	// UseSSL is on for every service but a local MinIO.
	UseSSL bool
	// PathStyle addresses the bucket in the path (MinIO) rather than the
	// host.
	PathStyle bool
	// PublicURL is where the bucket's objects are reachable when public.
	PublicURL string
}
```

Config connects to a service.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store is an S3-compatible [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(cfg Config) (*Store, error)
```

New connects to the service; it doesn't check the bucket (Ping does).

*Since `v0.1.0`*

<a id="Store.Copy"></a>

#### func (*Store) Copy

```go
func (s *Store) Copy(ctx context.Context, src, dst string) (storage.Object, error)
```

Copy implements [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*

<a id="Store.Delete"></a>

#### func (*Store) Delete

```go
func (s *Store) Delete(ctx context.Context, key string) error
```

Delete implements [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*

<a id="Store.Get"></a>

#### func (*Store) Get

```go
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, storage.Object, error)
```

Get implements [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*

<a id="Store.Info"></a>

#### func (*Store) Info

```go
func (s *Store) Info() storage.Info
```

Info implements [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*

<a id="Store.List"></a>

#### func (*Store) List

```go
func (s *Store) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error)
```

List implements [storage.Store](modules-storage.md#Store). The cursor is the last key of the previous page (S3's StartAfter).

*Since `v0.1.0`*

<a id="Store.Ping"></a>

#### func (*Store) Ping

```go
func (s *Store) Ping(ctx context.Context) error
```

Ping implements [storage.Store](modules-storage.md#Store): the bucket must exist.

*Since `v0.1.0`*

<a id="Store.Put"></a>

#### func (*Store) Put

```go
func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64, opts storage.PutOptions) (storage.Object, error)
```

Put implements [storage.Store](modules-storage.md#Store). size may be -1 when unknown.

*Since `v0.1.0`*

<a id="Store.SignedURL"></a>

#### func (*Store) SignedURL

```go
func (s *Store) SignedURL(ctx context.Context, key, method string, expiry time.Duration) (string, error)
```

SignedURL implements [storage.Store](modules-storage.md#Store) with a presigned URL.

*Since `v0.1.0`*

<a id="Store.Stat"></a>

#### func (*Store) Stat

```go
func (s *Store) Stat(ctx context.Context, key string) (storage.Object, error)
```

Stat implements [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*
