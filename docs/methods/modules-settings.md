# modules/settings

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/settings"
```

Package settings provides runtime settings: non-secret tunables declared in Go with a default and bounds, stored in PostgreSQL only when changed, and applied on every instance without a restart (ADR-0031).

Secrets and infrastructure (database URL, API keys, listen addresses) stay in the environment (ADR-0020); they can't be declared here.

```go
reg := settings.NewRegistry()
codeTTL := settings.Duration(reg, "auth.verification_code_ttl", 15*time.Minute,
	settings.Describe("How long email verification codes stay valid."),
	settings.Range(5*time.Minute, time.Hour),
	settings.ReasonRequired(),
)
store, err := settings.NewStore(ctx, pool, reg, recorder)
// Run store with the app's runners; pass codeTTL, a config.Value, to modules.
```

A [Store](#Store) loads every stored value at startup, applies changes made through [Store.Set](#Store.Set) and [Store.Reset](#Store.Reset) immediately, and learns about changes made by other instances through PostgreSQL LISTEN/NOTIFY, with a periodic full reload as a fallback.

Settings declared with [OrgOverridable](#OrgOverridable) also accept a value per organisation (ADR-0056). [Setting.Get](#Setting.Get) returns the organisation's value when the context's actor acts in one (actor.Actor.OrgID, set by orgs.RequireMember), and the platform value otherwise.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultMaxItems`](#DefaultMaxItems), [`DefaultResyncInterval`](#DefaultResyncInterval)
- Variables: [`ErrUnknownSetting`](#ErrUnknownSetting), [`ErrVersionConflict`](#ErrVersionConflict), [`ErrReasonRequired`](#ErrReasonRequired), [`ErrActorRequired`](#ErrActorRequired), [`ErrNotOrgOverridable`](#ErrNotOrgOverridable), [`ErrInvalidOrgID`](#ErrInvalidOrgID), [`ErrInvalidValue`](#ErrInvalidValue), [`Migrations`](#Migrations)
- Types:
  - [`Change`](#Change)
  - [`HistoryEntry`](#HistoryEntry)
  - [`InvalidValueError`](#InvalidValueError): [`InvalidValueError.Error`](#InvalidValueError.Error), [`InvalidValueError.Unwrap`](#InvalidValueError.Unwrap)
  - [`Kind`](#Kind): [`KindBool`](#KindBool), [`KindInt`](#KindInt), [`KindFloat`](#KindFloat), [`KindString`](#KindString), [`KindEnum`](#KindEnum), [`KindDuration`](#KindDuration), [`KindStringList`](#KindStringList)
  - [`Option`](#Option): [`Describe`](#Describe), [`Email`](#Email), [`Group`](#Group), [`MaxItems`](#MaxItems), [`MaxLen`](#MaxLen), [`OneOf`](#OneOf), [`OrgOverridable`](#OrgOverridable), [`Range`](#Range), [`ReasonRequired`](#ReasonRequired), [`RestartRequired`](#RestartRequired), [`URL`](#URL), [`Validate`](#Validate)
  - [`Registry`](#Registry): [`NewRegistry`](#NewRegistry), [`Registry.Keys`](#Registry.Keys)
  - [`Setting`](#Setting): [`Bool`](#Bool), [`Duration`](#Duration), [`Enum`](#Enum), [`Float`](#Float), [`Int`](#Int), [`String`](#String), [`StringList`](#StringList), [`Setting.Get`](#Setting.Get), [`Setting.Key`](#Setting.Key)
  - [`Store`](#Store): [`NewStore`](#NewStore), [`Store.DeleteHistoryBefore`](#Store.DeleteHistoryBefore), [`Store.Get`](#Store.Get), [`Store.GetForOrg`](#Store.GetForOrg), [`Store.History`](#Store.History), [`Store.HistoryForOrg`](#Store.HistoryForOrg), [`Store.List`](#Store.List), [`Store.ListForOrg`](#Store.ListForOrg), [`Store.OldestHistory`](#Store.OldestHistory), [`Store.Overrides`](#Store.Overrides), [`Store.Reload`](#Store.Reload), [`Store.Reset`](#Store.Reset), [`Store.ResetForOrg`](#Store.ResetForOrg), [`Store.Run`](#Store.Run), [`Store.Set`](#Store.Set), [`Store.SetForOrg`](#Store.SetForOrg), [`Store.UnknownKeys`](#Store.UnknownKeys)
  - [`StoreOption`](#StoreOption): [`WithLogger`](#WithLogger), [`WithResyncInterval`](#WithResyncInterval)
  - [`View`](#View)

## Constants

<a id="DefaultMaxItems"></a>

```go
const DefaultMaxItems = 100
```

DefaultMaxItems limits a StringList setting declared without [MaxItems](#MaxItems), so a list is never bounded only by the request body limit.

*Since `v0.1.0`*

<a id="DefaultResyncInterval"></a>

```go
const DefaultResyncInterval = 5 * time.Minute
```

DefaultResyncInterval is how often a running [Store](#Store) reloads every value, covering notifications lost while disconnected.

*Since `v0.1.0`*

## Variables

<a id="ErrUnknownSetting"></a>
<a id="ErrVersionConflict"></a>
<a id="ErrReasonRequired"></a>
<a id="ErrActorRequired"></a>
<a id="ErrNotOrgOverridable"></a>
<a id="ErrInvalidOrgID"></a>
<a id="ErrInvalidValue"></a>

```go
var (
	// ErrUnknownSetting reports a key that isn't declared in the registry.
	ErrUnknownSetting = errors.New("settings: unknown setting")

	// ErrVersionConflict reports that the setting changed after the caller
	// read it. Read it again and retry.
	ErrVersionConflict = errors.New("settings: setting changed since it was read")

	// ErrReasonRequired reports a change without a reason to a setting
	// declared with [ReasonRequired].
	ErrReasonRequired = errors.New("settings: a reason is required to change this setting")

	// ErrActorRequired reports a change without an authenticated actor in
	// the context.
	ErrActorRequired = errors.New("settings: changes require an authenticated actor")

	// ErrNotOrgOverridable reports an organisation value for a setting not
	// declared with [OrgOverridable].
	ErrNotOrgOverridable = errors.New("settings: setting can't be changed per organisation")

	// ErrInvalidOrgID reports an empty or malformed organisation ID.
	ErrInvalidOrgID = errors.New("settings: invalid organisation ID")

	// ErrInvalidValue reports a value that fails decoding or validation.
	// The error is an [*InvalidValueError].
	ErrInvalidValue = errors.New("settings: invalid value")
)
```

Errors returned by [Store](#Store) methods. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the module's goose migrations: the settings\_values and settings\_history tables. Apps copy them into db/migrations (the settings recipe does this); tests can apply them directly with pgtest.

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
	// Reason explains the change; required for ReasonRequired settings.
	Reason string
}
```

Change carries what a caller supplies with a change. The actor comes from the context.

*Since `v0.1.0`*

<a id="HistoryEntry"></a>
<a id="HistoryEntry.ID"></a>
<a id="HistoryEntry.Key"></a>
<a id="HistoryEntry.OrgID"></a>
<a id="HistoryEntry.OldValue"></a>
<a id="HistoryEntry.NewValue"></a>
<a id="HistoryEntry.Version"></a>
<a id="HistoryEntry.Reason"></a>
<a id="HistoryEntry.ActorKind"></a>
<a id="HistoryEntry.ActorID"></a>
<a id="HistoryEntry.RequestID"></a>
<a id="HistoryEntry.ChangedAt"></a>

### type HistoryEntry

```go
type HistoryEntry struct {
	ID  int64
	Key string
	// OrgID is the organisation whose value changed; empty for a
	// platform-wide change.
	OrgID     string
	OldValue  json.RawMessage
	NewValue  json.RawMessage
	Version   int64
	Reason    string
	ActorKind actor.Kind
	ActorID   string
	RequestID string
	ChangedAt time.Time
}
```

HistoryEntry is one change to a setting. A nil value means the default.

*Since `v0.1.0`*

<a id="InvalidValueError"></a>
<a id="InvalidValueError.Key"></a>
<a id="InvalidValueError.Reason"></a>

### type InvalidValueError

```go
type InvalidValueError struct {
	Key    string
	Reason string
}
```

InvalidValueError describes why a value was rejected. Reason never contains the rejected value.

*Since `v0.1.0`*

<a id="InvalidValueError.Error"></a>

#### func (*InvalidValueError) Error

```go
func (e *InvalidValueError) Error() string
```

Error names the setting key and the reason the value was rejected.

*Since `v0.1.0`*

<a id="InvalidValueError.Unwrap"></a>

#### func (*InvalidValueError) Unwrap

```go
func (e *InvalidValueError) Unwrap() error
```

Unwrap returns [ErrInvalidValue](#ErrInvalidValue).

*Since `v0.1.0`*

<a id="Kind"></a>

### type Kind

```go
type Kind string
```

Kind is a setting's value type.

*Since `v0.1.0`*

<a id="KindBool"></a>
<a id="KindInt"></a>
<a id="KindFloat"></a>
<a id="KindString"></a>
<a id="KindEnum"></a>
<a id="KindDuration"></a>
<a id="KindStringList"></a>

```go
const (
	KindBool       Kind = "bool"
	KindInt        Kind = "int"
	KindFloat      Kind = "float"
	KindString     Kind = "string"
	KindEnum       Kind = "enum"
	KindDuration   Kind = "duration"
	KindStringList Kind = "string_list"
)
```

Setting kinds.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures a setting declaration. An option that doesn't fit the setting's kind makes the declaration panic at startup.

*Since `v0.1.0`*

<a id="Describe"></a>

#### func Describe

```go
func Describe(text string) Option
```

Describe sets the help text shown to operators.

*Since `v0.1.0`*

<a id="Email"></a>

#### func Email

```go
func Email() Option
```

Email requires a String setting, or each item of a StringList, to be a bare email address such as no-reply@example.com.

*Since `v0.1.0`*

<a id="Group"></a>

#### func Group

```go
func Group(name string) Option
```

Group sets the group used to organise settings in listings. Default: the key's first segment.

*Since `v0.1.0`*

<a id="MaxItems"></a>

#### func MaxItems

```go
func MaxItems(n int) Option
```

MaxItems limits a StringList setting to n items. Without it, a StringList setting is limited to [DefaultMaxItems](#DefaultMaxItems).

*Since `v0.1.0`*

<a id="MaxLen"></a>

#### func MaxLen

```go
func MaxLen(n int) Option
```

MaxLen limits a String setting, or each item of a StringList, to n characters.

*Since `v0.1.0`*

<a id="OneOf"></a>

#### func OneOf

```go
func OneOf(values ...string) Option
```

OneOf limits a String setting, or each item of a StringList, to values.

*Since `v0.1.0`*

<a id="OrgOverridable"></a>

#### func OrgOverridable

```go
func OrgOverridable() Option
```

OrgOverridable lets each organisation have its own value, within the same validation, through [Store.SetForOrg](#Store.SetForOrg) (ADR-0056). [Setting.Get](#Setting.Get) returns it when the context's actor acts in that organisation. Only mark settings an organisation may choose for itself: never security-relevant ones such as sign-in, rate limits, retention, maintenance or email senders. It can't be combined with [RestartRequired](#RestartRequired).

*Since `v0.1.0`*

**Example**

```go
reg := settings.NewRegistry()
invitationTTL := settings.Duration(reg, "orgs.invitation_ttl", 7*24*time.Hour,
	settings.Range(24*time.Hour, 30*24*time.Hour),
	settings.OrgOverridable(),
)

// orgs.RequireMember returns a context whose actor acts in the
// organisation. Get returns the organisation's own value, set with
// Store.SetForOrg, when it has one, and the platform value otherwise.
ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1", OrgID: "org_1"})
fmt.Println(invitationTTL.Get(ctx))
```

Output:

```text
168h0m0s
```

<a id="Range"></a>

#### func Range

```go
func Range[T int | float64 | time.Duration](lo, hi T) Option
```

Range limits an Int, Float or Duration setting to \[lo, hi]. The bounds' type must match the setting: Range(0.0, 2.0) for a Float.

*Since `v0.1.0`*

<a id="ReasonRequired"></a>

#### func ReasonRequired

```go
func ReasonRequired() Option
```

ReasonRequired makes every change to the setting require a reason, recorded in its history. Use it for security-relevant settings.

*Since `v0.1.0`*

<a id="RestartRequired"></a>

#### func RestartRequired

```go
func RestartRequired() Option
```

RestartRequired makes [Setting.Get](#Setting.Get) return the value loaded at startup. Changes are stored immediately and take effect when instances restart.

*Since `v0.1.0`*

<a id="URL"></a>

#### func URL

```go
func URL() Option
```

URL requires a String setting, or each item of a StringList, to be an absolute http or https URL.

*Since `v0.1.0`*

<a id="Validate"></a>

#### func Validate

```go
func Validate[T any](fn func(T) error) Option
```

Validate adds a custom check. fn's parameter type must match the setting: func(time.Duration) error for a Duration. Error messages are shown to operators and must not include the value.

*Since `v0.1.0`*

<a id="Registry"></a>

### type Registry

```go
type Registry struct {
	// contains filtered or unexported fields
}
```

A Registry holds declared settings and their current values. Declare every setting before calling [NewStore](#NewStore). A Registry is safe for concurrent use.

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

Keys returns the key of every declared setting, in declaration order. Setting keys are public API (ADR-0015); apps record them in their surface inventory (ADR-0054).

*Since `v0.1.0`*

<a id="Setting"></a>

### type Setting

```go
type Setting[T any] struct {
	// contains filtered or unexported fields
}
```

A Setting is a typed handle to one declared setting. It implements [config.Value](config.md#Value), so library modules can accept it for live options.

*Since `v0.1.0`*

<a id="Bool"></a>

#### func Bool

```go
func Bool(r *Registry, key string, def bool, opts ...Option) *Setting[bool]
```

Bool declares a true/false setting.

*Since `v0.1.0`*

<a id="Duration"></a>

#### func Duration

```go
func Duration(r *Registry, key string, def time.Duration, opts ...Option) *Setting[time.Duration]
```

Duration declares a duration setting, stored and edited as a Go duration string such as "15m" or "1h30m". Combine with [Range](#Range).

*Since `v0.1.0`*

**Example**

```go
reg := settings.NewRegistry()
codeTTL := settings.Duration(reg, "auth.verification_code_ttl", 15*time.Minute,
	settings.Describe("How long email verification codes stay valid."),
	settings.Range(5*time.Minute, time.Hour),
	settings.ReasonRequired(),
)

// Until a store loads a changed value, Get returns the default.
fmt.Println(codeTTL.Get(context.Background()))
```

Output:

```text
15m0s
```

<a id="Enum"></a>

#### func Enum

```go
func Enum(r *Registry, key string, def string, allowed []string, opts ...Option) *Setting[string]
```

Enum declares a text setting limited to allowed values.

*Since `v0.1.0`*

<a id="Float"></a>

#### func Float

```go
func Float(r *Registry, key string, def float64, opts ...Option) *Setting[float64]
```

Float declares a decimal setting. Combine with [Range](#Range), passing float bounds such as Range(0.0, 2.0).

*Since `v0.1.0`*

<a id="Int"></a>

#### func Int

```go
func Int(r *Registry, key string, def int, opts ...Option) *Setting[int]
```

Int declares a whole-number setting. Combine with [Range](#Range).

*Since `v0.1.0`*

<a id="String"></a>

#### func String

```go
func String(r *Registry, key string, def string, opts ...Option) *Setting[string]
```

String declares a text setting. Combine with [MaxLen](#MaxLen), [URL](#URL) or [Email](#Email).

*Since `v0.1.0`*

<a id="StringList"></a>

#### func StringList

```go
func StringList(r *Registry, key string, def []string, opts ...Option) *Setting[[]string]
```

StringList declares a list-of-text setting, such as allowed origins. Get returns a copy the caller may modify. Combine with [MaxItems](#MaxItems); the default limit is [DefaultMaxItems](#DefaultMaxItems).

*Since `v0.1.0`*

<a id="Setting.Get"></a>

#### func (*Setting[T]) Get

```go
func (s *Setting[T]) Get(ctx context.Context) T
```

Get returns the setting's current value. It returns the default while the setting is unchanged, when its stored value fails validation, and before a store has loaded. Settings declared with [RestartRequired](#RestartRequired) keep the value loaded at startup. For a setting declared with [OrgOverridable](#OrgOverridable), when the actor in ctx acts in an organisation that has a valid value of its own, Get returns that value. Get never touches the database.

*Since `v0.1.0`*

<a id="Setting.Key"></a>

#### func (*Setting[T]) Key

```go
func (s *Setting[T]) Key() string
```

Key returns the setting's key.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store persists settings in PostgreSQL and keeps a [Registry](#Registry) current. It is an app.Runner: run it so changes from other instances arrive. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStore"></a>

#### func NewStore

```go
func NewStore(ctx context.Context, pool *pgxpool.Pool, reg *Registry, recorder audit.Recorder, opts ...StoreOption) (*Store, error)
```

NewStore loads every stored value into reg and returns the store. After NewStore, reg accepts no new declarations. Changes are recorded as "settings.value.changed" audit events through recorder.

The settings tables come from [Migrations](#Migrations); apply them first.

*Since `v0.1.0`*

**Example**

```go
ctx := context.Background()
var (
	pool     *pgxpool.Pool  // from postgres.Open
	recorder audit.Recorder // for example an auditpg store
)

reg := settings.NewRegistry()
maintenance := settings.Bool(reg, "ops.maintenance_mode", false,
	settings.Describe("Return 503 for all non-ops routes."),
	settings.ReasonRequired(),
)

store, err := settings.NewStore(ctx, pool, reg, recorder)
if err != nil {
	log.Fatal(err)
}
// Run store alongside the HTTP server so changes from other instances
// arrive: app.Run(ctx, []app.Runner{server, store}, ...).
_ = store
_ = maintenance
```

<a id="Store.DeleteHistoryBefore"></a>

#### func (*Store) DeleteHistoryBefore

```go
func (s *Store) DeleteHistoryBefore(ctx context.Context, before time.Time, limit int) (int64, error)
```

DeleteHistoryBefore deletes up to limit setting changes made before before, oldest first, and returns how many it deleted. Retention calls it until it deletes fewer than limit (ADR-0051).

*Since `v0.1.0`*

<a id="Store.Get"></a>

#### func (*Store) Get

```go
func (s *Store) Get(key string) (View, error)
```

Get returns one setting, or [ErrUnknownSetting](#ErrUnknownSetting).

*Since `v0.1.0`*

<a id="Store.GetForOrg"></a>

#### func (*Store) GetForOrg

```go
func (s *Store) GetForOrg(orgID, key string) (View, error)
```

GetForOrg returns one setting as organisation orgID sees it, or [ErrInvalidOrgID](#ErrInvalidOrgID), [ErrUnknownSetting](#ErrUnknownSetting) or [ErrNotOrgOverridable](#ErrNotOrgOverridable).

*Since `v0.1.0`*

<a id="Store.History"></a>

#### func (*Store) History

```go
func (s *Store) History(ctx context.Context, key string, before int64, limit int) ([]HistoryEntry, error)
```

History returns changes to key, newest first. For the next page, pass the last entry's ID as before (0 starts from the newest). limit is clamped to 1–100.

*Since `v0.1.0`*

<a id="Store.HistoryForOrg"></a>

#### func (*Store) HistoryForOrg

```go
func (s *Store) HistoryForOrg(ctx context.Context, orgID, key string, before int64, limit int) ([]HistoryEntry, error)
```

HistoryForOrg returns changes to organisation orgID's value of key, newest first, paged like [Store.History](#Store.History).

*Since `v0.1.0`*

<a id="Store.List"></a>

#### func (*Store) List

```go
func (s *Store) List() []View
```

List returns every declared setting in declaration order. It reads memory only.

*Since `v0.1.0`*

<a id="Store.ListForOrg"></a>

#### func (*Store) ListForOrg

```go
func (s *Store) ListForOrg(orgID string) ([]View, error)
```

ListForOrg returns the settings declared [OrgOverridable](#OrgOverridable), in declaration order, as organisation orgID sees them: Value is the organisation's own value when it has a valid one, else PlatformValue. It reads memory only. It returns [ErrInvalidOrgID](#ErrInvalidOrgID) for an empty or malformed ID; checking that the organisation exists, and that the caller may see it, is the caller's job.

*Since `v0.1.0`*

<a id="Store.OldestHistory"></a>

#### func (*Store) OldestHistory

```go
func (s *Store) OldestHistory(ctx context.Context) (oldest time.Time, ok bool, err error)
```

OldestHistory returns when the oldest recorded change was made; ok is false when there are none.

*Since `v0.1.0`*

<a id="Store.Overrides"></a>

#### func (*Store) Overrides

```go
func (s *Store) Overrides(ctx context.Context, key, after string, limit int) ([]View, error)
```

Overrides returns the organisations that have their own value of key, by organisation ID, as views for each organisation. For the next page, pass the last view's OrgID as after ("" starts from the first). limit is clamped to 1–100. A setting not declared [OrgOverridable](#OrgOverridable) has none; an undeclared key returns [ErrUnknownSetting](#ErrUnknownSetting).

*Since `v0.1.0`*

<a id="Store.Reload"></a>

#### func (*Store) Reload

```go
func (s *Store) Reload(ctx context.Context) error
```

Reload reads every stored value from the database, organisation values included.

*Since `v0.1.0`*

<a id="Store.Reset"></a>

#### func (*Store) Reset

```go
func (s *Store) Reset(ctx context.Context, key string, change Change) (View, error)
```

Reset returns key to its default. It returns the same errors as [Store.Set](#Store.Set), except for invalid values.

*Since `v0.1.0`*

<a id="Store.ResetForOrg"></a>

#### func (*Store) ResetForOrg

```go
func (s *Store) ResetForOrg(ctx context.Context, orgID, key string, change Change) (View, error)
```

ResetForOrg removes organisation orgID's own value of key, so the organisation gets the platform value again. It returns the same errors as [Store.SetForOrg](#Store.SetForOrg), except for invalid values.

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
func (s *Store) Set(ctx context.Context, key string, value json.RawMessage, change Change) (View, error)
```

Set validates value (JSON, such as \`"30m"\` or \`42\`) and stores it for key. The change applies to this instance immediately and to others within moments. It returns [ErrUnknownSetting](#ErrUnknownSetting), an [\*InvalidValueError](#InvalidValueError), [ErrReasonRequired](#ErrReasonRequired), [ErrActorRequired](#ErrActorRequired) or [ErrVersionConflict](#ErrVersionConflict). Setting the current value again changes nothing.

*Since `v0.1.0`*

<a id="Store.SetForOrg"></a>

#### func (*Store) SetForOrg

```go
func (s *Store) SetForOrg(ctx context.Context, orgID, key string, value json.RawMessage, change Change) (View, error)
```

SetForOrg validates value like [Store.Set](#Store.Set) and stores it as organisation orgID's own value of key, a setting declared [OrgOverridable](#OrgOverridable). Pass the organisation view's Version. The change is recorded in the organisation's history and as a "settings.value.changed" audit event carrying the organisation, and applies on every instance within moments.

It returns the errors of [Store.Set](#Store.Set), [ErrInvalidOrgID](#ErrInvalidOrgID) and [ErrNotOrgOverridable](#ErrNotOrgOverridable). The organisation must exist; checking that the caller may change its settings is the caller's job.

*Since `v0.1.0`*

<a id="Store.UnknownKeys"></a>

#### func (*Store) UnknownKeys

```go
func (s *Store) UnknownKeys() []string
```

UnknownKeys returns stored keys that no declaration matches, for example settings removed from the code. Their rows are kept and ignored.

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

WithLogger sets the logger for listener and invalid-value warnings. Default: discard.

*Since `v0.1.0`*

<a id="WithResyncInterval"></a>

#### func WithResyncInterval

```go
func WithResyncInterval(d time.Duration) StoreOption
```

WithResyncInterval sets how often [Store.Run](#Store.Run) reloads every value. Default: [DefaultResyncInterval](#DefaultResyncInterval).

*Since `v0.1.0`*

<a id="View"></a>
<a id="View.Key"></a>
<a id="View.Kind"></a>
<a id="View.Group"></a>
<a id="View.Description"></a>
<a id="View.Value"></a>
<a id="View.Default"></a>
<a id="View.Modified"></a>
<a id="View.InvalidStoredValue"></a>
<a id="View.Version"></a>
<a id="View.UpdatedAt"></a>
<a id="View.UpdatedBy"></a>
<a id="View.ReasonRequired"></a>
<a id="View.RestartRequired"></a>
<a id="View.RestartPending"></a>
<a id="View.Constraints"></a>
<a id="View.OrgOverridable"></a>
<a id="View.OrgID"></a>
<a id="View.PlatformValue"></a>

### type View

```go
type View struct {
	Key         string
	Kind        Kind
	Group       string
	Description string

	// Value is the effective value as JSON; Default is the declared default.
	Value   json.RawMessage
	Default json.RawMessage
	// Modified reports whether a valid stored value overrides the default,
	// or, in a view for an organisation, the platform value.
	Modified bool
	// InvalidStoredValue reports a stored value that fails validation, so
	// the default is in effect.
	InvalidStoredValue bool

	// Version increases with every change; pass it back to change the
	// setting. It is 0 for a setting that has never been changed. In a view
	// for an organisation, it is the version of the organisation's value.
	Version   int64
	UpdatedAt time.Time
	UpdatedBy string

	ReasonRequired  bool
	RestartRequired bool
	// RestartPending reports a restart-required setting changed since this
	// instance started.
	RestartPending bool

	// Constraints summarises validation, such as min, max, one_of, max_len,
	// max_items and format. Treat it as read-only.
	Constraints map[string]any

	// OrgOverridable reports a setting declared with [OrgOverridable].
	OrgOverridable bool
	// OrgID is the organisation of a view returned by [Store.ListForOrg],
	// [Store.GetForOrg], [Store.SetForOrg], [Store.ResetForOrg] or
	// [Store.Overrides]; empty for the platform-wide value.
	OrgID string
	// PlatformValue, in a view for an organisation, is the value the
	// organisation gets without its own: the effective platform-wide value.
	PlatformValue json.RawMessage
}
```

View is a setting's declaration and current state, for operator APIs.

*Since `v0.1.0`*
