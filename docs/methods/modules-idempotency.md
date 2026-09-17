# modules/idempotency

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/idempotency"
```

Package idempotency makes retried POST and PATCH requests safe (ADR-0060). A client sends an Idempotency-Key header; the first request with the key runs, and its response is stored in PostgreSQL for the retention period so a retry gets the same response instead of running the request again:

```go
store, err := idempotency.NewStore(pool, idempotency.WithRetention(retention.Get))
handler = idempotency.Middleware(store, idempotency.WithSkip(isSignIn))(handler)
```

Keys belong to the caller that sent them: the signed-in user or service account in the context (gorbital.dev/actor). Requests without one ignore the header, so two callers never share a key or see each other's responses. A key sent again with another method, path, query or body is refused with 422 idempotency\_key\_reused, and a key whose first request is still running with 409 idempotency\_in\_progress.

Only final outcomes are stored. Server errors (5xx), panics, and 401, 403, 408 and 429 responses release the key so the client can retry, and so do responses that set cookies or say Cache-Control: no-store, bodies larger than the cap, and requests whose handler calls [DontStore](#DontStore). A request that dies without releasing its key (a crashed instance) holds it for the lock TTL.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`Header`](#Header), [`ReplayedHeader`](#ReplayedHeader), [`MaxKeyLength`](#MaxKeyLength), [`DefaultRetention`](#DefaultRetention), [`DefaultLockTTL`](#DefaultLockTTL), [`DefaultMaxResponseBytes`](#DefaultMaxResponseBytes)
- Variables: [`ErrInProgress`](#ErrInProgress), [`ErrKeyReused`](#ErrKeyReused), [`ErrLockLost`](#ErrLockLost), [`Migrations`](#Migrations)
- Functions: [`ActorScope`](#ActorScope), [`DontStore`](#DontStore), [`Fingerprint`](#Fingerprint), [`Middleware`](#Middleware)
- Types:
  - [`Lock`](#Lock): [`Lock.Complete`](#Lock.Complete), [`Lock.Release`](#Lock.Release)
  - [`MiddlewareOption`](#MiddlewareOption): [`WithMaxResponseBytes`](#WithMaxResponseBytes), [`WithScope`](#WithScope), [`WithSkip`](#WithSkip)
  - [`Option`](#Option): [`WithClock`](#WithClock), [`WithLockTTL`](#WithLockTTL), [`WithLogger`](#WithLogger), [`WithRetention`](#WithRetention)
  - [`Response`](#Response)
  - [`Store`](#Store): [`NewStore`](#NewStore), [`Store.Claim`](#Store.Claim), [`Store.DeleteExpired`](#Store.DeleteExpired), [`Store.Oldest`](#Store.Oldest)

## Constants

<a id="Header"></a>
<a id="ReplayedHeader"></a>
<a id="MaxKeyLength"></a>
<a id="DefaultRetention"></a>
<a id="DefaultLockTTL"></a>

```go
const (
	// Header is the request header carrying the client's key.
	Header = "Idempotency-Key"
	// ReplayedHeader is set to "true" on responses replayed from the store.
	ReplayedHeader = "Idempotent-Replayed"
	// MaxKeyLength is the longest key accepted, in bytes.
	MaxKeyLength = 255

	// DefaultRetention is how long responses are kept without
	// [WithRetention].
	DefaultRetention = 24 * time.Hour
	// DefaultLockTTL is how long a request holds its key without
	// [WithLockTTL]. It is well above the HTTP server's write timeout, so a
	// slow request isn't run twice, and short enough that a key held by a
	// crashed instance frees up within minutes.
	DefaultLockTTL = 5 * time.Minute
)
```

*Since `v0.1.0`*

<a id="DefaultMaxResponseBytes"></a>

```go
const DefaultMaxResponseBytes = 1 << 20
```

DefaultMaxResponseBytes is the largest response body stored without [WithMaxResponseBytes](#WithMaxResponseBytes). Larger responses reach the client but release the key.

*Since `v0.1.0`*

## Variables

<a id="ErrInProgress"></a>
<a id="ErrKeyReused"></a>
<a id="ErrLockLost"></a>

```go
var (
	// ErrInProgress reports a key whose first request hasn't finished. The
	// client retries later.
	ErrInProgress = errors.New("idempotency: a request with this key is in progress")

	// ErrKeyReused reports a key sent again with a different method, path,
	// query or body.
	ErrKeyReused = errors.New("idempotency: the key was used for a different request")

	// ErrLockLost reports a [Lock] that no longer holds its key: it was held
	// longer than the lock TTL and another request took the key over, or
	// the key expired and was deleted.
	ErrLockLost = errors.New("idempotency: the lock on the key was lost")
)
```

Errors returned by [Store.Claim](#Store.Claim) and [Lock](#Lock) methods. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the module's goose migrations: the idempotency\_keys table. Apps copy them into db/migrations; tests can apply them directly with pgtest.

*Since `v0.1.0`*

## Functions

<a id="ActorScope"></a>

### func ActorScope

```go
func ActorScope(r *http.Request) (string, bool)
```

ActorScope owns keys by the actor in the request's context: a user or a service account, by kind and ID. Anonymous and system actors have no scope, so their requests ignore the header.

*Since `v0.1.0`*

<a id="DontStore"></a>

### func DontStore

```go
func DontStore(ctx context.Context)
```

DontStore stops the response of the current request from being stored, for responses that must be shown only once, such as a new API key. The key is released, so a retry runs the request again. It does nothing outside a request with an idempotency key.

*Since `v0.1.0`*

<a id="Fingerprint"></a>

### func Fingerprint

```go
func Fingerprint(method, target string, body []byte) []byte
```

Fingerprint identifies a request for comparing retries: its method, target (path and query, as in http.Request.RequestURI) and body.

*Since `v0.1.0`*

<a id="Middleware"></a>

### func Middleware

```go
func Middleware(store *Store, opts ...MiddlewareOption) func(http.Handler) http.Handler
```

Middleware applies idempotency keys to POST and PATCH requests carrying the [Header](#Header) (other methods are idempotent already, or read-only). Put it after authentication, so the caller is known, and after any request body limit: the body is read into memory to fingerprint it.

  - A key must be 1 to [MaxKeyLength](#MaxKeyLength) visible ASCII characters (400 invalid\_idempotency\_key), sent once.
  - The first request runs; its response is stored unless it must be released (see the package documentation).
  - A retry with the same method, target and body gets the stored status, Content-Type, Location, ETag and body, with [ReplayedHeader](#ReplayedHeader) "true".
  - A retry with another method, target or body: 422 idempotency\_key\_reused. While the first request runs: 409 idempotency\_in\_progress with Retry-After.
  - When the store can't be reached: 503 unavailable, without running the request.

*Since `v0.1.0`*

## Types

<a id="Lock"></a>

### type Lock

```go
type Lock struct {
	// contains filtered or unexported fields
}
```

Lock is a claimed key. Exactly one of [Lock.Complete](#Lock.Complete) and [Lock.Release](#Lock.Release) should be called.

*Since `v0.1.0`*

<a id="Lock.Complete"></a>

#### func (*Lock) Complete

```go
func (l *Lock) Complete(ctx context.Context, resp Response) error
```

Complete stores resp for the lock's key and gives the key up. It returns [ErrLockLost](#ErrLockLost) when the lock no longer holds the key.

*Since `v0.1.0`*

<a id="Lock.Release"></a>

#### func (*Lock) Release

```go
func (l *Lock) Release(ctx context.Context) error
```

Release deletes the lock's key without storing a response, so the next request with it runs. Releasing a lost lock does nothing.

*Since `v0.1.0`*

<a id="MiddlewareOption"></a>

### type MiddlewareOption

```go
type MiddlewareOption interface {
	// contains filtered or unexported methods
}
```

A MiddlewareOption configures [Middleware](#Middleware).

*Since `v0.1.0`*

<a id="WithMaxResponseBytes"></a>

#### func WithMaxResponseBytes

```go
func WithMaxResponseBytes(n int) MiddlewareOption
```

WithMaxResponseBytes sets the largest response body stored. Default: [DefaultMaxResponseBytes](#DefaultMaxResponseBytes).

*Since `v0.1.0`*

<a id="WithScope"></a>

#### func WithScope

```go
func WithScope(scope func(*http.Request) (string, bool)) MiddlewareOption
```

WithScope sets who owns a request's keys. scope returns false for callers whose header is ignored. Default: [ActorScope](#ActorScope).

*Since `v0.1.0`*

<a id="WithSkip"></a>

#### func WithSkip

```go
func WithSkip(skip func(*http.Request) bool) MiddlewareOption
```

WithSkip ignores the header on requests for which skip returns true, such as sign-in endpoints, whose responses carry session tokens.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option func(*Store)
```

