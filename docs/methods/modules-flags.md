# modules/flags

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/flags"
```

Package flags provides feature flags: on/off switches declared in Go, targeted at organisations and users, rolled out to a stable percentage of them, stored in PostgreSQL only when changed, and applied on every instance without a restart (ADR-0057).

```
reg := flags.NewRegistry()
newCheckout := flags.Bool(reg, "checkout.new_flow",
	flags.Describe("The redesigned checkout."),
	flags.Client(), // listed to signed-in clients by GET /v1/flags
)
store, err := flags.NewStore(ctx, pool, reg, recorder)
// Run store with the app's runners; ask the flag where it matters:
if newCheckout.Enabled(ctx) { ... }
```

## Evaluation

A flag's [State](#State) is its declared default until an operator changes it with [Store.Set](#Store.Set). [Flag.Enabled](#Flag.Enabled) reads the state from memory and the actor from ctx (actor.From), and decides, first rule that applies:

 1. The flag is disabled: false.
 2. The actor acts in an organisation (actor.Actor.OrgID, set by orgs.RequireMember): false if the organisation is in Orgs.Deny, true if it is in Orgs.Allow.
 3. The actor is authenticated (any kind but anonymous, with an ID): false if its ID is in Users.Deny, true if it is in Users.Allow.
 4. A rollout percentage is set: true when the subject's [Bucket](#Bucket) is below it. The subject is the organisation when the actor acts in one, so every member gets the same answer, and the actor's ID otherwise. Anonymous callers have no subject: a percentage of 0 or 100 still applies to them, and any other percentage leaves them to the default.
 5. The state's default.

Evaluation is deterministic: the same flag, state and subject always give the same answer, on every instance, and raising a percentage only adds subjects. It never touches the database.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`MaxTargets`](#MaxTargets), [`MaxIDLen`](#MaxIDLen), [`ActionChanged`](#ActionChanged), [`ActionReset`](#ActionReset), [`DefaultResyncInterval`](#DefaultResyncInterval)
- Variables: [`ErrUnknownFlag`](#ErrUnknownFlag), [`ErrVersionConflict`](#ErrVersionConflict), [`ErrReasonRequired`](#ErrReasonRequired), [`ErrActorRequired`](#ErrActorRequired), [`ErrInvalidState`](#ErrInvalidState), [`Migrations`](#Migrations)
- Functions: [`Bucket`](#Bucket), [`Percent`](#Percent)
- Types:
  - [`Change`](#Change)
  - [`Evaluation`](#Evaluation)
  - [`Flag`](#Flag): [`Bool`](#Bool), [`Flag.Enabled`](#Flag.Enabled), [`Flag.Evaluate`](#Flag.Evaluate), [`Flag.Get`](#Flag.Get), [`Flag.Key`](#Flag.Key)
  - [`HistoryEntry`](#HistoryEntry)
  - [`InvalidStateError`](#InvalidStateError): [`InvalidStateError.Error`](#InvalidStateError.Error), [`InvalidStateError.Unwrap`](#InvalidStateError.Unwrap)
  - [`Option`](#Option): [`Client`](#Client), [`DefaultOn`](#DefaultOn), [`Describe`](#Describe), [`Group`](#Group)
  - [`Reason`](#Reason): [`ReasonDisabled`](#ReasonDisabled), [`ReasonOrgDenied`](#ReasonOrgDenied), [`ReasonOrgAllowed`](#ReasonOrgAllowed), [`ReasonUserDenied`](#ReasonUserDenied), [`ReasonUserAllowed`](#ReasonUserAllowed), [`ReasonRollout`](#ReasonRollout), [`ReasonDefault`](#ReasonDefault)
  - [`Registry`](#Registry): [`NewRegistry`](#NewRegistry), [`Registry.Keys`](#Registry.Keys)
  - [`State`](#State)
  - [`Store`](#Store): [`NewStore`](#NewStore), [`Store.ClientFlags`](#Store.ClientFlags), [`Store.DeleteHistoryBefore`](#Store.DeleteHistoryBefore), [`Store.Get`](#Store.Get), [`Store.History`](#Store.History), [`Store.List`](#Store.List), [`Store.OldestHistory`](#Store.OldestHistory), [`Store.Reload`](#Store.Reload), [`Store.Reset`](#Store.Reset), [`Store.Run`](#Store.Run), [`Store.Set`](#Store.Set), [`Store.UnknownKeys`](#Store.UnknownKeys)
  - [`StoreOption`](#StoreOption): [`WithLogger`](#WithLogger), [`WithResyncInterval`](#WithResyncInterval)
  - [`Targets`](#Targets)
  - [`View`](#View)

## Constants

<a id="MaxTargets"></a>
<a id="MaxIDLen"></a>

```go
const (
	// MaxTargets bounds each allow and deny list. Longer lists belong in a
	// rollout percentage, or in the app's own data.
	MaxTargets = 1000
	// MaxIDLen bounds an organisation or user ID in a list.
	MaxIDLen = 100
)
```

Bounds of a [State](#State), checked by [Store.Set](#Store.Set) and when states load.

*Since `v0.1.0`*

<a id="ActionChanged"></a>
<a id="ActionReset"></a>

```go
const (
	ActionChanged = "flags.flag.changed"
	ActionReset   = "flags.flag.reset"
)
```

Audit actions the store records. They are public API (ADR-0015).

*Since `v0.1.0`*

<a id="DefaultResyncInterval"></a>

```go
const DefaultResyncInterval = 5 * time.Minute
```

DefaultResyncInterval is how often a running [Store](#Store) reloads every state, covering notifications lost while disconnected.

*Since `v0.1.0`*

## Variables

<a id="ErrUnknownFlag"></a>
<a id="ErrVersionConflict"></a>
<a id="ErrReasonRequired"></a>
<a id="ErrActorRequired"></a>
<a id="ErrInvalidState"></a>

```go
var (
	// ErrUnknownFlag reports a key that isn't declared in the registry.
	ErrUnknownFlag = errors.New("flags: unknown flag")

	// ErrVersionConflict reports that the flag changed after the caller
	// read it. Read it again and retry.
	ErrVersionConflict = errors.New("flags: flag changed since it was read")

	// ErrReasonRequired reports a change without a reason. Every flag change
	// needs one: flags change what users get in production.
	ErrReasonRequired = errors.New("flags: a reason is required to change a flag")

	// ErrActorRequired reports a change without an authenticated actor in
	// the context.
	ErrActorRequired = errors.New("flags: changes require an authenticated actor")

	// ErrInvalidState reports a state outside the bounds. The error is an
	// [*InvalidStateError].
	ErrInvalidState = errors.New("flags: invalid state")
)
```

Errors returned by [Store](#Store) methods. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the module's goose migrations: the flags\_states and flags\_history tables. Apps copy them into db/migrations; tests can apply them directly with pgtest.

*Since `v0.1.0`*

## Functions

<a id="Bucket"></a>

### func Bucket

```go
func Bucket(key, subject string) int
```

Bucket returns subject's rollout bucket for key, from 0 to 99: the first eight bytes of SHA-256 of key, a zero byte and subject, as a big-endian unsigned integer, modulo 100. A subject is in a rollout of p percent when its bucket is below p. Each flag buckets subjects independently, so the same 10% of users don't get every new feature first.

*Since `v0.1.0`*

**Example**

```go
// A subject is in a 30% rollout of a flag when its bucket is below 30.
fmt.Println(flags.Bucket("checkout.new_flow", "usr_1") < 30)
```

Output:

```text
true
```

<a id="Percent"></a>

### func Percent

```go
func Percent(p int) *int
```

Percent returns a pointer to p, for [State.Percentage](#State.Percentage).

*Since `v0.1.0`*

## Types

<a id="Change"></a>
<a id="Change.Version"></a>
<a id="Change.Reason"></a>

### type Change

```go
type Change struct {
	// Version is the View.Version the caller last read.
	Version int64
	// Reason explains the change; always required.
	Reason string
}
```

Change carries what a caller supplies with a change. The actor comes from the context.

*Since `v0.1.0`*

<a id="Evaluation"></a>
<a id="Evaluation.Key"></a>
<a id="Evaluation.Enabled"></a>
<a id="Evaluation.Reason"></a>

### type Evaluation

```go
type Evaluation struct {
	Key     string
	Enabled bool
	Reason  Reason
}
```

Evaluation is a flag's answer for one actor.

*Since `v0.1.0`*

<a id="Flag"></a>

### type Flag

```go
type Flag struct {
	// contains filtered or unexported fields
}
```

A Flag is a handle to one declared flag. It implements [config.Value](config.md#Value)\[bool], so library modules can accept a flag as a live boolean option without importing this package.

*Since `v0.1.0`*

<a id="Bool"></a>

#### func Bool

```go
func Bool(r *Registry, key string, opts ...Option) *Flag
```

Bool declares an on/off flag. It is off for everyone until declared with [DefaultOn](#DefaultOn) or turned on with [Store.Set](#Store.Set). Invalid declarations are programming errors found at startup, so they panic.

*Since `v0.1.0`*

**Example**

```go
reg := flags.NewRegistry()
newCheckout := flags.Bool(reg, "checkout.new_flow",
	flags.Describe("The redesigned checkout."),
	flags.Client(),
)

