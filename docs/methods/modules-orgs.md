# modules/orgs

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/orgs"
```

Package orgs holds the building blocks for organisations in multi-tenant apps (ADR-0023, ADR-0048): organisation IDs, the membership check every organisation operation starts with, and invitation emails. The generated app owns the flows, tables and SQL in internal/modules/orgs.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`RoleOwner`](#RoleOwner), [`RoleAdmin`](#RoleAdmin), [`RoleMember`](#RoleMember)
- Variables: [`ErrOrgNotFound`](#ErrOrgNotFound), [`ErrNotMember`](#ErrNotMember), [`ErrInvalidID`](#ErrInvalidID)
- Functions: [`Authorize`](#Authorize)
- Types:
  - [`Emails`](#Emails): [`NewBrandedEmails`](#NewBrandedEmails), [`NewMailEmails`](#NewMailEmails)
  - [`ID`](#ID): [`NewID`](#NewID), [`ParseID`](#ParseID), [`ID.String`](#ID.String)
  - [`Invitation`](#Invitation)
  - [`Member`](#Member): [`RequireMember`](#RequireMember)
  - [`Memberships`](#Memberships)

## Constants

<a id="RoleOwner"></a>
<a id="RoleAdmin"></a>
<a id="RoleMember"></a>

```go
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)
```

The organisation roles every app declares in its org catalog. Apps may add their own. Role names are public API.

*Since `v0.1.0`*

## Variables

<a id="ErrOrgNotFound"></a>
<a id="ErrNotMember"></a>
<a id="ErrInvalidID"></a>

```go
var (
	// ErrOrgNotFound reports an organisation that doesn't exist, is deleted,
	// or that the caller isn't a member of. The three look the same, so
	// organisation IDs can't be probed.
	ErrOrgNotFound = errors.New("orgs: organisation not found")
	// ErrNotMember is returned by [Memberships] when the user isn't a
	// member of a live organisation. [RequireMember] turns it into
	// [ErrOrgNotFound].
	ErrNotMember = errors.New("orgs: not a member")
	// ErrInvalidID reports a string that isn't an organisation ID.
	ErrInvalidID = errors.New("orgs: invalid organisation ID")
)
```

Errors. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

## Functions

<a id="Authorize"></a>

### func Authorize

```go
func Authorize(ctx context.Context, catalog *auth.Catalog, member Member, permission string) (context.Context, error)
```

Authorize is [RequireMember](#RequireMember) for a membership the caller has already read, such as one in a deleted organisation, which [Memberships](#Memberships) doesn't return: it checks that member is the signed-in user and that their role grants permission, with the same two-factor step-up, and returns a context whose actor acts in member's organisation. Every permission check on an organisation goes through it, so roles that require two-factor authentication can't be bypassed by a path that reads the role itself.

For a request authenticated with an API key, the role's permissions are limited by [auth.Principal.Restrict](modules-auth.md#Principal.Restrict): never those of roles that require two-factor authentication, and only the key's scopes. A service account's key reaches only the organisation the service account belongs to.

It returns [actor.ErrUnauthenticated](actor.md#ErrUnauthenticated) without a signed-in user, [ErrOrgNotFound](#ErrOrgNotFound) when member belongs to another user or has an invalid organisation ID, or a service account's key is used for another organisation, [actor.ErrStepUpRequired](actor.md#ErrStepUpRequired) when the role grants permission only to sessions verified with a second factor, and [actor.ErrForbidden](actor.md#ErrForbidden) when it doesn't grant it. The returned context is usable, with the role's permissions, even when the error is ErrStepUpRequired or ErrForbidden.

*Since `v0.1.0`*

## Types

<a id="Emails"></a>
<a id="Emails.SendInvitation"></a>

### type Emails

```go
type Emails interface {
	// SendInvitation invites to to join an organisation.
	SendInvitation(ctx context.Context, to string, inv Invitation) error
}
```

Emails sends the emails organisations need. Apps use [NewBrandedEmails](#NewBrandedEmails) or their own templates. Implementations should queue rather than deliver inline (jobs.AsyncSender), so requests don't wait for the provider.

*Since `v0.1.0`*

<a id="NewBrandedEmails"></a>

#### func NewBrandedEmails

```go
func NewBrandedEmails(sender mail.Sender, brand mail.Brand) Emails
```

NewBrandedEmails returns emails in brand's layout ([mail.Brand.Render](mail.md#Brand.Render)) sent through sender, which sets the From address (mail.WithDefaults). brand.Name appears in subjects and bodies.

*Since `v0.1.0`*

<a id="NewMailEmails"></a>

#### func NewMailEmails

```go
func NewMailEmails(sender mail.Sender, appName string) Emails
```

NewMailEmails returns the emails of [NewBrandedEmails](#NewBrandedEmails) with appName as the whole brand: no link, logo or support address.

*Since `v0.1.0`*

<a id="ID"></a>

### type ID

```go
type ID string
```

ID identifies an organisation, such as org\_2x7…. It is a distinct type so a user ID can't be passed where an organisation ID is expected.

*Since `v0.1.0`*

<a id="NewID"></a>

#### func NewID

```go
func NewID() ID
```

NewID returns a random organisation ID with 128 bits of randomness.

*Since `v0.1.0`*

<a id="ParseID"></a>

#### func ParseID

```go
func ParseID(s string) (ID, error)
```

ParseID returns s as an ID, or [ErrInvalidID](#ErrInvalidID) when s isn't shaped like one. It doesn't check that the organisation exists.

*Since `v0.1.0`*

<a id="ID.String"></a>

#### func (ID) String

```go
func (id ID) String() string
```

String returns the ID as stored.

*Since `v0.1.0`*

<a id="Invitation"></a>
<a id="Invitation.OrgName"></a>
<a id="Invitation.InvitedBy"></a>
<a id="Invitation.Role"></a>
<a id="Invitation.URL"></a>
<a id="Invitation.ExpiresIn"></a>

### type Invitation

```go
type Invitation struct {
	// OrgName is the organisation's name and InvitedBy who sent the
	// invitation (a name or email address).
	OrgName   string
	InvitedBy string
	// Role is the role the invited person gets.
	Role string
	// URL opens the invitation in the app's frontend; it carries the token.
	URL string
	// ExpiresIn is how long the invitation stays valid.
	ExpiresIn time.Duration
}
```

Invitation is what an invitation email says.

*Since `v0.1.0`*

<a id="Member"></a>
<a id="Member.OrgID"></a>
<a id="Member.UserID"></a>
<a id="Member.Role"></a>

### type Member

```go
type Member struct {
	OrgID  ID
	UserID string
	Role   string
}
```

Member is a user's membership in an organisation, or an organisation service account's role in its organisation (ADR-0058): then UserID is the service account's ID.

*Since `v0.1.0`*

<a id="RequireMember"></a>

#### func RequireMember

```go
func RequireMember(ctx context.Context, m Memberships, catalog *auth.Catalog, orgID ID, permission string) (context.Context, Member, error)
```

RequireMember checks that the signed-in user is a member of orgID whose role grants permission, and returns a context whose actor acts in that organisation: OrgID is set and Permissions are the role's permissions in catalog, replacing platform permissions for the operation. Every organisation operation calls it first (ADR-0048).

A service account's API key acts like a member when m answers for its ID (ADR-0058); see [Authorize](#Authorize) for what it may reach.

It returns [actor.ErrUnauthenticated](actor.md#ErrUnauthenticated) without a signed-in user, [ErrOrgNotFound](#ErrOrgNotFound) when orgID isn't one of the user's live organisations, [actor.ErrStepUpRequired](actor.md#ErrStepUpRequired) when the role grants permission only to sessions verified with a second factor, and [actor.ErrForbidden](actor.md#ErrForbidden) when it doesn't grant it.

The membership is read on every call, so removing a member takes effect on their next request.

*Since `v0.1.0`*

<a id="Memberships"></a>
<a id="Memberships.MemberRole"></a>

### type Memberships

```go
type Memberships interface {
	// MemberRole returns userID's role in orgID, or [ErrNotMember] when the
	// user isn't a member or the organisation is deleted. An implementation
	// that also answers for service account IDs returns the role of an
	// enabled service account of that organisation.
	MemberRole(ctx context.Context, orgID ID, userID string) (string, error)
}
```

Memberships reads memberships. The app's orgs repository implements it.

*Since `v0.1.0`*