An Option configures a [Store](#Store).

*Since `v0.1.0`*

<a id="WithClock"></a>

#### func WithClock

```go
func WithClock(now func() time.Time) Option
```

WithClock uses now instead of the database's time, for tests.

*Since `v0.1.0`*

<a id="WithLockTTL"></a>

#### func WithLockTTL

```go
func WithLockTTL(ttl time.Duration) Option
```

WithLockTTL sets how long a request holds its key before another request with the same key may take it over. Default: [DefaultLockTTL](#DefaultLockTTL).

*Since `v0.1.0`*

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

WithLogger sets the logger for keys that couldn't be stored or released. Default: discard.

*Since `v0.1.0`*

<a id="WithRetention"></a>

#### func WithRetention

```go
func WithRetention(retention func(context.Context) time.Duration) Option
```

WithRetention reads how long responses are kept, on every use, so a runtime setting applies at once, to existing keys too. Default: [DefaultRetention](#DefaultRetention).

*Since `v0.1.0`*

<a id="Response"></a>
<a id="Response.Status"></a>
<a id="Response.Header"></a>
<a id="Response.Body"></a>

### type Response

```go
type Response struct {
	Status int
	// Header holds only the headers worth replaying: Content-Type,
	// Location and ETag.
	Header http.Header
	Body   []byte
}
```

Response is a stored response.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store keeps idempotency keys and their responses in the idempotency\_keys table. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStore"></a>

#### func NewStore

```go
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error)
```

NewStore returns a store on pool, which must have the module's migrations applied. The middleware counts the OpenTelemetry metric idempotency.requests by outcome.

*Since `v0.1.0`*

<a id="Store.Claim"></a>

#### func (*Store) Claim

```go
func (s *Store) Claim(ctx context.Context, scope, key string, fingerprint []byte) (*Lock, *Response, error)
```

Claim takes key for the caller identified by scope. It returns a [Lock](#Lock) when the request should run, or the stored [Response](#Response) when a request with the same fingerprint already completed. It returns [ErrInProgress](#ErrInProgress) while another request holds the key, and [ErrKeyReused](#ErrKeyReused) when the key was used with another fingerprint. Keys held longer than the lock TTL, and keys older than the retention, can be claimed again.

*Since `v0.1.0`*

<a id="Store.DeleteExpired"></a>

#### func (*Store) DeleteExpired

```go
func (s *Store) DeleteExpired(ctx context.Context, limit int) (int64, error)
```

DeleteExpired deletes up to limit keys older than the retention, with their stored responses, and returns how many it deleted. Apps run it from a periodic job until it deletes fewer than limit.

*Since `v0.1.0`*

<a id="Store.Oldest"></a>

#### func (*Store) Oldest

```go
func (s *Store) Oldest(ctx context.Context) (oldest time.Time, ok bool, err error)
```

Oldest returns when the oldest stored key was claimed; ok is false when there are none.

*Since `v0.1.0`*
