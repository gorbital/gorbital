# modules/jobs

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/jobs"
```

Package jobs runs background jobs on PostgreSQL with River (ADR-0033).

Jobs are declared as definitions: code written and deployed by developers, with configuration (enabled, schedule, timeout, retries, queue) that operators change at runtime through a [Manager](#Manager), like serverless functions:

```go
defs := jobs.NewDefinitions()
jobs.Define(defs, jobs.Definition[cleanupsessions.Args]{
	Name:     "cleanup_sessions",
	Worker:   cleanupsessions.NewWorker(store),
	NewArgs:  func() cleanupsessions.Args { return cleanupsessions.Args{} },
	Enabled:  true,
	Schedule: "0 3 * * *",
})
workers := river.NewWorkers()
_ = jobs.AddMailWorker(workers, resendSender)
client, err := jobs.New(pool, workers,
	jobs.WithQueues(jobs.DefaultQueues()), jobs.WithDefinitions(defs))
```

The client is an app.Runner. Jobs enqueued from a request carry its request ID, trace and actor (but never its permissions); workers see them in their context (ADR-0030). River's tables are created by [Migrate](#Migrate).

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`MinScheduleInterval`](#MinScheduleInterval), [`MinTimeout`](#MinTimeout), [`MaxTimeout`](#MaxTimeout), [`MaxAttemptsLimit`](#MaxAttemptsLimit), [`DefaultDefinitionTimeout`](#DefaultDefinitionTimeout), [`DefaultDefinitionMaxAttempts`](#DefaultDefinitionMaxAttempts), [`DefaultStopTimeout`](#DefaultStopTimeout), [`DefaultCompletedRetention`](#DefaultCompletedRetention), [`DefaultCancelledRetention`](#DefaultCancelledRetention), [`DefaultDiscardedRetention`](#DefaultDiscardedRetention), [`DefaultResyncInterval`](#DefaultResyncInterval), [`MailKind`](#MailKind)
- Variables: [`ErrUnknownDefinition`](#ErrUnknownDefinition), [`ErrVersionConflict`](#ErrVersionConflict), [`ErrReasonRequired`](#ErrReasonRequired), [`ErrActorRequired`](#ErrActorRequired), [`ErrDefinitionDisabled`](#ErrDefinitionDisabled), [`ErrRunLimited`](#ErrRunLimited), [`ErrJobNotRetryable`](#ErrJobNotRetryable), [`ErrJobNotFound`](#ErrJobNotFound), [`ErrUnknownQueue`](#ErrUnknownQueue), [`ErrInvalidConfig`](#ErrInvalidConfig), [`ErrInvalidCursor`](#ErrInvalidCursor), [`Migrations`](#Migrations)
- Functions: [`AddMailWorker`](#AddMailWorker), [`AsyncSender`](#AsyncSender), [`DefaultQueues`](#DefaultQueues), [`Define`](#Define), [`Migrate`](#Migrate), [`MigrationsPending`](#MigrationsPending), [`OnBehalfOf`](#OnBehalfOf)
- Types:
  - [`AttemptError`](#AttemptError)
  - [`Change`](#Change)
  - [`Client`](#Client): [`New`](#New), [`Client.Insert`](#Client.Insert), [`Client.InsertTx`](#Client.InsertTx), [`Client.River`](#Client.River), [`Client.Run`](#Client.Run)
  - [`Config`](#Config): [`Config.Validate`](#Config.Validate)
  - [`ConfigPatch`](#ConfigPatch)
  - [`Definition`](#Definition)
  - [`DefinitionChange`](#DefinitionChange)
  - [`DefinitionView`](#DefinitionView)
  - [`Definitions`](#Definitions): [`NewDefinitions`](#NewDefinitions), [`Definitions.Names`](#Definitions.Names)
  - [`InvalidConfigError`](#InvalidConfigError): [`InvalidConfigError.Error`](#InvalidConfigError.Error), [`InvalidConfigError.Unwrap`](#InvalidConfigError.Unwrap)
  - [`JobFilter`](#JobFilter)
  - [`JobPage`](#JobPage)
  - [`JobRun`](#JobRun)
  - [`Manager`](#Manager): [`NewManager`](#NewManager), [`Manager.Cancel`](#Manager.Cancel), [`Manager.Definition`](#Manager.Definition), [`Manager.Definitions`](#Manager.Definitions), [`Manager.DeleteHistoryBefore`](#Manager.DeleteHistoryBefore), [`Manager.History`](#Manager.History), [`Manager.Job`](#Manager.Job), [`Manager.Jobs`](#Manager.Jobs), [`Manager.OldestHistory`](#Manager.OldestHistory), [`Manager.Overview`](#Manager.Overview), [`Manager.PauseQueue`](#Manager.PauseQueue), [`Manager.PauseQueueWithReason`](#Manager.PauseQueueWithReason), [`Manager.Queues`](#Manager.Queues), [`Manager.Reload`](#Manager.Reload), [`Manager.Reset`](#Manager.Reset), [`Manager.ResumeQueue`](#Manager.ResumeQueue), [`Manager.ResumeQueueWithReason`](#Manager.ResumeQueueWithReason), [`Manager.Retry`](#Manager.Retry), [`Manager.Run`](#Manager.Run), [`Manager.RunNow`](#Manager.RunNow), [`Manager.Scheduled`](#Manager.Scheduled), [`Manager.Update`](#Manager.Update)
  - [`ManagerOption`](#ManagerOption): [`WithManagerLogger`](#WithManagerLogger), [`WithResyncInterval`](#WithResyncInterval)
  - [`Option`](#Option): [`WithDefinitions`](#WithDefinitions), [`WithJobTimeout`](#WithJobTimeout), [`WithLogger`](#WithLogger), [`WithMaxAttempts`](#WithMaxAttempts), [`WithPropagator`](#WithPropagator), [`WithQueues`](#WithQueues), [`WithRetention`](#WithRetention), [`WithStopTimeout`](#WithStopTimeout), [`WithTracerProvider`](#WithTracerProvider)
  - [`Overview`](#Overview)
  - [`Queue`](#Queue)
  - [`QueueOverview`](#QueueOverview)

## Constants

<a id="MinScheduleInterval"></a>
<a id="MinTimeout"></a>
<a id="MaxTimeout"></a>
<a id="MaxAttemptsLimit"></a>

```go
const (
	MinScheduleInterval = time.Minute
	MinTimeout          = time.Second
	MaxTimeout          = 24 * time.Hour
	MaxAttemptsLimit    = 100
)
```

Bounds for job definition configuration (ADR-0033, threat 24).

*Since `v0.1.0`*

<a id="DefaultDefinitionTimeout"></a>
<a id="DefaultDefinitionMaxAttempts"></a>

```go
const (
	DefaultDefinitionTimeout     = time.Minute
	DefaultDefinitionMaxAttempts = 25
)
```

Defaults for definition fields left zero.

*Since `v0.1.0`*

<a id="DefaultStopTimeout"></a>
<a id="DefaultCompletedRetention"></a>
<a id="DefaultCancelledRetention"></a>
<a id="DefaultDiscardedRetention"></a>

```go
const (
	DefaultStopTimeout        = 20 * time.Second
	DefaultCompletedRetention = time.Hour
	DefaultCancelledRetention = 24 * time.Hour
	DefaultDiscardedRetention = 7 * 24 * time.Hour
)
```

Defaults applied by [New](#New).

*Since `v0.1.0`*

<a id="DefaultResyncInterval"></a>

```go
const DefaultResyncInterval = 5 * time.Minute
```

DefaultResyncInterval is how often a running [Manager](#Manager) reloads every override, covering notifications lost while disconnected.

*Since `v0.1.0`*

<a id="MailKind"></a>

```go
const MailKind = "gorbital.mail.send"
```

MailKind is the job kind that delivers queued email.

*Since `v0.1.0`*

## Variables

<a id="ErrUnknownDefinition"></a>
<a id="ErrVersionConflict"></a>
<a id="ErrReasonRequired"></a>
<a id="ErrActorRequired"></a>
<a id="ErrDefinitionDisabled"></a>
<a id="ErrRunLimited"></a>
<a id="ErrJobNotRetryable"></a>
<a id="ErrJobNotFound"></a>
<a id="ErrUnknownQueue"></a>
<a id="ErrInvalidConfig"></a>
<a id="ErrInvalidCursor"></a>

```go
var (
	// ErrUnknownDefinition reports a name that no definition declares.
	ErrUnknownDefinition = errors.New("jobs: unknown job definition")

	// ErrVersionConflict reports that the definition changed after the
	// caller read it. Read it again and retry.
	ErrVersionConflict = errors.New("jobs: job definition changed since it was read")

	// ErrReasonRequired reports a change that needs a reason without one:
	// disabling or rescheduling a job, changing its timeout, attempts or
	// queue, or pausing a queue.
	ErrReasonRequired = errors.New("jobs: a reason is required for this change")

	// ErrActorRequired reports a change without an authenticated actor in
	// the context.
	ErrActorRequired = errors.New("jobs: changes require an authenticated actor")

	// ErrDefinitionDisabled reports running a disabled job on demand.
	ErrDefinitionDisabled = errors.New("jobs: job definition is disabled")

	// ErrRunLimited reports running a job on demand while a run of it is
	// queued or running, or within [MinScheduleInterval] of its last run.
	ErrRunLimited = errors.New("jobs: the job is queued or running, or ran less than a minute ago")

	// ErrJobNotRetryable reports retrying a job that isn't waiting to retry,
	// discarded or cancelled: a completed job never runs again.
	ErrJobNotRetryable = errors.New("jobs: only jobs waiting to retry, discarded or cancelled can be retried")

	// ErrJobNotFound reports a job ID that doesn't exist, for example
	// because retention removed it.
	ErrJobNotFound = errors.New("jobs: job not found")

	// ErrUnknownQueue reports a queue no worker runs.
	ErrUnknownQueue = errors.New("jobs: queue is not active")

	// ErrInvalidConfig reports configuration outside the allowed bounds.
	// The error is an [*InvalidConfigError].
	ErrInvalidConfig = errors.New("jobs: invalid job configuration")

	// ErrInvalidCursor reports a malformed pagination cursor.
	ErrInvalidCursor = errors.New("jobs: invalid cursor")
)
```

Errors returned by [Manager](#Manager) methods. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the goose migrations for job definition overrides and their history. Apps copy them into db/migrations; River's own tables come from [Migrate](#Migrate).

*Since `v0.1.0`*

## Functions

<a id="AddMailWorker"></a>

### func AddMailWorker

```go
func AddMailWorker(workers *river.Workers, sender mail.Sender) error
```

AddMailWorker registers the worker that delivers queued email through sender, such as a Resend or SMTP provider. Register it on the workers of the client that works jobs.

*Since `v0.1.0`*

<a id="AsyncSender"></a>

### func AsyncSender

```go
func AsyncSender(client *Client) mail.Sender
```

AsyncSender returns a [mail.Sender](mail.md#Sender) that validates each message and queues it for the mail worker, returning once the job is stored. Delivery is retried up to 8 times, and each job's ID becomes the provider idempotency key, so a retry never sends twice (ADR-0025).

*Since `v0.1.0`*

<a id="DefaultQueues"></a>

### func DefaultQueues

```go
func DefaultQueues() map[string]river.QueueConfig
```

DefaultQueues returns the default queue with 10 workers per instance.

*Since `v0.1.0`*

<a id="Define"></a>

### func Define

```go
func Define[T river.JobArgs](defs *Definitions, d Definition[T])
```

Define adds a job definition. Invalid definitions are programming errors found at startup, so Define panics.

*Since `v0.1.0`*

<a id="Migrate"></a>

### func Migrate

```go
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]int, error)
```

Migrate applies River's pending schema migrations with River's own migrator and returns the versions applied. Run it from the app's migrate command after goose migrations (ADR-0033); never at startup (ADR-0017).

*Since `v0.1.0`*

<a id="MigrationsPending"></a>

### func MigrationsPending

```go
func MigrationsPending(ctx context.Context, pool *pgxpool.Pool) ([]string, error)
```

MigrationsPending describes River migrations not yet applied, empty when the schema is current. It changes nothing.

*Since `v0.1.0`*

<a id="OnBehalfOf"></a>

### func OnBehalfOf

```go
func OnBehalfOf(ctx context.Context) (actor.Actor, bool)
```

OnBehalfOf returns the actor who enqueued the job running with ctx. Inside a job the context actor is actor.System("jobs"); record this actor in audit metadata. It has no permissions: authorise work when enqueuing.

*Since `v0.1.0`*

## Types

<a id="AttemptError"></a>
<a id="AttemptError.At"></a>
<a id="AttemptError.Attempt"></a>
<a id="AttemptError.Message"></a>

### type AttemptError

```go
type AttemptError struct {
	At      time.Time
	Attempt int
	Message string
}
```

AttemptError is a failed attempt. Stack traces are omitted; they are in the logs.

*Since `v0.1.0`*

<a id="Change"></a>
<a id="Change.Version"></a>
<a id="Change.Reason"></a>

### type Change

```go
type Change struct {
	// Version is the DefinitionView.Version the caller last read.
	Version int64
	// Reason explains the change; required to disable or reschedule a job,
	// or to change its timeout, max attempts or queue.
	Reason string
}
```

Change carries what a caller supplies with a change. The actor comes from the context.

*Since `v0.1.0`*

<a id="Client"></a>

### type Client

```go
type Client struct {
	// contains filtered or unexported fields
}
```

Client enqueues and works jobs. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(pool *pgxpool.Pool, workers *river.Workers, opts ...Option) (*Client, error)
```

