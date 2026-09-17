# modules/storage/local

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/storage/local"
```

Package local stores objects on the local disk: development's storage driver (ADR-0075). Objects live under \<root>/objects/\<key>; each has a sidecar under \<root>/meta/\<key>.json with its content type, ETag and metadata. Signed URLs are HMAC-signed links served by [Store.Handler](#Store.Handler), which the app mounts (for example at /storage/).

Stability: experimental (ADR-0054).

## Contents

- Types:
  - [`Option`](#Option): [`WithSigner`](#WithSigner)
  - [`Store`](#Store): [`New`](#New), [`Store.Copy`](#Store.Copy), [`Store.Delete`](#Store.Delete), [`Store.Get`](#Store.Get), [`Store.Handler`](#Store.Handler), [`Store.Info`](#Store.Info), [`Store.List`](#Store.List), [`Store.Ping`](#Store.Ping), [`Store.Put`](#Store.Put), [`Store.Root`](#Store.Root), [`Store.SignedURL`](#Store.SignedURL), [`Store.Stat`](#Store.Stat)

## Types

<a id="Option"></a>

### type Option

```go
type Option func(*Store)
```

Option configures a Store.

*Since `v0.1.0`*

<a id="WithSigner"></a>

#### func WithSigner

```go
func WithSigner(secret []byte, baseURL string) Option
```

WithSigner enables signed URLs: secret signs them and baseURL (such as [http://127.0.0.1:8080/storage](http://127.0.0.1:8080/storage)) is where [Store.Handler](#Store.Handler) is mounted.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store is a local-disk [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(root string, opts ...Option) (*Store, error)
```

New opens the store under root, creating it.

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
func (s *Store) Delete(_ context.Context, key string) error
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

<a id="Store.Handler"></a>

#### func (*Store) Handler

```go
func (s *Store) Handler() http.Handler
```

Handler serves signed URLs: GET downloads the object, PUT stores the body. Mount it where [WithSigner](#WithSigner)'s baseURL points, with the prefix stripped (http.StripPrefix).

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

List implements [storage.Store](modules-storage.md#Store). The cursor is the last key of the previous page.

*Since `v0.1.0`*

<a id="Store.Ping"></a>

#### func (*Store) Ping

```go
func (s *Store) Ping(context.Context) error
```

Ping implements [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*

<a id="Store.Put"></a>

#### func (*Store) Put

```go
func (s *Store) Put(_ context.Context, key string, r io.Reader, _ int64, opts storage.PutOptions) (storage.Object, error)
```

Put implements [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*

<a id="Store.Root"></a>

#### func (*Store) Root

```go
func (s *Store) Root() string
```

Root is the store's directory.

*Since `v0.1.0`*

<a id="Store.SignedURL"></a>

#### func (*Store) SignedURL

```go
func (s *Store) SignedURL(_ context.Context, key, method string, expiry time.Duration) (string, error)
```

SignedURL implements [storage.Store](modules-storage.md#Store): baseURL/\<key>?exp=\<unix>&method=&sig=.

*Since `v0.1.0`*

<a id="Store.Stat"></a>

#### func (*Store) Stat

```go
func (s *Store) Stat(_ context.Context, key string) (storage.Object, error)
```

Stat implements [storage.Store](modules-storage.md#Store).

*Since `v0.1.0`*