// Until an operator turns it on with Store.Set, a flag is off.
ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"})
fmt.Println(newCheckout.Evaluate(ctx))
```

Output:

```text
{checkout.new_flow false disabled}
```

<a id="Flag.Enabled"></a>

#### func (*Flag) Enabled

```go
func (f *Flag) Enabled(ctx context.Context) bool
```

Enabled reports whether the flag is on for the actor in ctx, by the rules in the package documentation. It never touches the database.

*Since `v0.1.0`*

<a id="Flag.Evaluate"></a>

#### func (*Flag) Evaluate

```go
func (f *Flag) Evaluate(ctx context.Context) Evaluation
```

Evaluate returns the flag's answer for the actor in ctx and the rule that decided it.

*Since `v0.1.0`*

<a id="Flag.Get"></a>

#### func (*Flag) Get

```go
func (f *Flag) Get(ctx context.Context) bool
```

Get returns [Flag.Enabled](#Flag.Enabled), so a Flag is a [config.Value](config.md#Value)\[bool].

*Since `v0.1.0`*

<a id="Flag.Key"></a>

#### func (*Flag) Key

```go
func (f *Flag) Key() string
```

Key returns the flag's key.

*Since `v0.1.0`*

<a id="HistoryEntry"></a>
<a id="HistoryEntry.ID"></a>
<a id="HistoryEntry.Key"></a>
<a id="HistoryEntry.OldState"></a>
<a id="HistoryEntry.NewState"></a>
<a id="HistoryEntry.OldInvalid"></a>
<a id="HistoryEntry.NewInvalid"></a>
<a id="HistoryEntry.Version"></a>
<a id="HistoryEntry.Reason"></a>
<a id="HistoryEntry.ActorKind"></a>
<a id="HistoryEntry.ActorID"></a>
<a id="HistoryEntry.RequestID"></a>
<a id="HistoryEntry.ChangedAt"></a>

### type HistoryEntry

```go
type HistoryEntry struct {
	ID       int64
	Key      string
	OldState *State
	NewState *State
	// OldInvalid and NewInvalid report a recorded state that no longer
	// passes validation, returned as nil.
	OldInvalid bool
	NewInvalid bool
	Version    int64
	Reason     string
	ActorKind  actor.Kind
	ActorID    string
	RequestID  string
	ChangedAt  time.Time
}
```

HistoryEntry is one change to a flag. A nil state means the declared default.

*Since `v0.1.0`*

<a id="InvalidStateError"></a>
<a id="InvalidStateError.Key"></a>
<a id="InvalidStateError.Reason"></a>

### type InvalidStateError

```go
type InvalidStateError struct {
	Key    string
	Reason string
}
```

InvalidStateError describes why a state was rejected. Reason names the field and never contains an ID.

*Since `v0.1.0`*

<a id="InvalidStateError.Error"></a>

#### func (*InvalidStateError) Error

```go
func (e *InvalidStateError) Error() string
```

Error names the flag key and the reason the state was rejected.

*Since `v0.1.0`*

<a id="InvalidStateError.Unwrap"></a>

#### func (*InvalidStateError) Unwrap

```go
func (e *InvalidStateError) Unwrap() error
```

Unwrap returns [ErrInvalidState](#ErrInvalidState).

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures a flag declaration.

*Since `v0.1.0`*

<a id="Client"></a>

#### func Client

```go
func Client() Option
```

Client marks a flag clients may read: [Store.ClientFlags](#Store.ClientFlags) evaluates only these, for endpoints such as GET /v1/flags. Other flags stay server-side, so their keys don't reveal unreleased features.

*Since `v0.1.0`*

<a id="DefaultOn"></a>

#### func DefaultOn

```go
func DefaultOn() Option
```

DefaultOn declares the flag enabled with a default of true: on for everyone until an operator changes it. Use it for kill switches of features already released. Without it, a flag is disabled until an operator turns it on.

*Since `v0.1.0`*

<a id="Describe"></a>

#### func Describe

```go
func Describe(text string) Option
```

Describe sets the help text shown to operators.

*Since `v0.1.0`*

<a id="Group"></a>

#### func Group

```go
func Group(name string) Option
```

Group sets the group used to organise flags in listings. Default: the key's first segment.

*Since `v0.1.0`*

<a id="Reason"></a>

### type Reason

```go
type Reason string
```

Reason is the rule that decided an evaluation.

*Since `v0.1.0`*

<a id="ReasonDisabled"></a>
<a id="ReasonOrgDenied"></a>
<a id="ReasonOrgAllowed"></a>
<a id="ReasonUserDenied"></a>
<a id="ReasonUserAllowed"></a>
<a id="ReasonRollout"></a>
<a id="ReasonDefault"></a>

```go
const (
	ReasonDisabled    Reason = "disabled"
	ReasonOrgDenied   Reason = "org_denied"
	ReasonOrgAllowed  Reason = "org_allowed"
	ReasonUserDenied  Reason = "user_denied"
	ReasonUserAllowed Reason = "user_allowed"
	ReasonRollout     Reason = "rollout"
	ReasonDefault     Reason = "default"
)
```

Evaluation reasons, in the order the rules apply.

*Since `v0.1.0`*

<a id="Registry"></a>

### type Registry

```go
type Registry struct {
	// contains filtered or unexported fields
}
```

A Registry holds declared flags and their current states. Declare every flag before calling [NewStore](#NewStore). A Registry is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewRegistry"></a>

#### func NewRegistry

```go
func NewRegistry() *Registry
```

NewRegistry returns an empty registry.

*Since `v0.1.0`*

<a id="Registry.Keys"></a>

#### func (*Registry) Keys

```go
func (r *Registry) Keys() []string
```

Keys returns the key of every declared flag, in declaration order. Flag keys are public API (ADR-0015): clients read them from GET /v1/flags, and apps record them in their surface inventory (ADR-0054).

*Since `v0.1.0`*

<a id="State"></a>
<a id="State.Enabled"></a>
<a id="State.Default"></a>
<a id="State.Orgs"></a>
<a id="State.Users"></a>
<a id="State.Percentage"></a>

### type State

```go
type State struct {
	// Enabled false turns the flag off for everyone, whatever the other
	// fields say: a kill switch that keeps the targeting for later.
	Enabled bool
	// Default is the answer when no other rule applies.
	Default bool
	// Orgs and Users list IDs that always get false (Deny) or true (Allow).
	// An ID can't be in both lists of the same kind.
	Orgs  Targets
	Users Targets
	// Percentage, from 0 to 100, turns the flag on for that share of
	// subjects (see [Bucket]); nil means no rollout, so Default decides.
	Percentage *int
}
```

State is how a flag decides, in the order of the rules in the package documentation.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store persists flag states in PostgreSQL and keeps a [Registry](#Registry) current. It is an app.Runner: run it so changes from other instances arrive. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStore"></a>

#### func NewStore

```go
func NewStore(ctx context.Context, pool *pgxpool.Pool, reg *Registry, recorder audit.Recorder, opts ...StoreOption) (*Store, error)
```

NewStore loads every stored state into reg and returns the store. After NewStore, reg accepts no new declarations. Changes are recorded as "flags.flag.changed" and "flags.flag.reset" audit events through recorder.

The flags tables come from [Migrations](#Migrations); apply them first.

*Since `v0.1.0`*

**Example**

```go
ctx := context.Background()
var (
	pool     *pgxpool.Pool  // from postgres.Open
	recorder audit.Recorder // for example an auditpg store
)