New builds a client on pool. workers may be nil for an insert-only client or when every job comes from definitions.

*Since `v0.1.0`*

<a id="Client.Insert"></a>

#### func (*Client) Insert

```go
func (c *Client) Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
```

Insert enqueues a job. The job carries ctx's request ID, trace and actor.

*Since `v0.1.0`*

<a id="Client.InsertTx"></a>

#### func (*Client) InsertTx

```go
func (c *Client) InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
```

InsertTx enqueues a job in tx: it becomes visible to workers only if tx commits.

*Since `v0.1.0`*

<a id="Client.River"></a>

#### func (*Client) River

```go
func (c *Client) River() *river.Client[pgx.Tx]
```

River returns the underlying River client for advanced use.

*Since `v0.1.0`*

<a id="Client.Run"></a>

#### func (*Client) Run

```go
func (c *Client) Run(ctx context.Context) error
```

Run works jobs until ctx is done, then stops: no new jobs are fetched, and running jobs get the stop timeout before their contexts are cancelled. It returns nil after a clean stop. An insert-only client just waits for ctx.

*Since `v0.1.0`*

<a id="Config"></a>
<a id="Config.Enabled"></a>
<a id="Config.Schedule"></a>
<a id="Config.Timeout"></a>
<a id="Config.MaxAttempts"></a>
<a id="Config.Queue"></a>
<a id="Config.Priority"></a>

