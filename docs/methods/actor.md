# actor

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/actor"
```

Package actor records who is performing an operation, in a [context.Context](https://pkg.go.dev/context#Context). It knows nothing about how the actor was authenticated; authentication middleware sets the actor, and audit, jobs and use cases read it (ADR-0030).

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Variables: [`ErrUnauthenticated`](#ErrUnauthenticated), [`ErrForbidden`](#ErrForbidden), [`ErrStepUpRequired`](#ErrStepUpRequired), [`Anonymous`](#Anonymous)
- Functions: [`Require`](#Require), [`With`](#With), [`WithClient`](#WithClient)
- Types:
  - [`Actor`](#Actor): [`From`](#From), [`FromOrAnonymous`](#FromOrAnonymous), [`System`](#System), [`Actor.Can`](#Actor.Can)
  - [`Client`](#Client): [`ClientFrom`](#ClientFrom)
  - [`Kind`](#Kind): [`KindUser`](#KindUser), [`KindService`](#KindService), [`KindSystem`](#KindSystem), [`KindAnonymous`](#KindAnonymous)

## Variables

<a id="ErrUnauthenticated"></a>
<a id="ErrForbidden"></a>
<a id="ErrStepUpRequired"></a>

```go
var (
	// ErrUnauthenticated reports an operation without an actor, or with
	// the anonymous actor.
	ErrUnauthenticated = errors.New("actor: authentication required")
	// ErrForbidden reports an actor without the permission.
	ErrForbidden = errors.New("actor: permission denied")
	// ErrStepUpRequired reports an actor whose roles grant the permission
	// only after a stronger sign-in, such as two-factor authentication.
	ErrStepUpRequired = errors.New("actor: stronger sign-in required")
)
```

Errors returned by [Require](#Require). Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

<a id="Anonymous"></a>

```go
var Anonymous = Actor{Kind: KindAnonymous}
```

Anonymous is the actor for unauthenticated operations.

*Since `v0.1.0`*

## Functions

<a id="Require"></a>

### func Require

```go
func Require(ctx context.Context, permission string) error
```

Require checks that the actor in ctx holds permission. It returns [ErrUnauthenticated](#ErrUnauthenticated) without an actor, [ErrStepUpRequired](#ErrStepUpRequired) when the permission is in the actor's StepUp, and [ErrForbidden](#ErrForbidden) otherwise.

*Since `v0.1.0`*

<a id="With"></a>

### func With

```go
func With(ctx context.Context, a Actor) context.Context
```

With returns a copy of ctx carrying a. The permission slices are copied.

*Since `v0.1.0`*

<a id="WithClient"></a>

### func WithClient

```go
func WithClient(ctx context.Context, c Client) context.Context
```

WithClient returns a copy of ctx carrying c. HTTP middleware that knows the client's address, after trusted-proxy handling, sets it once per request; modules/auth's Middleware does. Values are stored as given: recorders bound and canonicalise them.

*Since `v0.1.0`*

## Types

<a id="Actor"></a>
<a id="Actor.Kind"></a>
<a id="Actor.ID"></a>
<a id="Actor.Label"></a>
<a id="Actor.OrgID"></a>
<a id="Actor.Permissions"></a>
<a id="Actor.StepUp"></a>

### type Actor

```go
type Actor struct {
	Kind Kind
	// ID is the stable identifier (for example a user ID). Empty for anonymous actors.
	ID string
	// Label is a human-readable name captured at the time of the action.
	Label string
	// OrgID is the organisation the actor is acting in, if any.
	OrgID string
	// Permissions are the permissions granted for this operation.
	Permissions []string
	// StepUp are permissions the actor's roles grant but this operation
	// doesn't get until the actor signs in more strongly, for example with
	// two-factor authentication. They are never granted by Can.
	StepUp []string
}
```

An Actor is the identity performing an operation.

*Since `v0.1.0`*

<a id="From"></a>

#### func From

```go
func From(ctx context.Context) (Actor, bool)
```

From returns the actor stored in ctx and whether one was set.

*Since `v0.1.0`*

<a id="FromOrAnonymous"></a>

#### func FromOrAnonymous

```go
func FromOrAnonymous(ctx context.Context) Actor
```

FromOrAnonymous returns the actor stored in ctx, or [Anonymous](#Anonymous).

*Since `v0.1.0`*

<a id="System"></a>

#### func System

```go
func System(name string) Actor
```

System returns an actor for work the application performs itself, such as a scheduled job.

*Since `v0.1.0`*

<a id="Actor.Can"></a>

#### func (Actor) Can

```go
func (a Actor) Can(permission string) bool
```

Can reports whether the actor holds permission.

*Since `v0.1.0`*

<a id="Client"></a>
<a id="Client.IP"></a>
<a id="Client.UserAgent"></a>

### type Client

```go
type Client struct {
	IP        string
	UserAgent string
}
```

Client is the network client an operation arrived from: its IP address and user agent. Audit events recorded during the operation carry them (audit.FromContext).

*Since `v0.1.0`*

<a id="ClientFrom"></a>

#### func ClientFrom

```go
func ClientFrom(ctx context.Context) (Client, bool)
```

ClientFrom returns the client stored in ctx and whether one was set.

*Since `v0.1.0`*

<a id="Kind"></a>

### type Kind

```go
type Kind string
```

Kind classifies an actor.

*Since `v0.1.0`*

<a id="KindUser"></a>
<a id="KindService"></a>
<a id="KindSystem"></a>
<a id="KindAnonymous"></a>

```go
const (
	KindUser      Kind = "user"
	KindService   Kind = "service"
	KindSystem    Kind = "system"
	KindAnonymous Kind = "anonymous"
)
```

Actor kinds.

*Since `v0.1.0`*
