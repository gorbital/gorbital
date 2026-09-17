# ratelimit

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/ratelimit"
```

Package ratelimit provides token-bucket rate limiting keyed by a string (an IP address, account or API key): the [Taker](#Taker) interface, an in-memory [Limiter](#Limiter), and HTTP middleware.

The in-memory limiter is per process: behind several instances each one enforces its own limit. gorbital.dev/modules/ratelimitpg implements [Taker](#Taker) with limits shared across instances (ADR-0052).

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultIdleTTL`](#DefaultIdleTTL), [`DefaultMaxKeys`](#DefaultMaxKeys)
- Variables: [`ErrEmptyKey`](#ErrEmptyKey)
- Functions: [`ByRemoteIP`](#ByRemoteIP), [`ClientKey`](#ClientKey), [`Middleware`](#Middleware)
- Types:
  - [`Decision`](#Decision)
  - [`KeyFunc`](#KeyFunc)
  - [`Limit`](#Limit): [`Per`](#Per), [`Limit.Valid`](#Limit.Valid)
  - [`Limiter`](#Limiter): [`New`](#New), [`Limiter.Allow`](#Limiter.Allow), [`Limiter.Take`](#Limiter.Take)
  - [`Option`](#Option): [`WithClock`](#WithClock), [`WithIdleTTL`](#WithIdleTTL), [`WithMaxKeys`](#WithMaxKeys)
  - [`Taker`](#Taker)

## Constants

<a id="DefaultIdleTTL"></a>
<a id="DefaultMaxKeys"></a>

```go
const (
	DefaultIdleTTL = 10 * time.Minute
	DefaultMaxKeys = 100_000
)
```

Defaults for [New](#New).

*Since `v0.1.0`*

## Variables

<a id="ErrEmptyKey"></a>

```go
var ErrEmptyKey = errors.New("ratelimit: empty key")
```

ErrEmptyKey reports a request without a key.

*Since `v0.1.0`*

## Functions

<a id="ByRemoteIP"></a>

### func ByRemoteIP

```go
func ByRemoteIP(r *http.Request) string
```

ByRemoteIP keys requests by the connection's remote IP, grouped as [ClientKey](#ClientKey) does. Behind a proxy, run trusted-proxy middleware first so RemoteAddr holds the client address.

*Since `v0.1.0`*

<a id="ClientKey"></a>

### func ClientKey

```go
func ClientKey(ip string) string
```

ClientKey returns the rate-limit key of a client IP address: an IPv4 address as it is, and an IPv6 address as its /64 network, since one host or customer usually holds a whole /64 and could otherwise use a new address, and a new budget, for every request. IPv4-mapped IPv6 addresses count as IPv4, and zones are ignored. A value that isn't an IP address is returned unchanged.

*Since `v0.1.0`*

<a id="Middleware"></a>

### func Middleware

```go
func Middleware(l Taker, key KeyFunc, onLimit http.Handler) func(http.Handler) http.Handler
```

Middleware limits requests with l. Limited requests receive onLimit, or a 429 application/problem+json response with a Retry-After header when onLimit is nil. When l can't decide, the request proceeds.

*Since `v0.1.0`*

## Types

<a id="Decision"></a>
<a id="Decision.Allowed"></a>
<a id="Decision.RetryAfter"></a>

### type Decision

```go
type Decision struct {
	Allowed bool
	// RetryAfter is how long until a request may proceed, when not allowed.
	RetryAfter time.Duration
}
```

Decision is the outcome of one request against a limit.

*Since `v0.1.0`*

<a id="KeyFunc"></a>

### type KeyFunc

```go
type KeyFunc func(r *http.Request) string
```

KeyFunc extracts the rate-limit key from a request. An empty key skips limiting for that request.

*Since `v0.1.0`*

<a id="Limit"></a>
<a id="Limit.PerSecond"></a>
<a id="Limit.Burst"></a>

### type Limit

```go
type Limit struct {
	PerSecond float64
	Burst     int
}
```

Limit is a token bucket: PerSecond requests on average, with bursts of up to Burst.

*Since `v0.1.0`*

<a id="Per"></a>

#### func Per

```go
func Per(n int, window time.Duration) Limit
```

Per returns the limit of n requests per window, all of which may arrive at once.

*Since `v0.1.0`*

<a id="Limit.Valid"></a>

#### func (Limit) Valid

```go
func (l Limit) Valid() bool
```

Valid reports whether l allows any request.

*Since `v0.1.0`*

<a id="Limiter"></a>

### type Limiter

```go
type Limiter struct {
	// contains filtered or unexported fields
}
```

A Limiter tracks one token bucket per key. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(perSecond float64, burst int, opts ...Option) *Limiter
```

New returns a limiter allowing perSecond requests per key on average, with bursts of up to burst requests.

*Since `v0.1.0`*

<a id="Limiter.Allow"></a>

#### func (*Limiter) Allow

```go
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration)
```

Allow reports whether a request for key may proceed. When it may not, retryAfter is how long until one token is available.

*Since `v0.1.0`*

<a id="Limiter.Take"></a>

#### func (*Limiter) Take

```go
func (l *Limiter) Take(_ context.Context, key string) (Decision, error)
```

Take implements [Taker](#Taker) with [Limiter.Allow](#Limiter.Allow); it never fails for a non-empty key.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures a [Limiter](#Limiter).

*Since `v0.1.0`*

<a id="WithClock"></a>

#### func WithClock

```go
func WithClock(now func() time.Time) Option
```

WithClock sets the time source, for tests.

*Since `v0.1.0`*

<a id="WithIdleTTL"></a>

#### func WithIdleTTL

```go
func WithIdleTTL(d time.Duration) Option
```

WithIdleTTL sets how long an unused key is remembered. Default: [DefaultIdleTTL](#DefaultIdleTTL).

*Since `v0.1.0`*

<a id="WithMaxKeys"></a>

#### func WithMaxKeys

```go
func WithMaxKeys(n int) Option
```

WithMaxKeys bounds memory use. When the limit is reached and no idle keys can be evicted, new keys are allowed without tracking (fail open), so a flood of distinct keys can't lock out all clients. Default: [DefaultMaxKeys](#DefaultMaxKeys).

*Since `v0.1.0`*

<a id="Taker"></a>
<a id="Taker.Take"></a>

### type Taker

```go
type Taker interface {
	Take(ctx context.Context, key string) (Decision, error)
}
```

A Taker decides whether a request for key may proceed. An error means the limiter couldn't decide, and callers allow the request: implementations that can fail handle their own fallback.

*Since `v0.1.0`*