### type Config

```go
type Config struct {
	// Enabled jobs run on their schedule and can be run on demand. Jobs
	// enqueued by application code run either way.
	Enabled bool
	// Schedule is a 5-field cron expression evaluated in UTC ("0 3 * * *"),
	// a descriptor ("@daily", "@every 15m"), or empty for on-demand jobs.
	Schedule string
	// Timeout bounds each attempt.
	Timeout time.Duration
	// MaxAttempts is the number of attempts before the job is discarded.
	MaxAttempts int
	// Queue is the queue new jobs go to; workers must run it.
	Queue string
	// Priority orders jobs within a queue, 1 (highest) to 4.
	Priority int
}
```

Config is a job definition's operator-editable configuration.

*Since `v0.1.0`*

<a id="Config.Validate"></a>

#### func (Config) Validate

```go
func (c Config) Validate() error
```

Validate reports whether c is within the allowed bounds.

*Since `v0.1.0`*

<a id="ConfigPatch"></a>
<a id="ConfigPatch.Enabled"></a>
<a id="ConfigPatch.Schedule"></a>
<a id="ConfigPatch.Timeout"></a>
<a id="ConfigPatch.MaxAttempts"></a>
<a id="ConfigPatch.Queue"></a>
<a id="ConfigPatch.Priority"></a>

