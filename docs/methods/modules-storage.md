# modules/storage

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/storage"
```

Package storage stores files for an app (ADR-0075): a [Store](#Store) is a bucket of objects addressed by key, with drivers for the local disk (development) and S3-compatible services (Amazon S3, DigitalOcean Spaces, Cloudflare R2, MinIO). The Dev Portal's Storage screen and the operators' API (/ops/storage) work on any Store.

Stability: experimental (ADR-0054).

## Contents

- Constants: [`MaxKeyLength`](#MaxKeyLength), [`MaxListLimit`](#MaxListLimit), [`DefaultListLimit`](#DefaultListLimit), [`MaxSignedURLExpiry`](#MaxSignedURLExpiry)
- Variables: [`ErrNotFound`](#ErrNotFound), [`ErrInvalidKey`](#ErrInvalidKey), [`ErrUnavailable`](#ErrUnavailable)
- Functions: [`ClampExpiry`](#ClampExpiry), [`ContentTypeFor`](#ContentTypeFor), [`DirectoryMarker`](#DirectoryMarker), [`IsDirectoryMarker`](#IsDirectoryMarker), [`ValidKey`](#ValidKey), [`ValidPrefix`](#ValidPrefix), [`ValidSignedMethod`](#ValidSignedMethod)
- Types:
  - [`Info`](#Info)
  - [`ListOptions`](#ListOptions)
  - [`Object`](#Object): [`Move`](#Move)
  - [`Page`](#Page)
  - [`PutOptions`](#PutOptions)
  - [`Store`](#Store)

## Constants

<a id="MaxKeyLength"></a>
<a id="MaxListLimit"></a>
<a id="DefaultListLimit"></a>
<a id="MaxSignedURLExpiry"></a>

```go
const (
	// MaxKeyLength bounds a key, as S3 does.
	MaxKeyLength = 1024
	// MaxListLimit bounds a List page.
	MaxListLimit = 1000
	// DefaultListLimit is a List page without a limit.
	DefaultListLimit = 200
	// MaxSignedURLExpiry bounds a signed URL's life (7 days, as S3 does).
	MaxSignedURLExpiry = 7 * 24 * time.Hour
)
```

Limits.

*Since `v0.1.0`*

## Variables

<a id="ErrNotFound"></a>
<a id="ErrInvalidKey"></a>
<a id="ErrUnavailable"></a>

```go
var (
	// ErrNotFound reports a key with no object.
	ErrNotFound = errors.New("storage: object not found")
	// ErrInvalidKey reports a key [ValidKey] refuses.
	ErrInvalidKey = errors.New("storage: invalid key")
	// ErrUnavailable reports a driver that can't reach its service.
	ErrUnavailable = errors.New("storage: unavailable")
)
```

Errors a Store returns.

*Since `v0.1.0`*

## Functions

<a id="ClampExpiry"></a>

### func ClampExpiry

```go
func ClampExpiry(d time.Duration) time.Duration
```

ClampExpiry bounds a signed URL's expiry: at least a second, at most [MaxSignedURLExpiry](#MaxSignedURLExpiry); zero means an hour.

*Since `v0.1.0`*

<a id="ContentTypeFor"></a>

### func ContentTypeFor

```go
func ContentTypeFor(key string) string
```

ContentTypeFor guesses a content type from a key's extension, falling back to application/octet-stream.

*Since `v0.1.0`*

<a id="DirectoryMarker"></a>

### func DirectoryMarker

```go
func DirectoryMarker(prefix string) string
```

DirectoryMarker is the key of the object that makes an empty "directory" exist: prefix + "/.keep". Listings hide it.

*Since `v0.1.0`*

<a id="IsDirectoryMarker"></a>

### func IsDirectoryMarker

```go
func IsDirectoryMarker(key string) bool
```

IsDirectoryMarker reports a key [DirectoryMarker](#DirectoryMarker) made.

*Since `v0.1.0`*

<a id="ValidKey"></a>

### func ValidKey

```go
func ValidKey(key string) bool
```

ValidKey reports whether key can name an object: 1 to [MaxKeyLength](#MaxKeyLength) bytes of valid UTF-8, no leading slash, no empty or "." or ".." segment, no control characters.

*Since `v0.1.0`*

<a id="ValidPrefix"></a>

### func ValidPrefix

```go
func ValidPrefix(prefix string) bool
```

ValidPrefix reports whether prefix can select objects: empty, or a valid key optionally ending with a slash.

*Since `v0.1.0`*

<a id="ValidSignedMethod"></a>

### func ValidSignedMethod

```go
func ValidSignedMethod(method string) bool
```

ValidSignedMethod reports a method [Store.SignedURL](#Store.SignedURL) accepts.

*Since `v0.1.0`*

## Types

<a id="Info"></a>
<a id="Info.Driver"></a>
<a id="Info.Bucket"></a>
<a id="Info.Endpoint"></a>
<a id="Info.Region"></a>
<a id="Info.Local"></a>
<a id="Info.PublicURL"></a>

### type Info

```go
type Info struct {
	// Driver is local, s3, spaces, r2 or minio.
	Driver string `json:"driver"`
	Bucket string `json:"bucket"`
	// Endpoint is the service's address (the directory for local).
	Endpoint string `json:"endpoint,omitempty"`
	Region   string `json:"region,omitempty"`
	// Local reports a store on this machine, safe to change freely.
	Local bool `json:"local"`
	// PublicURL is where objects are reachable without a signature, when
	// the bucket is public; empty otherwise.
	PublicURL string `json:"public_url,omitempty"`
}
```

Info describes a store for operators and the portal.

*Since `v0.1.0`*

<a id="ListOptions"></a>
<a id="ListOptions.Prefix"></a>
<a id="ListOptions.Recursive"></a>
<a id="ListOptions.Cursor"></a>
<a id="ListOptions.Limit"></a>

### type ListOptions

```go
type ListOptions struct {
	// Prefix keeps keys starting with it.
	Prefix string
	// Recursive lists every key under the prefix; otherwise keys with a
	// slash after the prefix are folded into Prefixes (directories).
	Recursive bool
	// Cursor continues a previous page.
	Cursor string
	// Limit is the page size (default [DefaultListLimit], at most
	// [MaxListLimit]).
	Limit int
}
```

ListOptions select a page of objects.

*Since `v0.1.0`*

<a id="Object"></a>
<a id="Object.Key"></a>
<a id="Object.Size"></a>
<a id="Object.ContentType"></a>
<a id="Object.ETag"></a>
<a id="Object.LastModified"></a>
<a id="Object.Metadata"></a>

### type Object

```go
type Object struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	ETag         string            `json:"etag,omitempty"`
	LastModified time.Time         `json:"last_modified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}
```

