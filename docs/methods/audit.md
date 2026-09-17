# audit

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/audit"
```

Package audit defines audit events and the [Recorder](#Recorder) contract that any module uses to record who did what to which resource, and whether it worked. Storage lives in separate modules (for example auditpg).

Audit events are not application logs: they have their own retention, integrity and access rules (ADR-0026).

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Types:
  - [`Event`](#Event): [`FromContext`](#FromContext), [`Event.Validate`](#Event.Validate)
  - [`LogRecorder`](#LogRecorder): [`NewLogRecorder`](#NewLogRecorder), [`LogRecorder.Record`](#LogRecorder.Record)
  - [`Outcome`](#Outcome): [`OutcomeSuccess`](#OutcomeSuccess), [`OutcomeFailure`](#OutcomeFailure), [`OutcomeDenied`](#OutcomeDenied)
  - [`Recorder`](#Recorder)
  - [`RecorderFunc`](#RecorderFunc): [`RecorderFunc.Record`](#RecorderFunc.Record)

## Types

<a id="Event"></a>
<a id="Event.OccurredAt"></a>
<a id="Event.ActorKind"></a>
<a id="Event.ActorID"></a>
<a id="Event.ActorLabel"></a>
<a id="Event.Action"></a>
<a id="Event.ResourceType"></a>
<a id="Event.ResourceID"></a>
<a id="Event.Outcome"></a>
<a id="Event.OrgID"></a>
<a id="Event.RequestID"></a>
<a id="Event.TraceID"></a>
<a id="Event.IP"></a>
<a id="Event.UserAgent"></a>
<a id="Event.Metadata"></a>

### type Event

```go
type Event struct {
	// OccurredAt is set by recorders when zero.
	OccurredAt time.Time

	ActorKind  actor.Kind
	ActorID    string
	ActorLabel string

	// Action is a dotted, past-tense name namespaced by module, such as
	// "auth.session.revoked". Action names are public API (ADR-0015).
	Action string

	ResourceType string
	ResourceID   string
	Outcome      Outcome
	OrgID        string

	RequestID string
	TraceID   string
	IP        string
	UserAgent string

	// Metadata holds structured detail. Recorders apply redaction rules.
	Metadata map[string]any
}
```

An Event records one audited action.

*Since `v0.1.0`*

<a id="FromContext"></a>

#### func FromContext

```go
func FromContext(ctx context.Context, e Event) Event
```

FromContext returns e with empty actor, organisation, request, trace, IP and user agent fields filled from ctx. The IP address and user agent come from [actor.WithClient](actor.md#WithClient).

*Since `v0.1.0`*

<a id="Event.Validate"></a>

#### func (Event) Validate

```go
func (e Event) Validate() error
```

Validate reports whether e has a well-formed action and a known outcome.

*Since `v0.1.0`*

<a id="LogRecorder"></a>

### type LogRecorder

```go
type LogRecorder struct {
	// contains filtered or unexported fields
}
```

LogRecorder writes audit events to a structured logger. It is meant for development and apps without an audit store; metadata is not logged.

*Since `v0.1.0`*

<a id="NewLogRecorder"></a>

#### func NewLogRecorder

```go
func NewLogRecorder(logger *slog.Logger) *LogRecorder
```

NewLogRecorder returns a recorder logging to logger.

*Since `v0.1.0`*

<a id="LogRecorder.Record"></a>

#### func (*LogRecorder) Record

```go
func (r *LogRecorder) Record(ctx context.Context, e Event) error
```

Record validates e, fills it from ctx and logs it.

*Since `v0.1.0`*

<a id="Outcome"></a>

### type Outcome

```go
type Outcome string
```

Outcome is the result of an audited action.

*Since `v0.1.0`*

<a id="OutcomeSuccess"></a>
<a id="OutcomeFailure"></a>
<a id="OutcomeDenied"></a>

```go
const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeDenied  Outcome = "denied"
)
```

Outcomes.

*Since `v0.1.0`*

<a id="Recorder"></a>
<a id="Recorder.Record"></a>

### type Recorder

```go
type Recorder interface {
	Record(ctx context.Context, e Event) error
}
```

A Recorder stores audit events. Implementations must be safe for concurrent use and return an error when the event can't be stored.

*Since `v0.1.0`*

<a id="RecorderFunc"></a>

### type RecorderFunc

```go
type RecorderFunc func(ctx context.Context, e Event) error
```

RecorderFunc adapts a function to the [Recorder](#Recorder) interface.

*Since `v0.1.0`*

<a id="RecorderFunc.Record"></a>

#### func (RecorderFunc) Record

```go
func (f RecorderFunc) Record(ctx context.Context, e Event) error
```

Record calls f(ctx, e).

*Since `v0.1.0`*