### type ConfigPatch

```go
type ConfigPatch struct {
	Enabled     *bool
	Schedule    *string
	Timeout     *time.Duration
	MaxAttempts *int
	Queue       *string
	Priority    *int
}
```

ConfigPatch changes selected fields; nil fields keep their current value. Setting a field to its code default removes the override for that field.

*Since `v0.1.0`*

<a id="Definition"></a>
<a id="Definition.Name"></a>
<a id="Definition.Description"></a>
<a id="Definition.Worker"></a>
<a id="Definition.NewArgs"></a>
<a id="Definition.Enabled"></a>
<a id="Definition.Schedule"></a>
<a id="Definition.Timeout"></a>
<a id="Definition.MaxAttempts"></a>
<a id="Definition.Queue"></a>
<a id="Definition.Priority"></a>

### type Definition

```go
type Definition[T river.JobArgs] struct {
	// Name identifies the job; T's Kind() must return it. Names are public
	// API: renaming one orphans its overrides and history.
	Name        string
	Description string
	// Worker runs the job. Its Timeout method is ignored: the definition's
	// timeout applies.
	Worker river.Worker[T]
	// NewArgs builds the arguments for scheduled and on-demand runs.
	NewArgs func() T

	Enabled     bool
	Schedule    string
	Timeout     time.Duration
	MaxAttempts int
	Queue       string
	Priority    int
}
```

A Definition declares a named job whose configuration operators can change at runtime, like a serverless function: the code is deployed, the settings are edited in the admin panel. Zero Timeout, MaxAttempts, Queue and Priority take the defaults.

*Since `v0.1.0`*

<a id="DefinitionChange"></a>
<a id="DefinitionChange.ID"></a>
<a id="DefinitionChange.Name"></a>
<a id="DefinitionChange.Action"></a>
<a id="DefinitionChange.OldConfig"></a>
<a id="DefinitionChange.NewConfig"></a>
<a id="DefinitionChange.Version"></a>
<a id="DefinitionChange.Reason"></a>
<a id="DefinitionChange.ActorKind"></a>
<a id="DefinitionChange.ActorID"></a>
<a id="DefinitionChange.RequestID"></a>
<a id="DefinitionChange.ChangedAt"></a>

### type DefinitionChange

```go
type DefinitionChange struct {
	ID        int64
	Name      string
	Action    string
	OldConfig json.RawMessage
	NewConfig json.RawMessage
	Version   int64
	Reason    string
	ActorKind string
	ActorID   string
	RequestID string
	ChangedAt time.Time
}
```

DefinitionChange is one change to a job definition. OldConfig and NewConfig are the overridden fields as JSON objects; {} means the code defaults.