Object describes a stored object.

*Since `v0.1.0`*

<a id="Move"></a>

#### func Move

```go
func Move(ctx context.Context, s Store, src, dst string) (Object, error)
```

Move copies src to dst and deletes src. It is a helper for every Store, since object stores have no rename.

*Since `v0.1.0`*

<a id="Page"></a>
<a id="Page.Objects"></a>
<a id="Page.Prefixes"></a>
<a id="Page.NextCursor"></a>

### type Page

```go
type Page struct {
	Objects []Object `json:"objects"`
	// Prefixes are the "directories" folded when not recursive.
	Prefixes []string `json:"prefixes"`
	// NextCursor continues the listing; empty at the end.
	NextCursor string `json:"next_cursor,omitempty"`
}
```

Page is one page of a listing.

*Since `v0.1.0`*

<a id="PutOptions"></a>
<a id="PutOptions.ContentType"></a>
<a id="PutOptions.Metadata"></a>

### type PutOptions

```go
type PutOptions struct {
	// ContentType of the object; guessed from the key when empty.
	ContentType string
	// Metadata is kept with the object (S3 user metadata; a sidecar file
	// on disk). Keys are lowercased; values must be plain text.
	Metadata map[string]string
}
```

PutOptions describe an upload.

*Since `v0.1.0`*

<a id="Store"></a>
<a id="Store.Info"></a>
<a id="Store.Ping"></a>
<a id="Store.Put"></a>
<a id="Store.Get"></a>
<a id="Store.Stat"></a>
<a id="Store.Delete"></a>
<a id="Store.Copy"></a>
<a id="Store.List"></a>
<a id="Store.SignedURL"></a>

### type Store

```go
type Store interface {
	// Info describes the store.
	Info() Info
	// Ping checks the service answers; [ErrUnavailable] wraps the reason.
	Ping(ctx context.Context) error
	// Put stores r under key, replacing any object there.
	Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (Object, error)
	// Get opens the object for reading; the caller closes it.
	Get(ctx context.Context, key string) (io.ReadCloser, Object, error)
	// Stat describes the object without reading it.
	Stat(ctx context.Context, key string) (Object, error)
	// Delete removes the object; a missing key isn't an error.
	Delete(ctx context.Context, key string) error
	// Copy duplicates src to dst.
	Copy(ctx context.Context, src, dst string) (Object, error)
	// List returns a page of objects under a prefix.
	List(ctx context.Context, opts ListOptions) (Page, error)
	// SignedURL returns a URL that lets its holder GET (download) or PUT
	// (upload) the key until expiry, without other credentials.
	SignedURL(ctx context.Context, key string, method string, expiry time.Duration) (string, error)
}
```

A Store is a bucket of objects. Implementations are safe for concurrent use.

*Since `v0.1.0`*