reg := flags.NewRegistry()
newCheckout := flags.Bool(reg, "checkout.new_flow", flags.Client())

store, err := flags.NewStore(ctx, pool, reg, recorder)
if err != nil {
	log.Fatal(err)
}
// Run store alongside the HTTP server so changes from other instances
// arrive: app.Run(ctx, []app.Runner{server, store}, ...).
_ = store
_ = newCheckout
```

<a id="Store.ClientFlags"></a>

#### func (*Store) ClientFlags

```go
func (s *Store) ClientFlags(ctx context.Context) []Evaluation
```

ClientFlags evaluates every flag declared with [Client](#Client) for the actor in ctx, in declaration order, for endpoints that tell clients which features they get. It reads memory only.

*Since `v0.1.0`*

<a id="Store.DeleteHistoryBefore"></a>

#### func (*Store) DeleteHistoryBefore

```go
func (s *Store) DeleteHistoryBefore(ctx context.Context, before time.Time, limit int) (int64, error)
```

DeleteHistoryBefore deletes up to limit flag changes made before before, oldest first, and returns how many it deleted. Retention calls it until it deletes fewer than limit (ADR-0051).

*Since `v0.1.0`*

<a id="Store.Get"></a>

#### func (*Store) Get

```go
func (s *Store) Get(key string) (View, error)
```

Get returns one flag, or [ErrUnknownFlag](#ErrUnknownFlag).

*Since `v0.1.0`*

<a id="Store.History"></a>

#### func (*Store) History

```go
func (s *Store) History(ctx context.Context, key string, before int64, limit int) ([]HistoryEntry, error)
```

History returns changes to key, newest first. For the next page, pass the last entry's ID as before (0 starts from the newest). limit is clamped to 1–100.

*Since `v0.1.0`*

<a id="Store.List"></a>

#### func (*Store) List

```go
func (s *Store) List() []View
```

List returns every declared flag in declaration order. It reads memory only.

*Since `v0.1.0`*

<a id="Store.OldestHistory"></a>

#### func (*Store) OldestHistory

```go
func (s *Store) OldestHistory(ctx context.Context) (oldest time.Time, ok bool, err error)
```

OldestHistory returns when the oldest recorded flag change was made; ok is false when there are none.

*Since `v0.1.0`*

<a id="Store.Reload"></a>

#### func (*Store) Reload

```go
func (s *Store) Reload(ctx context.Context) error
```

Reload reads every stored state from the database.

*Since `v0.1.0`*

<a id="Store.Reset"></a>

#### func (*Store) Reset

```go
func (s *Store) Reset(ctx context.Context, key string, change Change) (View, error)
```

Reset returns key to its declared state. It returns the same errors as [Store.Set](#Store.Set), except for invalid states.

*Since `v0.1.0`*

<a id="Store.Run"></a>

#### func (*Store) Run

```go
func (s *Store) Run(ctx context.Context) error
```

Run keeps the registry current until ctx is done, then returns nil. It listens on a dedicated connection for changes committed by any instance, reloads everything after connecting (changes made while disconnected send no notification), and reloads everything every resync interval as a fallback. Connection failures are logged and retried with backoff.

*Since `v0.1.0`*

<a id="Store.Set"></a>

#### func (*Store) Set

```go
func (s *Store) Set(ctx context.Context, key string, state State, change Change) (View, error)
```

Set validates state and stores it for key. The change applies to this instance immediately and to others within moments. It returns [ErrUnknownFlag](#ErrUnknownFlag), an [\*InvalidStateError](#InvalidStateError), [ErrReasonRequired](#ErrReasonRequired), [ErrActorRequired](#ErrActorRequired) or [ErrVersionConflict](#ErrVersionConflict). Setting the current state again changes nothing. Lists are stored sorted and without duplicates.

*Since `v0.1.0`*

<a id="Store.UnknownKeys"></a>

#### func (*Store) UnknownKeys

```go
func (s *Store) UnknownKeys() []string
```

UnknownKeys returns stored keys that no declaration matches, for example flags removed from the code. Their rows are kept and ignored.

*Since `v0.1.0`*

<a id="StoreOption"></a>

### type StoreOption

```go
type StoreOption interface {
	// contains filtered or unexported methods
}
```

A StoreOption configures [NewStore](#NewStore).

*Since `v0.1.0`*

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) StoreOption
```