*Since `v0.1.0`*

<a id="DefinitionView"></a>
<a id="DefinitionView.Name"></a>
<a id="DefinitionView.Description"></a>
<a id="DefinitionView.Config"></a>
<a id="DefinitionView.Defaults"></a>
<a id="DefinitionView.Modified"></a>
<a id="DefinitionView.InvalidOverride"></a>
<a id="DefinitionView.Version"></a>
<a id="DefinitionView.UpdatedAt"></a>
<a id="DefinitionView.UpdatedBy"></a>
<a id="DefinitionView.NextRunAt"></a>
<a id="DefinitionView.LastRun"></a>

### type DefinitionView

```go
type DefinitionView struct {
	Name        string
	Description string
	// Config is the effective configuration; Defaults is the code's.
	Config   Config
	Defaults Config
	// Modified reports a valid override; InvalidOverride one that no longer
	// passes validation, so the defaults apply.
	Modified        bool
	InvalidOverride bool
	// Version increases with every change; pass it back to change the
	// definition. It is 0 for a definition never changed.
	Version   int64
	UpdatedAt time.Time
	UpdatedBy string
	// NextRunAt is the next scheduled time, approximately; zero when the job
	// is disabled or has no schedule.
	NextRunAt time.Time
	// LastRun is the most recent job of this definition, if any.
	LastRun *JobRun
}
```

DefinitionView is a job definition's configuration and status, for the admin panel.

*Since `v0.1.0`*

<a id="Definitions"></a>

### type Definitions

```go
type Definitions struct {
	// contains filtered or unexported fields
}
```

Definitions holds job definitions and the live overrides applied to them. Declare every definition before building the client. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewDefinitions"></a>

#### func NewDefinitions

```go
func NewDefinitions() *Definitions
```

NewDefinitions returns an empty set of definitions.

*Since `v0.1.0`*

<a id="Definitions.Names"></a>

#### func (*Definitions) Names

```go
func (defs *Definitions) Names() []string
```

Names returns the name of every defined job, in definition order. Job names are public API (ADR-0015); apps record them in their surface inventory (ADR-0054).

*Since `v0.1.0`*

<a id="InvalidConfigError"></a>
<a id="InvalidConfigError.Name"></a>
<a id="InvalidConfigError.Reason"></a>

### type InvalidConfigError

```go
type InvalidConfigError struct {
	Name   string
	Reason string
}
```

InvalidConfigError describes why a configuration change was rejected.

*Since `v0.1.0`*

<a id="InvalidConfigError.Error"></a>

#### func (*InvalidConfigError) Error

```go
func (e *InvalidConfigError) Error() string
```

Error names the job and the reason its configuration was rejected.

*Since `v0.1.0`*

<a id="InvalidConfigError.Unwrap"></a>

#### func (*InvalidConfigError) Unwrap

```go
func (e *InvalidConfigError) Unwrap() error
```

