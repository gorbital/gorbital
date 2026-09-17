# modules/ratelimitpg

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/ratelimitpg"
```

Package ratelimitpg shares rate limits across instances in PostgreSQL (ADR-0052). A [Limiter](#Limiter) implements [ratelimit.Taker](ratelimit.md#Taker) with GCRA, the generic cell rate algorithm: each key keeps one row with its theoretical arrival time, and one statement decides and records a request, so every instance sees the same budget with the same rate-and-burst behaviour as the in-memory limiter. Keys are stored hashed.

Two in-memory limiters protect it:

  - a local pre-check with twice the burst refuses a client far over its limit without touching the database, so a flood of requests can't become a flood of writes;
  - a fallback with the same limit decides when the database doesn't answer within [DecisionTimeout](#DecisionTimeout), and a warning is logged at most once a minute per limiter, so requests keep working with per-instance limits during an outage.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DecisionTimeout`](#DecisionTimeout)
- Variables: [`Migrations`](#Migrations)
- Types:
  - [`Limiter`](#Limiter): [`Limiter.Take`](#Limiter.Take)
  - [`Option`](#Option): [`WithClock`](#WithClock), [`WithLogger`](#WithLogger)
  - [`Store`](#Store): [`NewStore`](#NewStore), [`Store.DeleteExpired`](#Store.DeleteExpired), [`Store.Limiter`](#Store.Limiter), [`Store.Reset`](#Store.Reset)

## Constants

<a id="DecisionTimeout"></a>

```go
const (
	// DecisionTimeout bounds each database decision.
	DecisionTimeout = 250 * time.Millisecond
)
```

*Since `v0.1.0`*

## Variables

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the module's goose migrations: the ratelimit\_buckets table. Apps copy them into db/migrations; tests can apply them directly with pgtest.

*Since `v0.1.0`*

## Types

<a id="Limiter"></a>

### type Limiter

```go
type Limiter struct {
	// contains filtered or unexported fields
}
```

Limiter shares one limit across instances. Limiters with the same name on any instance share their keys' budgets. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="Limiter.Take"></a>

#### func (*Limiter) Take

```go
func (l *Limiter) Take(ctx context.Context, key string) (ratelimit.Decision, error)
```

Take implements [ratelimit.Taker](ratelimit.md#Taker). It fails only for an empty key or an invalid limit; database failures are decided in memory.

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

WithClock makes decisions at now instead of the database's time, for tests.

*Since `v0.1.0`*

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

WithLogger sets the logger for fallback warnings. Default: discard.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store holds the shared buckets of every limiter. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStore"></a>

#### func NewStore

```go
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error)
```

NewStore returns a store on pool, which must have the module's migrations applied. Decisions count the OpenTelemetry metrics ratelimit.decisions and ratelimit.fallbacks.

*Since `v0.1.0`*

<a id="Store.DeleteExpired"></a>

#### func (*Store) DeleteExpired

```go
func (s *Store) DeleteExpired(ctx context.Context, limit int) (int64, error)
```

DeleteExpired removes up to limit buckets whose keys are back to a full budget, so the table holds only recently limited keys. Apps run it from a periodic job.

*Since `v0.1.0`*

<a id="Store.Limiter"></a>

#### func (*Store) Limiter

```go
func (s *Store) Limiter(name string, limit func(context.Context) ratelimit.Limit) (*Limiter, error)
```

Limiter returns the limiter name, whose limit is read on every decision, so a limit backed by a runtime setting applies at once. The name is part of every key: use one name per kind of limit, such as auth\_login.

*Since `v0.1.0`*

<a id="Store.Reset"></a>

#### func (*Store) Reset

```go
func (s *Store) Reset(ctx context.Context, name, key string) (bool, error)
```

Reset forgets the budget of key under the limiter named name, for operators unblocking a client (ADR-0070). It reports whether a bucket existed. Local and fallback limiters in memory keep their own state, which empties within the window.

*Since `v0.1.0`*