WithLogger sets the logger for listener and invalid-state warnings. Default: discard.

*Since `v0.1.0`*

<a id="WithResyncInterval"></a>

#### func WithResyncInterval

```go
func WithResyncInterval(d time.Duration) StoreOption
```

WithResyncInterval sets how often [Store.Run](#Store.Run) reloads every state. Default: [DefaultResyncInterval](#DefaultResyncInterval).

*Since `v0.1.0`*

<a id="Targets"></a>
<a id="Targets.Allow"></a>
<a id="Targets.Deny"></a>

### type Targets

```go
type Targets struct {
	Allow []string
	Deny  []string
}
```

Targets are IDs that get a fixed answer.

*Since `v0.1.0`*

<a id="View"></a>
<a id="View.Key"></a>
<a id="View.Group"></a>
<a id="View.Description"></a>
<a id="View.Client"></a>
<a id="View.State"></a>
<a id="View.Default"></a>
<a id="View.Modified"></a>
<a id="View.InvalidStoredValue"></a>
<a id="View.Version"></a>
<a id="View.UpdatedAt"></a>
<a id="View.UpdatedBy"></a>

### type View

```go
type View struct {
	Key         string
	Group       string
	Description string
	// Client reports a flag declared with [Client].
	Client bool

	// State is the state in effect; Default is the declared state.
	State   State
	Default State
	// Modified reports whether a valid stored state replaces the declared
	// one.
	Modified bool
	// InvalidStoredValue reports a stored state that fails validation, so
	// the declared state is in effect.
	InvalidStoredValue bool

	// Version increases with every change; pass it back to change the flag.
	// It is 0 for a flag that has never been changed.
	Version   int64
	UpdatedAt time.Time
	UpdatedBy string
}
```

View is a flag's declaration and current state, for operator APIs.

*Since `v0.1.0`*
