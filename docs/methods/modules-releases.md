# modules/releases

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/releases"
```

Package releases records which build every instance of an app runs and answers which releases are running (ADR-0040).

A [Tracker](#Tracker) is an app.Runner: it records the instance's build when it starts, sends a heartbeat while it runs, and marks the instance stopped when its context ends. A [Store](#Store) queries the records for operator APIs:

```go
tracker, err := releases.NewTracker(pool, buildinfo.Read())
store, err := releases.NewStore(pool)
current, err := store.Current(ctx)
```

An instance is running while it hasn't stopped and its last heartbeat is recent, so instances that crash or lose their network stop counting after three missed heartbeats. Tracking never stops an app from starting or serving: failed writes are logged and retried at the next heartbeat.

The release\_instances table comes from [Migrations](#Migrations); apply them first.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultHeartbeat`](#DefaultHeartbeat), [`DefaultRetention`](#DefaultRetention)
- Variables: [`ErrInvalidCursor`](#ErrInvalidCursor), [`Migrations`](#Migrations)
- Types:
  - [`CurrentRelease`](#CurrentRelease)
  - [`Instance`](#Instance)
  - [`InstanceFilter`](#InstanceFilter)
  - [`InstancePage`](#InstancePage)
  - [`Option`](#Option): [`WithClock`](#WithClock), [`WithHeartbeat`](#WithHeartbeat), [`WithHost`](#WithHost), [`WithLogger`](#WithLogger), [`WithRetention`](#WithRetention), [`WithRetentionFunc`](#WithRetentionFunc)
  - [`Release`](#Release)
  - [`ReleaseFilter`](#ReleaseFilter)
  - [`ReleasePage`](#ReleasePage)
  - [`Store`](#Store): [`NewStore`](#NewStore), [`Store.Current`](#Store.Current), [`Store.Instances`](#Store.Instances), [`Store.Releases`](#Store.Releases)
  - [`Tracker`](#Tracker): [`NewTracker`](#NewTracker), [`Tracker.InstanceID`](#Tracker.InstanceID), [`Tracker.Run`](#Tracker.Run)

## Constants

<a id="DefaultHeartbeat"></a>
<a id="DefaultRetention"></a>

```go
const (
	DefaultHeartbeat = 30 * time.Second
	DefaultRetention = 90 * 24 * time.Hour
)
```

Defaults.

*Since `v0.1.0`*

## Variables

<a id="ErrInvalidCursor"></a>

```go
var ErrInvalidCursor = errors.New("releases: invalid cursor")
```

ErrInvalidCursor reports a cursor that wasn't returned by a [Store](#Store) list.

*Since `v0.1.0`*

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the module's goose migrations: the release\_instances table. Apps copy them into db/migrations; tests can apply them directly with pgtest.

*Since `v0.1.0`*

## Types

<a id="CurrentRelease"></a>
<a id="CurrentRelease.Version"></a>
<a id="CurrentRelease.Commit"></a>
<a id="CurrentRelease.Instances"></a>

### type CurrentRelease

```go
type CurrentRelease struct {
	Version   string
	Commit    string
	Instances []Instance
}
```

CurrentRelease is a release with the instances running it now.

*Since `v0.1.0`*

<a id="Instance"></a>
<a id="Instance.ID"></a>
<a id="Instance.InstanceID"></a>
<a id="Instance.Version"></a>
<a id="Instance.Commit"></a>
<a id="Instance.BuildTime"></a>
<a id="Instance.Modified"></a>
<a id="Instance.GoVersion"></a>
<a id="Instance.Host"></a>
<a id="Instance.StartedAt"></a>
<a id="Instance.LastSeenAt"></a>
<a id="Instance.StoppedAt"></a>
<a id="Instance.Running"></a>

### type Instance

```go
type Instance struct {
	ID int64
	// InstanceID is random per process start.
	InstanceID string
	Version    string
	Commit     string
	// BuildTime is nil when the build didn't record one.
	BuildTime *time.Time
	// Modified reports a build from a tree with uncommitted changes.
	Modified   bool
	GoVersion  string
	Host       string
	StartedAt  time.Time
	LastSeenAt time.Time
	// StoppedAt is set when the instance shut down cleanly.
	StoppedAt *time.Time
	// Running reports that the instance hasn't stopped and sent a heartbeat
	// recently.
	Running bool
}
```

Instance is one start of an app instance.

*Since `v0.1.0`*

<a id="InstanceFilter"></a>
<a id="InstanceFilter.Version"></a>
<a id="InstanceFilter.Commit"></a>
<a id="InstanceFilter.RunningOnly"></a>
<a id="InstanceFilter.Limit"></a>
<a id="InstanceFilter.Cursor"></a>

### type InstanceFilter

```go
type InstanceFilter struct {
	Version string
	Commit  string
	// RunningOnly keeps the instances running now.
	RunningOnly bool
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is InstancePage.NextCursor from the previous page.
	Cursor string
}
```

InstanceFilter selects instance starts. Empty fields match everything.

*Since `v0.1.0`*

<a id="InstancePage"></a>
<a id="InstancePage.Instances"></a>
<a id="InstancePage.NextCursor"></a>

### type InstancePage

```go
type InstancePage struct {
	Instances []Instance
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}
```

InstancePage is a page of instance starts, newest first.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [NewTracker](#NewTracker) and [NewStore](#NewStore).

*Since `v0.1.0`*

<a id="WithClock"></a>

#### func WithClock

```go
func WithClock(now func() time.Time) Option
```

WithClock sets the clock, for tests.

*Since `v0.1.0`*

<a id="WithHeartbeat"></a>

#### func WithHeartbeat

```go
func WithHeartbeat(d time.Duration) Option
```

WithHeartbeat sets how often a tracker updates its instance, from 5 seconds to 5 minutes. Give the store the same value: it counts an instance as running for three heartbeats after it was last seen. Default: [DefaultHeartbeat](#DefaultHeartbeat).

*Since `v0.1.0`*

<a id="WithHost"></a>

#### func WithHost

```go
func WithHost(host string) Option
```

WithHost sets the host a tracker records, such as a pod name. Default: the operating system's host name.

*Since `v0.1.0`*

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

WithLogger sets the logger for failed writes. Default: discard.

*Since `v0.1.0`*

<a id="WithRetention"></a>

#### func WithRetention

```go
func WithRetention(d time.Duration) Option
```

WithRetention sets how long instances are kept after they were last seen, from 1 day to 3 years. A tracker deletes older instances when it starts. Default: [DefaultRetention](#DefaultRetention).

*Since `v0.1.0`*

<a id="WithRetentionFunc"></a>

#### func WithRetentionFunc

```go
func WithRetentionFunc(fn func(context.Context) time.Duration) Option
```

WithRetentionFunc reads the retention each time a tracker starts, so a runtime setting such as releases.instance\_retention applies without a redeploy (ADR-0051). Values outside 1 day to 3 years are clamped. It overrides [WithRetention](#WithRetention).

*Since `v0.1.0`*

<a id="Release"></a>
<a id="Release.Version"></a>
<a id="Release.Commit"></a>
<a id="Release.FirstStartedAt"></a>
<a id="Release.LastSeenAt"></a>
<a id="Release.Running"></a>
<a id="Release.Starts"></a>
<a id="Release.Modified"></a>

### type Release

```go
type Release struct {
	Version        string
	Commit         string
	FirstStartedAt time.Time
	LastSeenAt     time.Time
	// Running counts the instances running now.
	Running int
	// Starts counts every recorded start, including stopped instances.
	Starts int
	// Modified reports that an instance ran a build with uncommitted changes.
	Modified bool
}
```

Release is one build: every instance start with the same version and commit.

*Since `v0.1.0`*

<a id="ReleaseFilter"></a>
<a id="ReleaseFilter.Limit"></a>
<a id="ReleaseFilter.Cursor"></a>

### type ReleaseFilter

```go
type ReleaseFilter struct {
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is ReleasePage.NextCursor from the previous page.
	Cursor string
}
```

ReleaseFilter pages through releases.

*Since `v0.1.0`*

<a id="ReleasePage"></a>
<a id="ReleasePage.Releases"></a>
<a id="ReleasePage.NextCursor"></a>

### type ReleasePage

```go
type ReleasePage struct {
	Releases []Release
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}
```

ReleasePage is a page of releases, newest first.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store queries recorded instances and the releases they ran. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStore"></a>

#### func NewStore

```go
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error)
```

NewStore returns a store on pool. Give it the trackers' heartbeat, if not the default.

*Since `v0.1.0`*

<a id="Store.Current"></a>

#### func (*Store) Current

```go
func (s *Store) Current(ctx context.Context) ([]CurrentRelease, error)
```

Current returns the releases running now, newest start first, each with its running instances. During a rolling deploy it returns more than one; when no instance sent a recent heartbeat, none.

*Since `v0.1.0`*

<a id="Store.Instances"></a>

#### func (*Store) Instances

```go
func (s *Store) Instances(ctx context.Context, f InstanceFilter) (InstancePage, error)
```

Instances returns instance starts matching f, newest first. It returns [ErrInvalidCursor](#ErrInvalidCursor) for a cursor it didn't return.

*Since `v0.1.0`*

<a id="Store.Releases"></a>

#### func (*Store) Releases

```go
func (s *Store) Releases(ctx context.Context, f ReleaseFilter) (ReleasePage, error)
```

Releases returns releases, newest first by their first start. It returns [ErrInvalidCursor](#ErrInvalidCursor) for a cursor it didn't return.

*Since `v0.1.0`*

<a id="Tracker"></a>

### type Tracker

```go
type Tracker struct {
	// contains filtered or unexported fields
}
```

Tracker records one app instance: its build when it starts, a heartbeat while it runs and the time it stops. Run it once, as an app.Runner.

*Since `v0.1.0`*

<a id="NewTracker"></a>

#### func NewTracker

```go
func NewTracker(pool *pgxpool.Pool, info buildinfo.Info, opts ...Option) (*Tracker, error)
```

NewTracker returns a tracker for an instance running the build info describes, usually [buildinfo.Read](buildinfo.md#Read). An empty version is recorded as dev.

*Since `v0.1.0`*

<a id="Tracker.InstanceID"></a>

#### func (*Tracker) InstanceID

```go
func (t *Tracker) InstanceID() string
```

InstanceID returns the random ID this instance is recorded under, as listed by the release log's instances.

*Since `v0.1.0`*

<a id="Tracker.Run"></a>

#### func (*Tracker) Run

```go
func (t *Tracker) Run(ctx context.Context) error
```

Run records the instance, sends heartbeats until ctx ends, then marks the instance stopped. It always returns nil: failed writes are logged and retried at the next heartbeat, so tracking never stops the app.

*Since `v0.1.0`*