Unwrap returns [ErrInvalidConfig](#ErrInvalidConfig).

*Since `v0.1.0`*

<a id="JobFilter"></a>
<a id="JobFilter.Kind"></a>
<a id="JobFilter.Queue"></a>
<a id="JobFilter.States"></a>
<a id="JobFilter.Limit"></a>
<a id="JobFilter.Cursor"></a>

### type JobFilter

```go
type JobFilter struct {
	Kind   string
	Queue  string
	States []rivertype.JobState
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is JobPage.NextCursor from the previous page.
	Cursor string
}
```

JobFilter selects jobs to list. Empty fields match everything.

*Since `v0.1.0`*

<a id="JobPage"></a>
<a id="JobPage.Jobs"></a>
<a id="JobPage.NextCursor"></a>

### type JobPage

```go
type JobPage struct {
	Jobs []JobRun
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}
```

JobPage is a page of jobs, newest first.

*Since `v0.1.0`*

<a id="JobRun"></a>
<a id="JobRun.ID"></a>
<a id="JobRun.Kind"></a>
<a id="JobRun.Queue"></a>
<a id="JobRun.State"></a>
<a id="JobRun.Attempt"></a>
<a id="JobRun.MaxAttempts"></a>
<a id="JobRun.Priority"></a>
<a id="JobRun.CreatedAt"></a>
<a id="JobRun.ScheduledAt"></a>
<a id="JobRun.AttemptedAt"></a>
<a id="JobRun.FinalizedAt"></a>
<a id="JobRun.Errors"></a>
<a id="JobRun.RequestID"></a>
<a id="JobRun.ActorKind"></a>
<a id="JobRun.ActorID"></a>

### type JobRun

```go
type JobRun struct {
	ID          int64
	Kind        string
	Queue       string
	State       rivertype.JobState
	Attempt     int
	MaxAttempts int
	Priority    int
	CreatedAt   time.Time
	ScheduledAt time.Time
	AttemptedAt *time.Time
	FinalizedAt *time.Time
	Errors      []AttemptError
	// RequestID and the enqueuing actor come from the job's context metadata.
	RequestID string
	ActorKind string
	ActorID   string
}
```

JobRun is one job and its attempts, without its arguments: arguments can contain personal data and are never shown in the admin panel.

*Since `v0.1.0`*

<a id="Manager"></a>

### type Manager

```go
type Manager struct {
	// contains filtered or unexported fields
}
```

Manager is the admin-panel backend for jobs (ADR-0033): it stores operator overrides of job definitions, keeps every instance's definitions and schedules current, and inspects and controls runs and queues. It is an app.Runner. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewManager"></a>

#### func NewManager

```go
func NewManager(ctx context.Context, pool *pgxpool.Pool, client *Client, recorder audit.Recorder, opts ...ManagerOption) (*Manager, error)
```

NewManager loads stored overrides for client's definitions and returns the manager. Changes are recorded as audit events through recorder. The tables come from [Migrations](#Migrations).

*Since `v0.1.0`*

<a id="Manager.Cancel"></a>

#### func (*Manager) Cancel

```go
func (m *Manager) Cancel(ctx context.Context, id int64) (JobRun, error)
```

Cancel cancels a job: queued jobs never run, and a running job's context is cancelled. It returns [ErrJobNotFound](#ErrJobNotFound) or [ErrActorRequired](#ErrActorRequired).

*Since `v0.1.0`*

<a id="Manager.Definition"></a>

#### func (*Manager) Definition

```go
func (m *Manager) Definition(ctx context.Context, name string) (DefinitionView, error)
```

Definition returns one definition, or [ErrUnknownDefinition](#ErrUnknownDefinition).

*Since `v0.1.0`*

<a id="Manager.Definitions"></a>

#### func (*Manager) Definitions

```go
func (m *Manager) Definitions(ctx context.Context) ([]DefinitionView, error)
```

Definitions returns every definition in declaration order.

*Since `v0.1.0`*

<a id="Manager.DeleteHistoryBefore"></a>

#### func (*Manager) DeleteHistoryBefore

```go
func (m *Manager) DeleteHistoryBefore(ctx context.Context, before time.Time, limit int) (int64, error)
```

DeleteHistoryBefore deletes up to limit job configuration changes made before before, oldest first, and returns how many it deleted. Retention calls it until it deletes fewer than limit (ADR-0051).

*Since `v0.1.0`*

<a id="Manager.History"></a>

#### func (*Manager) History

```go
func (m *Manager) History(ctx context.Context, name string, before int64, limit int) ([]DefinitionChange, error)
```

History returns changes to name, newest first. For the next page, pass the last entry's ID as before (0 starts from the newest). limit is clamped to 1–100.

*Since `v0.1.0`*

<a id="Manager.Job"></a>

#### func (*Manager) Job

```go
func (m *Manager) Job(ctx context.Context, id int64) (JobRun, error)
```

Job returns one job, or [ErrJobNotFound](#ErrJobNotFound).

*Since `v0.1.0`*

<a id="Manager.Jobs"></a>

#### func (*Manager) Jobs

```go
func (m *Manager) Jobs(ctx context.Context, f JobFilter) (JobPage, error)
```

Jobs lists jobs, newest first. It returns [ErrInvalidCursor](#ErrInvalidCursor) for a malformed cursor.

*Since `v0.1.0`*

<a id="Manager.OldestHistory"></a>

#### func (*Manager) OldestHistory

```go
func (m *Manager) OldestHistory(ctx context.Context) (oldest time.Time, ok bool, err error)
```

OldestHistory returns when the oldest recorded configuration change was made; ok is false when there are none.

*Since `v0.1.0`*

<a id="Manager.Overview"></a>

#### func (*Manager) Overview

```go
func (m *Manager) Overview(ctx context.Context) (Overview, error)
```

Overview returns the job overview.

*Since `v0.1.0`*

<a id="Manager.PauseQueue"></a>

#### func (*Manager) PauseQueue

```go
func (m *Manager) PauseQueue(ctx context.Context, name string) error
```

PauseQueue always returns [ErrReasonRequired](#ErrReasonRequired): pausing a queue needs a reason.

Deprecated: Use [Manager.PauseQueueWithReason](#Manager.PauseQueueWithReason).

*Since `v0.1.0`*

<a id="Manager.PauseQueueWithReason"></a>

#### func (*Manager) PauseQueueWithReason

```go
func (m *Manager) PauseQueueWithReason(ctx context.Context, name, reason string) error
```

PauseQueueWithReason stops every instance fetching jobs from name, and records reason in the audit event. Running jobs finish. Pausing stops every job in the queue, email delivery included, so it needs a reason. It returns [ErrReasonRequired](#ErrReasonRequired), [ErrUnknownQueue](#ErrUnknownQueue) or [ErrActorRequired](#ErrActorRequired).

*Since `v0.1.0`*

<a id="Manager.Queues"></a>

#### func (*Manager) Queues

```go
func (m *Manager) Queues(ctx context.Context) ([]Queue, error)
```

Queues lists active queues by name.

*Since `v0.1.0`*

<a id="Manager.Reload"></a>

#### func (*Manager) Reload

```go
func (m *Manager) Reload(ctx context.Context) error
```

Reload reads every override from the database and reapplies schedules.

*Since `v0.1.0`*

<a id="Manager.Reset"></a>

#### func (*Manager) Reset

```go
func (m *Manager) Reset(ctx context.Context, name string, change Change) (DefinitionView, error)
```

Reset returns name to its code defaults. It returns the same errors as [Manager.Update](#Manager.Update).

*Since `v0.1.0`*

<a id="Manager.ResumeQueue"></a>

#### func (*Manager) ResumeQueue

```go
func (m *Manager) ResumeQueue(ctx context.Context, name string) error
```

ResumeQueue resumes a paused queue on every instance.

*Since `v0.1.0`*

<a id="Manager.ResumeQueueWithReason"></a>

#### func (*Manager) ResumeQueueWithReason

```go
func (m *Manager) ResumeQueueWithReason(ctx context.Context, name, reason string) error
```

ResumeQueueWithReason resumes a paused queue on every instance, recording reason, which may be empty, in the audit event.

*Since `v0.1.0`*

<a id="Manager.Retry"></a>

#### func (*Manager) Retry

```go
func (m *Manager) Retry(ctx context.Context, id int64) (JobRun, error)
```

Retry makes a job waiting to retry, discarded or cancelled available to run again immediately. Completed, queued and running jobs are refused, so a delivered email or a finished purge never runs twice, and so are jobs of a disabled definition. It returns [ErrJobNotFound](#ErrJobNotFound), [ErrJobNotRetryable](#ErrJobNotRetryable), [ErrDefinitionDisabled](#ErrDefinitionDisabled) or [ErrActorRequired](#ErrActorRequired).

*Since `v0.1.0`*

<a id="Manager.Run"></a>

#### func (*Manager) Run

```go
func (m *Manager) Run(ctx context.Context) error
```

Run keeps job definitions and schedules current until ctx is done, then returns nil. It listens on a dedicated connection for changes committed by any instance, reloads everything after connecting, and reloads everything every resync interval as a fallback. Connection failures are logged and retried with backoff.

*Since `v0.1.0`*

<a id="Manager.RunNow"></a>

#### func (*Manager) RunNow

```go
func (m *Manager) RunNow(ctx context.Context, name string) (JobRun, error)
```

RunNow enqueues a job for an enabled definition immediately, with its current configuration. A definition runs on demand at most once at a time and once per [MinScheduleInterval](#MinScheduleInterval), the same floor as schedules. It returns [ErrUnknownDefinition](#ErrUnknownDefinition), [ErrDefinitionDisabled](#ErrDefinitionDisabled), [ErrRunLimited](#ErrRunLimited) or [ErrActorRequired](#ErrActorRequired).

*Since `v0.1.0`*

<a id="Manager.Scheduled"></a>

#### func (*Manager) Scheduled

```go
func (m *Manager) Scheduled(ctx context.Context) ([]DefinitionView, error)
```

Scheduled returns enabled definitions with a schedule, soonest first.

*Since `v0.1.0`*

<a id="Manager.Update"></a>

#### func (*Manager) Update

```go
func (m *Manager) Update(ctx context.Context, name string, patch ConfigPatch, change Change) (DefinitionView, error)
```

Update applies patch to name's configuration. The change reaches every instance within moments; a new schedule takes effect on the leader. It returns [ErrUnknownDefinition](#ErrUnknownDefinition), an [\*InvalidConfigError](#InvalidConfigError), [ErrUnknownQueue](#ErrUnknownQueue), [ErrReasonRequired](#ErrReasonRequired), [ErrActorRequired](#ErrActorRequired) or [ErrVersionConflict](#ErrVersionConflict).

*Since `v0.1.0`*

<a id="ManagerOption"></a>

### type ManagerOption

```go
type ManagerOption interface {
	// contains filtered or unexported methods
}
```

A ManagerOption configures [NewManager](#NewManager).

*Since `v0.1.0`*

<a id="WithManagerLogger"></a>

#### func WithManagerLogger

```go
func WithManagerLogger(logger *slog.Logger) ManagerOption
```

WithManagerLogger sets the logger for listener and invalid-override warnings. Default: discard.

*Since `v0.1.0`*

<a id="WithResyncInterval"></a>

#### func WithResyncInterval

```go
func WithResyncInterval(d time.Duration) ManagerOption
```

WithResyncInterval sets how often [Manager.Run](#Manager.Run) reloads every override. Default: [DefaultResyncInterval](#DefaultResyncInterval).

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [New](#New).

*Since `v0.1.0`*

<a id="WithDefinitions"></a>

#### func WithDefinitions

```go
func WithDefinitions(defs *Definitions) Option
```

WithDefinitions registers job definitions: their workers, their live configuration and, on working clients, their schedules. Use the same definitions for API and worker processes.

*Since `v0.1.0`*

<a id="WithJobTimeout"></a>

#### func WithJobTimeout

```go
func WithJobTimeout(d time.Duration) Option
```

WithJobTimeout sets the time a job without a definition may run. Default: River's, one minute.

*Since `v0.1.0`*

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

WithLogger sets the logger for job failures and River's own messages. Email addresses in their messages and attributes are redacted. Default: discard.

*Since `v0.1.0`*

<a id="WithMaxAttempts"></a>

#### func WithMaxAttempts

```go
func WithMaxAttempts(n int) Option
```

WithMaxAttempts sets the attempts for jobs without a definition before they are discarded. Default: River's, 25.

*Since `v0.1.0`*

<a id="WithPropagator"></a>

#### func WithPropagator

```go
func WithPropagator(p propagation.TextMapPropagator) Option
```

WithPropagator sets how trace context is stored in job metadata. Default: the global OpenTelemetry propagator, which telemetry.Setup configures.

*Since `v0.1.0`*

<a id="WithQueues"></a>

#### func WithQueues

```go
func WithQueues(queues map[string]river.QueueConfig) Option
```

WithQueues makes the client work jobs from queues. Without it the client only enqueues, as an API process does when a separate worker process runs jobs.

*Since `v0.1.0`*

<a id="WithRetention"></a>

#### func WithRetention

```go
func WithRetention(completed, cancelled, discarded time.Duration) Option
```

WithRetention sets how long finished jobs are kept. -1 keeps them forever. Job arguments may contain personal data such as email bodies, so keep completed jobs briefly. Defaults: [DefaultCompletedRetention](#DefaultCompletedRetention), [DefaultCancelledRetention](#DefaultCancelledRetention), [DefaultDiscardedRetention](#DefaultDiscardedRetention).

*Since `v0.1.0`*

<a id="WithStopTimeout"></a>

#### func WithStopTimeout

```go
func WithStopTimeout(d time.Duration) Option
```

WithStopTimeout sets how long running jobs may continue after shutdown starts before their contexts are cancelled. Keep it below the app's shutdown timeout. Default: [DefaultStopTimeout](#DefaultStopTimeout).

*Since `v0.1.0`*

<a id="WithTracerProvider"></a>

#### func WithTracerProvider

```go
func WithTracerProvider(tp trace.TracerProvider) Option
```

WithTracerProvider sets the provider for job spans. Default: the global OpenTelemetry provider. Trace context is propagated with the global propagator.

*Since `v0.1.0`*

<a id="Overview"></a>
<a id="Overview.Queues"></a>
<a id="Overview.Failing"></a>

### type Overview

```go
type Overview struct {
	// Queues are the queues with active workers or unfinished jobs, by name.
	Queues []QueueOverview
	// Failing are the definitions whose most recent run failed: retrying or
	// discarded.
	Failing []DefinitionView
}
```

Overview summarises job work across every instance, for GET /ops/jobs/overview (ADR-0051).

*Since `v0.1.0`*

<a id="Queue"></a>
<a id="Queue.Name"></a>
<a id="Queue.Paused"></a>
<a id="Queue.PausedAt"></a>
<a id="Queue.CreatedAt"></a>
<a id="Queue.UpdatedAt"></a>

### type Queue

```go
type Queue struct {
	Name      string
	Paused    bool
	PausedAt  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

Queue is a queue workers run or recently ran.

*Since `v0.1.0`*

<a id="QueueOverview"></a>
<a id="QueueOverview.Name"></a>
<a id="QueueOverview.Paused"></a>
<a id="QueueOverview.Active"></a>
<a id="QueueOverview.Available"></a>
<a id="QueueOverview.Scheduled"></a>
<a id="QueueOverview.Running"></a>
<a id="QueueOverview.Retryable"></a>
<a id="QueueOverview.DiscardedLastDay"></a>

### type QueueOverview

```go
type QueueOverview struct {
	Name   string
	Paused bool
	// Active reports that some instance runs workers for the queue.
	Active                                   bool
	Available, Scheduled, Running, Retryable int64
	// DiscardedLastDay counts jobs that ran out of attempts in the last 24
	// hours.
	DiscardedLastDay int64
}
```

QueueOverview counts one queue's unfinished jobs.

*Since `v0.1.0`*
