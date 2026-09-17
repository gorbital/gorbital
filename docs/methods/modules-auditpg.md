# modules/auditpg

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/auditpg"
```

Package auditpg stores audit events in PostgreSQL and queries them for operator APIs (ADR-0036). [Store](#Store) implements [audit.Recorder](audit.md#Recorder):

```go
store, err := auditpg.NewStore(pool)
settingsStore, err := settings.NewStore(ctx, pool, reg, store)
```

Events are append-only. Before storing, the store fills actor, request and trace fields from the context, validates the event, redacts metadata under sensitive keys such as "password" or "token", and bounds text lengths and metadata size, so one bad event never blocks the rest.

Use [Store.RecordTx](#Store.RecordTx) to commit an event with the change it describes.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultStatsWindow`](#DefaultStatsWindow), [`MaxStatsWindow`](#MaxStatsWindow), [`MaxStatsGroups`](#MaxStatsGroups), [`DefaultMaxMetadataBytes`](#DefaultMaxMetadataBytes), [`DefaultQueryTimeout`](#DefaultQueryTimeout)
- Variables: [`ErrEventNotFound`](#ErrEventNotFound), [`ErrInvalidCursor`](#ErrInvalidCursor), [`ErrInvalidFilter`](#ErrInvalidFilter), [`ErrQueryTimeout`](#ErrQueryTimeout), [`Migrations`](#Migrations)
- Types:
  - [`Filter`](#Filter)
  - [`Option`](#Option): [`WithMaxMetadataBytes`](#WithMaxMetadataBytes), [`WithQueryTimeout`](#WithQueryTimeout), [`WithRedactedKeys`](#WithRedactedKeys)
  - [`Page`](#Page)
  - [`Stats`](#Stats)
  - [`StatsCount`](#StatsCount)
  - [`StatsFilter`](#StatsFilter)
  - [`StatsGroup`](#StatsGroup): [`StatsByAction`](#StatsByAction), [`StatsByOutcome`](#StatsByOutcome), [`StatsByActorKind`](#StatsByActorKind), [`StatsByResourceType`](#StatsByResourceType), [`StatsByDay`](#StatsByDay)
  - [`Store`](#Store): [`NewStore`](#NewStore), [`Store.DeleteBefore`](#Store.DeleteBefore), [`Store.Get`](#Store.Get), [`Store.List`](#Store.List), [`Store.Oldest`](#Store.Oldest), [`Store.Record`](#Store.Record), [`Store.RecordTx`](#Store.RecordTx), [`Store.Stats`](#Store.Stats)
  - [`StoredEvent`](#StoredEvent)

## Constants

<a id="DefaultStatsWindow"></a>
<a id="MaxStatsWindow"></a>
<a id="MaxStatsGroups"></a>

```go
const (
	DefaultStatsWindow = 7 * 24 * time.Hour
	MaxStatsWindow     = 90 * 24 * time.Hour
	// MaxStatsGroups is the most groups returned, largest first; the rest are
	// counted in Stats.Other. Days are never cut: a window has at most 91.
	MaxStatsGroups = 50
)
```

Bounds of a stats window.

*Since `v0.1.0`*

<a id="DefaultMaxMetadataBytes"></a>

```go
const DefaultMaxMetadataBytes = 16 << 10
```

DefaultMaxMetadataBytes bounds an event's metadata after redaction.

*Since `v0.1.0`*

<a id="DefaultQueryTimeout"></a>

```go
const DefaultQueryTimeout = 5 * time.Second
```

DefaultQueryTimeout bounds each [Store.List](#Store.List) and [Store.Stats](#Store.Stats) query, so a filter no index serves can't hold a connection while it scans the table.

*Since `v0.1.0`*

## Variables

<a id="ErrEventNotFound"></a>
<a id="ErrInvalidCursor"></a>
<a id="ErrInvalidFilter"></a>
<a id="ErrQueryTimeout"></a>

```go
var (
	// ErrEventNotFound reports an event ID that doesn't exist, or was
	// removed by retention.
	ErrEventNotFound = errors.New("auditpg: audit event not found")

	// ErrInvalidCursor reports a cursor that wasn't returned by [Store.List].
	ErrInvalidCursor = errors.New("auditpg: invalid cursor")

	// ErrInvalidFilter reports a [Filter] with an unknown outcome, a
	// malformed action prefix or an empty time range. The wrapping error
	// says which.
	ErrInvalidFilter = errors.New("auditpg: invalid filter")

	// ErrQueryTimeout reports a list or stats query that ran longer than
	// the store's query timeout, usually because no index serves its
	// filters. Narrow the filters or the time range.
	ErrQueryTimeout = errors.New("auditpg: audit query timed out")
)
```

Errors returned by [Store](#Store) methods. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the module's goose migrations: the audit\_events table. Apps copy them into db/migrations; tests can apply them directly with pgtest.

*Since `v0.1.0`*

## Types

<a id="Filter"></a>
<a id="Filter.ActorKind"></a>
<a id="Filter.ActorID"></a>
<a id="Filter.Action"></a>
<a id="Filter.ActionPrefix"></a>
<a id="Filter.ResourceType"></a>
<a id="Filter.ResourceID"></a>
<a id="Filter.OrgID"></a>
<a id="Filter.Outcome"></a>
<a id="Filter.RequestID"></a>
<a id="Filter.From"></a>
<a id="Filter.To"></a>
<a id="Filter.Limit"></a>
<a id="Filter.Cursor"></a>

### type Filter

```go
type Filter struct {
	ActorKind actor.Kind
	ActorID   string
	// Action matches one action exactly; ActionPrefix matches every action
	// starting with it, such as "jobs." or "auth.session.".
	Action       string
	ActionPrefix string
	ResourceType string
	ResourceID   string
	OrgID        string
	Outcome      audit.Outcome
	RequestID    string
	// From and To bound OccurredAt: From inclusive, To exclusive.
	From time.Time
	To   time.Time
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is Page.NextCursor from the previous page.
	Cursor string
}
```

Filter selects events to list. Empty fields match everything.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [NewStore](#NewStore).

*Since `v0.1.0`*

<a id="WithMaxMetadataBytes"></a>

#### func WithMaxMetadataBytes

```go
func WithMaxMetadataBytes(n int) Option
```

WithMaxMetadataBytes sets the largest metadata stored, as JSON after redaction. Larger metadata is replaced with {"metadata\_dropped": "too\_large"}. Default: [DefaultMaxMetadataBytes](#DefaultMaxMetadataBytes).

*Since `v0.1.0`*

<a id="WithQueryTimeout"></a>

#### func WithQueryTimeout

```go
func WithQueryTimeout(d time.Duration) Option
```

WithQueryTimeout sets how long a [Store.List](#Store.List) or [Store.Stats](#Store.Stats) query may run before it is cancelled with [ErrQueryTimeout](#ErrQueryTimeout). Default: [DefaultQueryTimeout](#DefaultQueryTimeout).

*Since `v0.1.0`*

<a id="WithRedactedKeys"></a>

#### func WithRedactedKeys

```go
func WithRedactedKeys(names ...string) Option
```

WithRedactedKeys adds metadata keys whose values are replaced with "\[REDACTED]", on top of the defaults (password, passcode, secret, token, cookie, authorization, bearer, api\_key, private\_key, signing\_key, encryption\_key, credential, jwt, pin, otp, totp, magic\_link, and codes qualified as recovery, verification, reset, login, sign\_in, mfa, backup, security or access codes). A key matches when it equals a name or contains it as whole snake\_case segments, in any case style and singular or plural: "token" matches "refresh\_tokens" and "accessToken" but not "tokenizer". A bare "code" isn't redacted by default, since it usually names an error code; add it here if your metadata uses it for secrets.

*Since `v0.1.0`*

<a id="Page"></a>
<a id="Page.Events"></a>
<a id="Page.NextCursor"></a>

### type Page

```go
type Page struct {
	Events []StoredEvent
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}
```

Page is a page of events, newest first.

*Since `v0.1.0`*

<a id="Stats"></a>
<a id="Stats.From"></a>
<a id="Stats.To"></a>
<a id="Stats.GroupBy"></a>
<a id="Stats.Total"></a>
<a id="Stats.Groups"></a>
<a id="Stats.Other"></a>

### type Stats

```go
type Stats struct {
	From, To time.Time
	GroupBy  StatsGroup
	Total    int64
	// Groups are the largest groups, most events first, or every day in
	// order for StatsByDay.
	Groups []StatsCount
	// Other counts events in groups beyond MaxStatsGroups.
	Other int64
}
```

Stats are event counts in a window.

*Since `v0.1.0`*

<a id="StatsCount"></a>
<a id="StatsCount.Key"></a>
<a id="StatsCount.Count"></a>

### type StatsCount

```go
type StatsCount struct {
	Key   string
	Count int64
}
```

StatsCount is one group's count. Key is empty for events without a resource type.

*Since `v0.1.0`*

<a id="StatsFilter"></a>
<a id="StatsFilter.Filter"></a>
<a id="StatsFilter.GroupBy"></a>

### type StatsFilter

```go
type StatsFilter struct {
	Filter
	GroupBy StatsGroup
}
```

StatsFilter selects the events to count: Filter's fields except Limit and Cursor. From and To default to the DefaultStatsWindow ending now and may be at most MaxStatsWindow apart.

*Since `v0.1.0`*

<a id="StatsGroup"></a>

### type StatsGroup

```go
type StatsGroup string
```

StatsGroup is what [Store.Stats](#Store.Stats) counts events by.

*Since `v0.1.0`*

<a id="StatsByAction"></a>
<a id="StatsByOutcome"></a>
<a id="StatsByActorKind"></a>
<a id="StatsByResourceType"></a>
<a id="StatsByDay"></a>

```go
const (
	StatsByAction       StatsGroup = "action"
	StatsByOutcome      StatsGroup = "outcome"
	StatsByActorKind    StatsGroup = "actor_kind"
	StatsByResourceType StatsGroup = "resource_type"
	// StatsByDay counts events per UTC day, keyed YYYY-MM-DD.
	StatsByDay StatsGroup = "day"
)
```

Groupings for [Store.Stats](#Store.Stats).

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store records audit events in the audit\_events table and lists them. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStore"></a>

#### func NewStore

```go
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error)
```

NewStore returns a store on pool. The audit\_events table comes from [Migrations](#Migrations); apply them first.

*Since `v0.1.0`*

<a id="Store.DeleteBefore"></a>

#### func (*Store) DeleteBefore

```go
func (s *Store) DeleteBefore(ctx context.Context, before time.Time, limit int) (int64, error)
```

DeleteBefore deletes up to limit events that occurred before before, oldest first, and returns how many it deleted. Retention calls it until it deletes fewer than limit (ADR-0051).

*Since `v0.1.0`*

<a id="Store.Get"></a>

#### func (*Store) Get

```go
func (s *Store) Get(ctx context.Context, id int64) (StoredEvent, error)
```

Get returns one event, or [ErrEventNotFound](#ErrEventNotFound).

*Since `v0.1.0`*

<a id="Store.List"></a>

#### func (*Store) List

```go
func (s *Store) List(ctx context.Context, f Filter) (Page, error)
```

List returns events matching f, newest first. It returns an error wrapping [ErrInvalidFilter](#ErrInvalidFilter) or [ErrQueryTimeout](#ErrQueryTimeout), or [ErrInvalidCursor](#ErrInvalidCursor). Filters on actor ID, action, resource, organisation and request use an index; others, such as outcome or an action prefix alone, scan events newest first until the page fills or the query timeout expires.

*Since `v0.1.0`*

<a id="Store.Oldest"></a>

#### func (*Store) Oldest

```go
func (s *Store) Oldest(ctx context.Context) (oldest time.Time, ok bool, err error)
```

Oldest returns when the oldest stored event occurred; ok is false when there are none.

*Since `v0.1.0`*

<a id="Store.Record"></a>

#### func (*Store) Record

```go
func (s *Store) Record(ctx context.Context, e audit.Event) error
```

Record stores e. Empty actor, organisation, request and trace fields are filled from ctx, and a zero OccurredAt becomes now. It returns an error for an invalid event or when the event can't be stored.

*Since `v0.1.0`*

<a id="Store.RecordTx"></a>

#### func (*Store) RecordTx

```go
func (s *Store) RecordTx(ctx context.Context, db postgres.DBTX, e audit.Event) error
```

RecordTx stores e through db, usually a pgx.Tx, so the event commits or rolls back with the change it describes.

*Since `v0.1.0`*

<a id="Store.Stats"></a>

#### func (*Store) Stats

```go
func (s *Store) Stats(ctx context.Context, f StatsFilter) (Stats, error)
```

Stats counts events matching f by f.GroupBy. It returns an error wrapping [ErrInvalidFilter](#ErrInvalidFilter) for an unknown grouping or a window over MaxStatsWindow, or [ErrQueryTimeout](#ErrQueryTimeout).

*Since `v0.1.0`*

<a id="StoredEvent"></a>
<a id="StoredEvent.ID"></a>
<a id="StoredEvent.RecordedAt"></a>
<a id="StoredEvent.Event"></a>

### type StoredEvent

```go
type StoredEvent struct {
	// ID increases in recording order.
	ID int64
	// RecordedAt is when the database stored the event, by its clock;
	// OccurredAt is when the action happened, by the recording instance's.
	// Both are UTC.
	RecordedAt time.Time
	audit.Event
}
```

StoredEvent is a recorded audit event.

*Since `v0.1.0`*
