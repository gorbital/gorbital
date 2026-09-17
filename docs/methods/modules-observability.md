# modules/observability

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/observability"
```

Package observability shows how an application is doing across all its instances and keeps a record of incidents (ADR-0064).

A [Collector](#Collector) on every instance counts HTTP requests per minute, method and route: requests, client and server errors, and a latency histogram. Run as a background runner, it writes its minutes to PostgreSQL every 15 seconds through [Store.WriteMinutes](#Store.WriteMinutes), so [Store.Summary](#Store.Summary) can report request rates, error rates and latency percentiles over any recent window for the whole deployment, per instance and per route:

```
collector, err := observability.NewCollector(observability.WithInstance(tracker.InstanceID()), observability.WithSink(store))
handler := httpx.Chain(observability.RecordRoute(mux), httpx.Recover(logger), collector.Middleware(), …)
```

Series are keyed by the route pattern registered in code, never the requested path, so clients can't grow them; each instance holds at most a bounded number of series for a bounded number of minutes.

Incidents are opened by operators ([Store.OpenIncident](#Store.OpenIncident)) or by [Store.DetectIncident](#Store.DetectIncident) when the error rate crosses a threshold, and move through investigating, identified, monitoring and resolved with timeline updates. At most one automatic incident is open at a time, whichever instance runs detection.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultFlushInterval`](#DefaultFlushInterval), [`DefaultMaxSeries`](#DefaultMaxSeries), [`DefaultMaxPendingMinutes`](#DefaultMaxPendingMinutes), [`OverflowRoute`](#OverflowRoute), [`OtherMethod`](#OtherMethod), [`MaxTitleLength`](#MaxTitleLength), [`MaxSummaryLength`](#MaxSummaryLength), [`MaxMessageLength`](#MaxMessageLength), [`MaxIncidentUpdates`](#MaxIncidentUpdates), [`MaxStartedAge`](#MaxStartedAge), [`DefaultQueryTimeout`](#DefaultQueryTimeout), [`MaxSummaryRange`](#MaxSummaryRange), [`BucketCount`](#BucketCount), [`MaxInstanceLength`](#MaxInstanceLength)
- Variables: [`ErrInvalidRange`](#ErrInvalidRange), [`ErrQueryTimeout`](#ErrQueryTimeout), [`ErrIncidentNotFound`](#ErrIncidentNotFound), [`ErrInvalidIncident`](#ErrInvalidIncident), [`ErrIncidentResolved`](#ErrIncidentResolved), [`ErrTooManyUpdates`](#ErrTooManyUpdates), [`ErrInvalidCursor`](#ErrInvalidCursor), [`ErrTooManyStreams`](#ErrTooManyStreams), [`ErrStreamExpired`](#ErrStreamExpired), [`ErrStreamsClosed`](#ErrStreamsClosed), [`Migrations`](#Migrations)
- Functions: [`BucketBounds`](#BucketBounds), [`RecordRoute`](#RecordRoute)
- Types:
  - [`Collector`](#Collector): [`NewCollector`](#NewCollector), [`Collector.Flush`](#Collector.Flush), [`Collector.Instance`](#Collector.Instance), [`Collector.Lost`](#Collector.Lost), [`Collector.Middleware`](#Collector.Middleware), [`Collector.Minutes`](#Collector.Minutes), [`Collector.Record`](#Collector.Record), [`Collector.Run`](#Collector.Run), [`Collector.Subscribe`](#Collector.Subscribe)
  - [`Detection`](#Detection)
  - [`DetectionAction`](#DetectionAction): [`DetectionNone`](#DetectionNone), [`DetectionOpened`](#DetectionOpened), [`DetectionRecovered`](#DetectionRecovered), [`DetectionBreaching`](#DetectionBreaching)
  - [`DetectionResult`](#DetectionResult): [`DetectionResult.ErrorRate`](#DetectionResult.ErrorRate)
  - [`Incident`](#Incident)
  - [`IncidentChange`](#IncidentChange)
  - [`IncidentFilter`](#IncidentFilter)
  - [`IncidentPage`](#IncidentPage)
  - [`IncidentUpdate`](#IncidentUpdate)
  - [`InstanceStats`](#InstanceStats)
  - [`Minute`](#Minute)
  - [`MinuteStats`](#MinuteStats)
  - [`NewIncident`](#NewIncident)
  - [`Option`](#Option): [`WithClock`](#WithClock), [`WithFlushInterval`](#WithFlushInterval), [`WithInstance`](#WithInstance), [`WithLogger`](#WithLogger), [`WithMaxPendingMinutes`](#WithMaxPendingMinutes), [`WithMaxSeries`](#WithMaxSeries), [`WithSink`](#WithSink)
  - [`Request`](#Request)
  - [`RouteStats`](#RouteStats)
  - [`Severity`](#Severity): [`SeveritySev1`](#SeveritySev1), [`SeveritySev2`](#SeveritySev2), [`SeveritySev3`](#SeveritySev3), [`SeveritySev4`](#SeveritySev4)
  - [`Sink`](#Sink)
  - [`Source`](#Source): [`SourceManual`](#SourceManual), [`SourceAutomatic`](#SourceAutomatic)
  - [`Stats`](#Stats): [`Stats.ErrorRate`](#Stats.ErrorRate), [`Stats.Mean`](#Stats.Mean), [`Stats.Quantile`](#Stats.Quantile)
  - [`Status`](#Status): [`StatusInvestigating`](#StatusInvestigating), [`StatusIdentified`](#StatusIdentified), [`StatusMonitoring`](#StatusMonitoring), [`StatusResolved`](#StatusResolved)
  - [`Store`](#Store): [`NewStore`](#NewStore), [`Store.DeleteBefore`](#Store.DeleteBefore), [`Store.DetectIncident`](#Store.DetectIncident), [`Store.Incident`](#Store.Incident), [`Store.IncidentUpdates`](#Store.IncidentUpdates), [`Store.Incidents`](#Store.Incidents), [`Store.Oldest`](#Store.Oldest), [`Store.OpenIncident`](#Store.OpenIncident), [`Store.ResolveIncident`](#Store.ResolveIncident), [`Store.Summary`](#Store.Summary), [`Store.UpdateIncident`](#Store.UpdateIncident), [`Store.WriteMinutes`](#Store.WriteMinutes)
  - [`StoreOption`](#StoreOption): [`WithQueryTimeout`](#WithQueryTimeout), [`WithStoreClock`](#WithStoreClock)
  - [`Streams`](#Streams): [`NewStreams`](#NewStreams), [`Streams.Close`](#Streams.Close), [`Streams.Count`](#Streams.Count), [`Streams.MaxDuration`](#Streams.MaxDuration), [`Streams.Open`](#Streams.Open)
  - [`Summary`](#Summary)
  - [`UpdateKind`](#UpdateKind): [`UpdateOpened`](#UpdateOpened), [`UpdateNote`](#UpdateNote), [`UpdateResolved`](#UpdateResolved), [`UpdateRecovered`](#UpdateRecovered), [`UpdateBreaching`](#UpdateBreaching)

## Constants

<a id="DefaultFlushInterval"></a>
<a id="DefaultMaxSeries"></a>
<a id="DefaultMaxPendingMinutes"></a>
<a id="OverflowRoute"></a>
<a id="OtherMethod"></a>

```go
const (
	// DefaultFlushInterval is how often a collector writes its windows
	// without [WithFlushInterval].
	DefaultFlushInterval = 15 * time.Second
	// DefaultMaxSeries is how many method and route pairs one minute keeps
	// without [WithMaxSeries].
	DefaultMaxSeries = 500
	// DefaultMaxPendingMinutes is how many unwritten minutes a collector
	// keeps while its sink fails, without [WithMaxPendingMinutes].
	DefaultMaxPendingMinutes = 10

	// OverflowRoute is the route of requests counted after a minute already
	// has its maximum number of series.
	OverflowRoute = "_overflow"
	// OtherMethod replaces request methods that aren't standard HTTP
	// methods, as OpenTelemetry does.
	OtherMethod = "_OTHER"
)
```

*Since `v0.1.0`*

<a id="MaxTitleLength"></a>
<a id="MaxSummaryLength"></a>
<a id="MaxMessageLength"></a>
<a id="MaxIncidentUpdates"></a>
<a id="MaxStartedAge"></a>

```go
const (
	MaxTitleLength   = 200
	MaxSummaryLength = 5000
	MaxMessageLength = 5000
	// MaxIncidentUpdates is how many updates one incident keeps.
	MaxIncidentUpdates = 500
	// MaxStartedAge is how long before being opened an incident may have
	// started.
	MaxStartedAge = 90 * 24 * time.Hour
)
```

Limits on incidents.

*Since `v0.1.0`*

<a id="DefaultQueryTimeout"></a>
<a id="MaxSummaryRange"></a>

```go
const (
	// DefaultQueryTimeout bounds summary queries without
	// [WithQueryTimeout].
	DefaultQueryTimeout = 5 * time.Second
	// MaxSummaryRange is the longest time range [Store.Summary] reports.
	MaxSummaryRange = 7 * 24 * time.Hour
)
```

*Since `v0.1.0`*

<a id="BucketCount"></a>

```go
const BucketCount = len(bucketBounds) + 1
```

BucketCount is the number of latency histogram buckets: one per bound in [BucketBounds](#BucketBounds), and one for slower requests.

*Since `v0.1.0`*

<a id="MaxInstanceLength"></a>

```go
const MaxInstanceLength = 64
```

MaxInstanceLength is the longest instance ID stored.

*Since `v0.1.0`*

## Variables

<a id="ErrInvalidRange"></a>
<a id="ErrQueryTimeout"></a>
<a id="ErrIncidentNotFound"></a>
<a id="ErrInvalidIncident"></a>
<a id="ErrIncidentResolved"></a>
<a id="ErrTooManyUpdates"></a>
<a id="ErrInvalidCursor"></a>

```go
var (
	// ErrInvalidRange reports a summary range that is empty, reversed or
	// longer than [MaxSummaryRange].
	ErrInvalidRange = errors.New("observability: invalid time range")
	// ErrQueryTimeout reports a summary that took longer than the query
	// timeout.
	ErrQueryTimeout = errors.New("observability: query timed out")
	// ErrIncidentNotFound reports an incident ID that doesn't exist.
	ErrIncidentNotFound = errors.New("observability: incident not found")
	// ErrInvalidIncident reports an incident or update with a missing or
	// out-of-bounds field; the wrapping error says which.
	ErrInvalidIncident = errors.New("observability: invalid incident")
	// ErrIncidentResolved reports a change to a resolved incident.
	ErrIncidentResolved = errors.New("observability: incident is resolved")
	// ErrTooManyUpdates reports an incident that already has
	// [MaxIncidentUpdates] updates.
	ErrTooManyUpdates = errors.New("observability: incident has too many updates")
	// ErrInvalidCursor reports a cursor [Store.Incidents] didn't return.
	ErrInvalidCursor = errors.New("observability: invalid cursor")
)
```

Errors returned by [Store](#Store) methods. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

<a id="ErrTooManyStreams"></a>
<a id="ErrStreamExpired"></a>
<a id="ErrStreamsClosed"></a>

```go
var (
	// ErrTooManyStreams reports a stream refused because the instance, or
	// the subject, already has the most streams allowed.
	ErrTooManyStreams = errors.New("observability: too many streams")
	// ErrStreamExpired is the cause of a stream context that reached its
	// maximum duration.
	ErrStreamExpired = errors.New("observability: stream reached its maximum duration")
	// ErrStreamsClosed is the cause of stream contexts ended by
	// [Streams.Close], and the error of Open afterwards.
	ErrStreamsClosed = errors.New("observability: streams closed")
)
```

Errors of [Streams](#Streams).

*Since `v0.1.0`*

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the module's goose migrations: the observability\_minutes, incidents and incident\_updates tables. Apps copy them into db/migrations; tests can apply them directly with pgtest.

*Since `v0.1.0`*

## Functions

<a id="BucketBounds"></a>

### func BucketBounds

```go
func BucketBounds() []time.Duration
```

BucketBounds returns the upper bounds of the latency histogram's buckets, fastest first. Bucket i counts requests that took at most bound i and longer than bound i-1; the last bucket counts requests slower than every bound.

*Since `v0.1.0`*

<a id="RecordRoute"></a>

### func RecordRoute

```go
func RecordRoute(router http.Handler) http.Handler
```

RecordRoute wraps the application's router, normally the http.ServeMux at the end of the middleware chain, so [Collector.Middleware](#Collector.Middleware) learns the route pattern the router matched even when middleware between them replaced the request. Without the middleware, it does nothing.

*Since `v0.1.0`*

## Types

<a id="Collector"></a>

### type Collector

```go
type Collector struct {
	// contains filtered or unexported fields
}
```

Collector counts an instance's HTTP requests in one-minute windows, per method and route, and writes them to its [Sink](#Sink) periodically when run. Its memory is bounded: at most [WithMaxSeries](#WithMaxSeries) series per minute, each a fixed-size histogram, for at most [WithMaxPendingMinutes](#WithMaxPendingMinutes) minutes. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewCollector"></a>

#### func NewCollector

```go
func NewCollector(opts ...Option) (*Collector, error)
```

NewCollector returns a collector.

*Since `v0.1.0`*

<a id="Collector.Flush"></a>

#### func (*Collector) Flush

```go
func (c *Collector) Flush(ctx context.Context) error
```

Flush writes every minute the collector holds to the sink, then forgets minutes that ended before the write started. The current minute is written again at the next flush, with its new totals.

*Since `v0.1.0`*

<a id="Collector.Instance"></a>

#### func (*Collector) Instance

```go
func (c *Collector) Instance() string
```

Instance returns the instance ID the collector writes minutes under.

*Since `v0.1.0`*

<a id="Collector.Lost"></a>

#### func (*Collector) Lost

```go
func (c *Collector) Lost() int64
```

Lost returns how many requests were dropped without being written because the sink failed for longer than the pending minutes last.

*Since `v0.1.0`*

<a id="Collector.Middleware"></a>

#### func (*Collector) Middleware

```go
func (c *Collector) Middleware() func(http.Handler) http.Handler
```

Middleware records every request in the collector: its method, the route pattern the router matched, its status and duration. Install it early in the chain, after panic recovery, so responses written by other middleware (rate limits, maintenance mode, authentication failures) are counted too; a handler that panics counts as a 500.

Paths, query strings, headers and other values clients choose never become part of a series: the route is the pattern registered in code, or empty when none matched. http.ServeMux sets the pattern on the request it routes, which is a copy when a middleware in between calls r.WithContext, so wrap the router with [RecordRoute](#RecordRoute):

```
handler := httpx.Chain(observability.RecordRoute(mux), httpx.Recover(logger), collector.Middleware(), …)
```

*Since `v0.1.0`*

<a id="Collector.Minutes"></a>

#### func (*Collector) Minutes

```go
func (c *Collector) Minutes() []Minute
```

Minutes returns what the collector holds now: the current minute and finished minutes not yet written for the last time, oldest first.

*Since `v0.1.0`*

<a id="Collector.Record"></a>

#### func (*Collector) Record

```go
func (c *Collector) Record(r Request)
```

Record counts a finished request. [Collector.Middleware](#Collector.Middleware) calls it; call it directly only for requests served another way.

*Since `v0.1.0`*

<a id="Collector.Run"></a>

#### func (*Collector) Run

```go
func (c *Collector) Run(ctx context.Context) error
```

Run writes minutes to the sink every flush interval until ctx ends, then writes once more. Failed writes are logged and retried at the next interval; Run always returns nil, so observability never stops the app.

*Since `v0.1.0`*

<a id="Collector.Subscribe"></a>

#### func (*Collector) Subscribe

```go
func (c *Collector) Subscribe(fn func(Request)) (unsubscribe func())
```

Subscribe calls fn with every request recorded from now on, until unsubscribe is called. fn runs on the request's goroutine before the response is complete, so it must return quickly and never block, such as by appending to a ring buffer. Requests carry their path, request ID and trace ID only for subscribers.

*Since `v0.1.0`*

<a id="Detection"></a>
<a id="Detection.Window"></a>
<a id="Detection.Threshold"></a>
<a id="Detection.MinRequests"></a>
<a id="Detection.Severity"></a>
<a id="Detection.Actor"></a>

### type Detection

```go
type Detection struct {
	// Window is how far back requests count, 1 minute to 1 hour.
	Window time.Duration
	// Threshold is the error rate above which an incident opens, from 0 to
	// 1: 0.05 for 5% of requests being server errors.
	Threshold float64
	// MinRequests is how many requests the window needs before its error
	// rate counts, at least 1: a few failures of a quiet app don't open
	// incidents, and no traffic never looks recovered.
	MinRequests int64
	// Severity of incidents detection opens. Default: sev2.
	Severity Severity
	// Actor is who detection acts as in incidents' timelines, such as
	// actor.System("incidents_detect").
	Actor actor.Actor
}
```

Detection configures [Store.DetectIncident](#Store.DetectIncident).

*Since `v0.1.0`*

<a id="DetectionAction"></a>

### type DetectionAction

```go
type DetectionAction string
```

DetectionAction is what a detection changed.

*Since `v0.1.0`*

<a id="DetectionNone"></a>
<a id="DetectionOpened"></a>
<a id="DetectionRecovered"></a>
<a id="DetectionBreaching"></a>

```go
const (
	// DetectionNone: nothing changed.
	DetectionNone DetectionAction = ""
	// DetectionOpened: an automatic incident was opened.
	DetectionOpened DetectionAction = "opened"
	// DetectionRecovered: the open automatic incident's error rate
	// recovered; an update says so. The incident stays open for operators
	// to resolve.
	DetectionRecovered DetectionAction = "recovered"
	// DetectionBreaching: the error rate is high again after recovering.
	DetectionBreaching DetectionAction = "breaching"
)
```

Detection actions.

*Since `v0.1.0`*

<a id="DetectionResult"></a>
<a id="DetectionResult.From"></a>
<a id="DetectionResult.To"></a>
<a id="DetectionResult.Requests"></a>
<a id="DetectionResult.ServerErrors"></a>
<a id="DetectionResult.Breaching"></a>
<a id="DetectionResult.Healthy"></a>
<a id="DetectionResult.Action"></a>
<a id="DetectionResult.Incident"></a>
<a id="DetectionResult.Update"></a>

### type DetectionResult

```go
type DetectionResult struct {
	// From and To are the minutes counted: From inclusive, To exclusive.
	From, To     time.Time
	Requests     int64
	ServerErrors int64
	// Breaching reports an error rate above the threshold with at least
	// MinRequests requests; Healthy one at or below it with at least
	// MinRequests requests. Without enough requests, both are false.
	Breaching bool
	Healthy   bool
	Action    DetectionAction
	// Incident is the open automatic incident after detection, if any, and
	// Update the timeline update detection added.
	Incident *Incident
	Update   *IncidentUpdate
}
```

DetectionResult is what [Store.DetectIncident](#Store.DetectIncident) saw and did.

*Since `v0.1.0`*

<a id="DetectionResult.ErrorRate"></a>

#### func (DetectionResult) ErrorRate

```go
func (r DetectionResult) ErrorRate() float64
```

ErrorRate returns the share of requests that were server errors.

*Since `v0.1.0`*

<a id="Incident"></a>
<a id="Incident.ID"></a>
<a id="Incident.Title"></a>
<a id="Incident.Summary"></a>
<a id="Incident.Severity"></a>
<a id="Incident.Status"></a>
<a id="Incident.Source"></a>
<a id="Incident.StartedAt"></a>
<a id="Incident.ResolvedAt"></a>
<a id="Incident.RecoveredAt"></a>
<a id="Incident.CreatedByKind"></a>
<a id="Incident.CreatedByID"></a>
<a id="Incident.CreatedAt"></a>
<a id="Incident.UpdatedAt"></a>

### type Incident

```go
type Incident struct {
	ID       int64
	Title    string
	Summary  string
	Severity Severity
	Status   Status
	Source   Source
	// StartedAt is when the incident began, which can be before it was
	// opened; ResolvedAt is set once it is resolved.
	StartedAt  time.Time
	ResolvedAt *time.Time
	// RecoveredAt is set on an open automatic incident while detection sees
	// the error rate recovered.
	RecoveredAt *time.Time
	// CreatedByKind and CreatedByID identify who opened it: a user, or the
	// system for automatic incidents.
	CreatedByKind actor.Kind
	CreatedByID   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
```

Incident is a period during which the application didn't work as it should.

*Since `v0.1.0`*

<a id="IncidentChange"></a>
<a id="IncidentChange.Message"></a>
<a id="IncidentChange.Status"></a>
<a id="IncidentChange.Severity"></a>

### type IncidentChange

```go
type IncidentChange struct {
	Message string
	// Status and Severity are left unchanged when empty. Status can't be
	// resolved: use [Store.ResolveIncident].
	Status   Status
	Severity Severity
}
```

IncidentChange is a timeline update, optionally changing the incident's status and severity.

*Since `v0.1.0`*

<a id="IncidentFilter"></a>
<a id="IncidentFilter.Open"></a>
<a id="IncidentFilter.Status"></a>
<a id="IncidentFilter.Severity"></a>
<a id="IncidentFilter.Source"></a>
<a id="IncidentFilter.StartedFrom"></a>
<a id="IncidentFilter.StartedTo"></a>
<a id="IncidentFilter.Limit"></a>
<a id="IncidentFilter.Cursor"></a>

### type IncidentFilter

```go
type IncidentFilter struct {
	// Open selects incidents that aren't resolved; Status one status.
	Open     bool
	Status   Status
	Severity Severity
	Source   Source
	// StartedFrom and StartedTo bound StartedAt: From inclusive, To
	// exclusive.
	StartedFrom time.Time
	StartedTo   time.Time
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is IncidentPage.NextCursor from the previous page.
	Cursor string
}
```

IncidentFilter selects incidents. Empty fields match everything.

*Since `v0.1.0`*

<a id="IncidentPage"></a>
<a id="IncidentPage.Incidents"></a>
<a id="IncidentPage.NextCursor"></a>

### type IncidentPage

```go
type IncidentPage struct {
	Incidents []Incident
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}
```

IncidentPage is a page of incidents, most recently opened first.

*Since `v0.1.0`*

<a id="IncidentUpdate"></a>
<a id="IncidentUpdate.ID"></a>
<a id="IncidentUpdate.IncidentID"></a>
<a id="IncidentUpdate.Kind"></a>
<a id="IncidentUpdate.Message"></a>
<a id="IncidentUpdate.Status"></a>
<a id="IncidentUpdate.Severity"></a>
<a id="IncidentUpdate.ActorKind"></a>
<a id="IncidentUpdate.ActorID"></a>
<a id="IncidentUpdate.CreatedAt"></a>

### type IncidentUpdate

```go
type IncidentUpdate struct {
	ID         int64
	IncidentID int64
	Kind       UpdateKind
	Message    string
	// Status and Severity are the incident's after the update.
	Status    Status
	Severity  Severity
	ActorKind actor.Kind
	ActorID   string
	CreatedAt time.Time
}
```

IncidentUpdate is one entry of an incident's timeline.

*Since `v0.1.0`*

<a id="InstanceStats"></a>
<a id="InstanceStats.Instance"></a>
<a id="InstanceStats.LastMinute"></a>
<a id="InstanceStats.LastWrite"></a>
<a id="InstanceStats.Stats"></a>

### type InstanceStats

```go
type InstanceStats struct {
	Instance string
	// LastMinute is the latest minute with requests, and LastWrite when the
	// instance last wrote its minutes: an instance that stopped writing
	// has stopped or can't reach the database.
	LastMinute time.Time
	LastWrite  time.Time
	Stats
}
```

InstanceStats are one instance's requests.

*Since `v0.1.0`*

<a id="Minute"></a>
<a id="Minute.Start"></a>
<a id="Minute.Instance"></a>
<a id="Minute.Method"></a>
<a id="Minute.Route"></a>
<a id="Minute.Stats"></a>

### type Minute

```go
type Minute struct {
	// Start is the minute's first instant, in UTC.
	Start    time.Time
	Instance string
	Method   string
	Route    string
	Stats
}
```

Minute is one instance's requests for one method and route during one minute. Written again while the minute is in progress, it carries the totals so far.

*Since `v0.1.0`*

<a id="MinuteStats"></a>
<a id="MinuteStats.Start"></a>
<a id="MinuteStats.Stats"></a>

### type MinuteStats

```go
type MinuteStats struct {
	Start time.Time
	Stats
}
```

MinuteStats are one minute's requests across instances.

*Since `v0.1.0`*

<a id="NewIncident"></a>
<a id="NewIncident.Title"></a>
<a id="NewIncident.Summary"></a>
<a id="NewIncident.Severity"></a>
<a id="NewIncident.Status"></a>
<a id="NewIncident.StartedAt"></a>
<a id="NewIncident.Message"></a>

### type NewIncident

```go
type NewIncident struct {
	Title   string
	Summary string
	// Severity is required.
	Severity Severity
	// Status defaults to investigating; it can't be resolved.
	Status Status
	// StartedAt defaults to now; it can be up to [MaxStartedAge] earlier.
	StartedAt time.Time
	// Message is the first timeline update; default "Incident opened.".
	Message string
}
```

NewIncident is an incident an operator opens.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option func(*Collector)
```

An Option configures a [Collector](#Collector).

*Since `v0.1.0`*

<a id="WithClock"></a>

#### func WithClock

```go
func WithClock(now func() time.Time) Option
```

WithClock uses now instead of time.Now to place requests in minutes, for tests.

*Since `v0.1.0`*

<a id="WithFlushInterval"></a>

#### func WithFlushInterval

```go
func WithFlushInterval(d time.Duration) Option
```

WithFlushInterval sets how often Run writes minutes: 1 second to 1 minute. Default: [DefaultFlushInterval](#DefaultFlushInterval).

*Since `v0.1.0`*

<a id="WithInstance"></a>

#### func WithInstance

```go
func WithInstance(id string) Option
```

WithInstance names the instance in the stored minutes, such as the release tracker's instance ID. Default: a random ID.

*Since `v0.1.0`*

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

WithLogger sets the logger for failed writes. Default: discard.

*Since `v0.1.0`*

<a id="WithMaxPendingMinutes"></a>

#### func WithMaxPendingMinutes

```go
func WithMaxPendingMinutes(n int) Option
```

WithMaxPendingMinutes sets how many finished minutes wait for a failing sink before the oldest is dropped. Default: [DefaultMaxPendingMinutes](#DefaultMaxPendingMinutes).

*Since `v0.1.0`*

<a id="WithMaxSeries"></a>

#### func WithMaxSeries

```go
func WithMaxSeries(n int) Option
```

WithMaxSeries sets how many method and route pairs one minute keeps; later pairs are counted under [OverflowRoute](#OverflowRoute). Default: [DefaultMaxSeries](#DefaultMaxSeries).

*Since `v0.1.0`*

<a id="WithSink"></a>

#### func WithSink

```go
func WithSink(sink Sink) Option
```

WithSink sets where [Collector.Run](#Collector.Run) writes minutes. Without a sink, Run only discards finished minutes.

*Since `v0.1.0`*

<a id="Request"></a>
<a id="Request.Time"></a>
<a id="Request.Method"></a>
<a id="Request.Route"></a>
<a id="Request.Status"></a>
<a id="Request.Duration"></a>
<a id="Request.Path"></a>
<a id="Request.RequestID"></a>
<a id="Request.TraceID"></a>

### type Request

```go
type Request struct {
	// Time is when the request finished.
	Time time.Time
	// Method is the request method: a standard method, or [OtherMethod].
	Method string
	// Route is the path of the pattern the router matched, such as
	// "/v1/projects/{id}"; empty when no route matched, such as a request
	// answered by middleware.
	Route    string
	Status   int
	Duration time.Duration

	// Path (without the query), RequestID and TraceID are set only for
	// subscribers ([Collector.Subscribe]). The collector never stores or
	// aggregates them.
	Path      string
	RequestID string
	TraceID   string
}
```

Request is one finished HTTP request, as [Collector.Middleware](#Collector.Middleware) recorded it.

*Since `v0.1.0`*

<a id="RouteStats"></a>
<a id="RouteStats.Method"></a>
<a id="RouteStats.Route"></a>
<a id="RouteStats.Stats"></a>

### type RouteStats

```go
type RouteStats struct {
	Method string
	// Route is the route pattern's path; empty for requests no route
	// matched, and [OverflowRoute] for requests beyond the series limit.
	Route string
	Stats
}
```

RouteStats are one method and route's requests across instances.

*Since `v0.1.0`*

<a id="Severity"></a>

### type Severity

```go
type Severity string
```

Severity is how bad an incident is, from sev1 (most severe) to sev4.

*Since `v0.1.0`*

<a id="SeveritySev1"></a>
<a id="SeveritySev2"></a>
<a id="SeveritySev3"></a>
<a id="SeveritySev4"></a>

```go
const (
	SeveritySev1 Severity = "sev1"
	SeveritySev2 Severity = "sev2"
	SeveritySev3 Severity = "sev3"
	SeveritySev4 Severity = "sev4"
)
```

Severities.

*Since `v0.1.0`*

<a id="Sink"></a>
<a id="Sink.WriteMinutes"></a>

### type Sink

```go
type Sink interface {
	WriteMinutes(ctx context.Context, minutes []Minute) error
}
```

A Sink stores a collector's minutes. [Store](#Store) implements it.

*Since `v0.1.0`*

<a id="Source"></a>

### type Source

```go
type Source string
```

Source is who opened an incident.

*Since `v0.1.0`*

<a id="SourceManual"></a>
<a id="SourceAutomatic"></a>

```go
const (
	SourceManual    Source = "manual"
	SourceAutomatic Source = "automatic"
)
```

Sources.

*Since `v0.1.0`*

<a id="Stats"></a>
<a id="Stats.Requests"></a>
<a id="Stats.ClientErrors"></a>
<a id="Stats.ServerErrors"></a>
<a id="Stats.DurationSum"></a>
<a id="Stats.DurationMax"></a>
<a id="Stats.Buckets"></a>

### type Stats

```go
type Stats struct {
	Requests int64
	// ClientErrors are 4xx responses, ServerErrors 5xx responses; a
	// handler that panicked counts as a server error.
	ClientErrors int64
	ServerErrors int64
	// DurationSum is the total time spent, DurationMax the slowest request.
	DurationSum time.Duration
	DurationMax time.Duration
	// Buckets holds [BucketCount] request counts, by [BucketBounds]. Nil
	// when there are no requests.
	Buckets []int64
}
```

Stats are request counts and latencies over some set of requests: a route, an instance, a minute, or everything in a time range.

*Since `v0.1.0`*

<a id="Stats.ErrorRate"></a>

#### func (Stats) ErrorRate

```go
func (s Stats) ErrorRate() float64
```

ErrorRate returns the share of requests that were server errors, from 0 to 1; 0 without requests.

*Since `v0.1.0`*

<a id="Stats.Mean"></a>

#### func (Stats) Mean

```go
func (s Stats) Mean() time.Duration
```

Mean returns the average request duration; 0 without requests.

*Since `v0.1.0`*

<a id="Stats.Quantile"></a>

#### func (Stats) Quantile

```go
func (s Stats) Quantile(q float64) time.Duration
```

Quantile estimates the duration q (0 \< q ≤ 1) of requests took at most, such as 0.95 for the 95th percentile; 0 without requests.

The estimate finds the bucket holding the q-th request and interpolates linearly inside it, assuming requests are spread evenly across the bucket; the last bucket is interpolated up to [Stats.DurationMax](#Stats.DurationMax), and no estimate exceeds it. The exact value lies in the same bucket, so the error is at most the bucket's width: under 0.5 ms for requests faster than 1 ms, at most 50% of the exact value between 1 ms and 10 s, and at most 100% above 10 s. Real latency distributions change little inside a bucket, so errors are usually a few percent (ADR-0064 records measured errors).

*Since `v0.1.0`*

<a id="Status"></a>

### type Status

```go
type Status string
```

Status is where an incident is in its lifecycle.

*Since `v0.1.0`*

<a id="StatusInvestigating"></a>
<a id="StatusIdentified"></a>
<a id="StatusMonitoring"></a>
<a id="StatusResolved"></a>

```go
const (
	StatusInvestigating Status = "investigating"
	StatusIdentified    Status = "identified"
	StatusMonitoring    Status = "monitoring"
	StatusResolved      Status = "resolved"
)
```

Statuses. Open incidents move between the first three in any order; resolved is final.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store keeps request minutes and incidents in PostgreSQL. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStore"></a>

#### func NewStore

```go
func NewStore(pool *pgxpool.Pool, opts ...StoreOption) (*Store, error)
```

NewStore returns a store on pool, which must have the module's migrations applied. [Store.DetectIncident](#Store.DetectIncident) counts the OpenTelemetry metric incidents.detections by action.

*Since `v0.1.0`*

<a id="Store.DeleteBefore"></a>

#### func (*Store) DeleteBefore

```go
func (s *Store) DeleteBefore(ctx context.Context, before time.Time, limit int) (int64, error)
```

DeleteBefore deletes up to limit minutes that started before before, and returns how many it deleted. Apps run it from a periodic job until it deletes fewer than limit.

*Since `v0.1.0`*

<a id="Store.DetectIncident"></a>

#### func (*Store) DetectIncident

```go
func (s *Store) DetectIncident(ctx context.Context, d Detection) (DetectionResult, error)
```

DetectIncident compares every instance's server error rate over the last Window, up to and including the current minute, with the threshold:

  - above it, with no automatic incident open: it opens one;
  - above it again after recovering: it adds a "breaching" update;
  - at or below it while an automatic incident is open and not yet seen recovered: it adds a "recovered" update, leaving the incident open for operators to resolve.

Windows with fewer than MinRequests requests change nothing. Detection runs in one transaction holding an advisory lock, and a unique index allows one open automatic incident, so instances running it at the same time never open two. It counts incidents.detections by action.

*Since `v0.1.0`*

<a id="Store.Incident"></a>

#### func (*Store) Incident

```go
func (s *Store) Incident(ctx context.Context, id int64) (Incident, error)
```

Incident returns an incident, or [ErrIncidentNotFound](#ErrIncidentNotFound).

*Since `v0.1.0`*

<a id="Store.IncidentUpdates"></a>

#### func (*Store) IncidentUpdates

```go
func (s *Store) IncidentUpdates(ctx context.Context, id int64) ([]IncidentUpdate, error)
```

IncidentUpdates returns an incident's timeline, oldest first: at most [MaxIncidentUpdates](#MaxIncidentUpdates) updates. An unknown incident has none.

*Since `v0.1.0`*

<a id="Store.Incidents"></a>

#### func (*Store) Incidents

```go
func (s *Store) Incidents(ctx context.Context, f IncidentFilter) (IncidentPage, error)
```

Incidents returns incidents matching f, most recently opened first. It returns [ErrInvalidCursor](#ErrInvalidCursor) for a cursor it didn't return, and an error wrapping [ErrInvalidIncident](#ErrInvalidIncident) for an unknown status, severity or source.

*Since `v0.1.0`*

<a id="Store.Oldest"></a>

#### func (*Store) Oldest

```go
func (s *Store) Oldest(ctx context.Context) (oldest time.Time, ok bool, err error)
```

Oldest returns the start of the oldest stored minute; ok is false when there are none.

*Since `v0.1.0`*

<a id="Store.OpenIncident"></a>

#### func (*Store) OpenIncident

```go
func (s *Store) OpenIncident(ctx context.Context, n NewIncident) (Incident, IncidentUpdate, error)
```

OpenIncident opens an incident by the context's actor, with a first timeline update. It returns an error wrapping [ErrInvalidIncident](#ErrInvalidIncident) for a missing or out-of-bounds field.

*Since `v0.1.0`*

<a id="Store.ResolveIncident"></a>

#### func (*Store) ResolveIncident

```go
func (s *Store) ResolveIncident(ctx context.Context, id int64, message string) (Incident, IncidentUpdate, error)
```

ResolveIncident resolves an open incident now, by the context's actor, with a final timeline update. It returns [ErrIncidentNotFound](#ErrIncidentNotFound), [ErrIncidentResolved](#ErrIncidentResolved), [ErrTooManyUpdates](#ErrTooManyUpdates), or an error wrapping [ErrInvalidIncident](#ErrInvalidIncident) for a missing message.

*Since `v0.1.0`*

<a id="Store.Summary"></a>

#### func (*Store) Summary

```go
func (s *Store) Summary(ctx context.Context, from, to time.Time) (Summary, error)
```

Summary adds up the minutes starting in \[from, to), both truncated to the minute. It returns [ErrInvalidRange](#ErrInvalidRange) for an empty or reversed range or one longer than [MaxSummaryRange](#MaxSummaryRange), and [ErrQueryTimeout](#ErrQueryTimeout) when the query takes longer than the query timeout.

*Since `v0.1.0`*

<a id="Store.UpdateIncident"></a>

#### func (*Store) UpdateIncident

```go
func (s *Store) UpdateIncident(ctx context.Context, id int64, c IncidentChange) (Incident, IncidentUpdate, error)
```

UpdateIncident adds a timeline update to an open incident by the context's actor, changing its status and severity when the change sets them. It returns [ErrIncidentNotFound](#ErrIncidentNotFound), [ErrIncidentResolved](#ErrIncidentResolved), [ErrTooManyUpdates](#ErrTooManyUpdates), or an error wrapping [ErrInvalidIncident](#ErrInvalidIncident).

*Since `v0.1.0`*

<a id="Store.WriteMinutes"></a>

#### func (*Store) WriteMinutes

```go
func (s *Store) WriteMinutes(ctx context.Context, minutes []Minute) error
```

WriteMinutes implements [Sink](#Sink): it stores minutes, replacing earlier writes of the same instance, minute, method and route.

*Since `v0.1.0`*

<a id="StoreOption"></a>

### type StoreOption

```go
type StoreOption func(*Store)
```

A StoreOption configures a [Store](#Store).

*Since `v0.1.0`*

<a id="WithQueryTimeout"></a>

#### func WithQueryTimeout

```go
func WithQueryTimeout(d time.Duration) StoreOption
```

WithQueryTimeout bounds [Store.Summary](#Store.Summary). Default: [DefaultQueryTimeout](#DefaultQueryTimeout).

*Since `v0.1.0`*

<a id="WithStoreClock"></a>

#### func WithStoreClock

```go
func WithStoreClock(now func() time.Time) StoreOption
```

WithStoreClock uses now instead of time.Now, for tests.

*Since `v0.1.0`*

<a id="Streams"></a>

### type Streams

```go
type Streams struct {
	// contains filtered or unexported fields
}
```

Streams bounds long-lived responses such as Server-Sent Events on one instance: how many run at once, how many one subject (such as a user) may hold, and how long each lasts. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStreams"></a>

#### func NewStreams

```go
func NewStreams(maxTotal, maxPerSubject int, maxDuration time.Duration) (*Streams, error)
```

NewStreams returns limits of maxTotal streams, maxPerSubject per subject, each lasting at most maxDuration.

*Since `v0.1.0`*

<a id="Streams.Close"></a>

#### func (*Streams) Close

```go
func (s *Streams) Close()
```

Close ends every open stream and refuses new ones, for a shutting-down instance: load balancers and HTTP servers wait for long-lived responses.

*Since `v0.1.0`*

<a id="Streams.Count"></a>

#### func (*Streams) Count

```go
func (s *Streams) Count() int
```

Count returns how many streams are open.

*Since `v0.1.0`*

<a id="Streams.MaxDuration"></a>

#### func (*Streams) MaxDuration

```go
func (s *Streams) MaxDuration() time.Duration
```

MaxDuration returns how long a stream lasts at most.

*Since `v0.1.0`*

<a id="Streams.Open"></a>

#### func (*Streams) Open

```go
func (s *Streams) Open(ctx context.Context, subject string) (streamCtx context.Context, done func(), err error)
```

Open starts a stream for subject. The returned context ends with ctx, after the maximum duration (cause [ErrStreamExpired](#ErrStreamExpired)) or when the streams are closed (cause [ErrStreamsClosed](#ErrStreamsClosed)); call done when the stream ends. Open returns [ErrTooManyStreams](#ErrTooManyStreams) over a limit.

*Since `v0.1.0`*

<a id="Summary"></a>
<a id="Summary.From"></a>
<a id="Summary.To"></a>
<a id="Summary.Total"></a>
<a id="Summary.Minutes"></a>
<a id="Summary.Instances"></a>
<a id="Summary.Routes"></a>

### type Summary

```go
type Summary struct {
	// From and To are the range's minutes: From inclusive, To exclusive.
	From, To time.Time
	Total    Stats
	// Minutes has one entry per minute with requests, oldest first.
	Minutes []MinuteStats
	// Instances has one entry per instance with requests, by instance ID.
	Instances []InstanceStats
	// Routes has one entry per method and route with requests, by route
	// and method.
	Routes []RouteStats
}
```

Summary is every instance's requests in a time range.

*Since `v0.1.0`*

<a id="UpdateKind"></a>

### type UpdateKind

```go
type UpdateKind string
```

UpdateKind classifies a timeline update.

*Since `v0.1.0`*

<a id="UpdateOpened"></a>
<a id="UpdateNote"></a>
<a id="UpdateResolved"></a>
<a id="UpdateRecovered"></a>
<a id="UpdateBreaching"></a>

```go
const (
	UpdateOpened   UpdateKind = "opened"
	UpdateNote     UpdateKind = "update"
	UpdateResolved UpdateKind = "resolved"
	// UpdateRecovered and UpdateBreaching are added by detection when an
	// automatic incident's error rate recovers and when it is high again.
	UpdateRecovered UpdateKind = "recovered"
	UpdateBreaching UpdateKind = "breaching"
)
```

Update kinds.

*Since `v0.1.0`*
