# modules/mail/suppressionpg

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/mail/suppressionpg"
```

Package suppressionpg stores the email suppression list in PostgreSQL (ADR-0062): addresses that bounced permanently or marked an email as spam, which must not receive email again. [Store](#Store) implements [mail.SuppressionList](mail.md#SuppressionList), so the mail worker skips them:

```go
store, err := suppressionpg.NewStore(pool)
jobs.AddMailWorker(workers, mail.WithSuppressionList(sender, store))
```

Provider webhooks add entries with [Store.AddOnce](#Store.AddOnce), which applies each delivery once; operators list and remove them with [Store.List](#Store.List) and [Store.Remove](#Store.Remove). Addresses are personal data: they are stored normalized with [mail.NormalizeAddress](mail.md#NormalizeAddress) and kept until removed.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`MaxEmailLength`](#MaxEmailLength)
- Variables: [`ErrNotFound`](#ErrNotFound), [`ErrInvalidEntry`](#ErrInvalidEntry), [`ErrInvalidCursor`](#ErrInvalidCursor), [`ErrDuplicateDelivery`](#ErrDuplicateDelivery), [`Migrations`](#Migrations)
- Types:
  - [`Entry`](#Entry)
  - [`Filter`](#Filter)
  - [`Page`](#Page)
  - [`Reason`](#Reason): [`ReasonBounce`](#ReasonBounce), [`ReasonComplaint`](#ReasonComplaint)
  - [`Store`](#Store): [`NewStore`](#NewStore), [`Store.Add`](#Store.Add), [`Store.AddOnce`](#Store.AddOnce), [`Store.List`](#Store.List), [`Store.Remove`](#Store.Remove), [`Store.Suppressed`](#Store.Suppressed)
  - [`Suppression`](#Suppression)

## Constants

<a id="MaxEmailLength"></a>

```go
const (
	// MaxEmailLength is the longest address stored.
	MaxEmailLength = 320
)
```

Limits on stored values.

*Since `v0.1.0`*

## Variables

<a id="ErrNotFound"></a>
<a id="ErrInvalidEntry"></a>
<a id="ErrInvalidCursor"></a>
<a id="ErrDuplicateDelivery"></a>

```go
var (
	// ErrNotFound reports a suppression ID that doesn't exist.
	ErrNotFound = errors.New("suppressionpg: suppression not found")
	// ErrInvalidEntry reports an entry without a valid address, reason or
	// source. The wrapping error says which, without the address.
	ErrInvalidEntry = errors.New("suppressionpg: invalid entry")
	// ErrInvalidCursor reports a cursor that wasn't returned by [Store.List].
	ErrInvalidCursor = errors.New("suppressionpg: invalid cursor")
	// ErrDuplicateDelivery reports a delivery [Store.AddOnce] already applied.
	ErrDuplicateDelivery = errors.New("suppressionpg: delivery already applied")
)
```

Errors returned by [Store](#Store) methods. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

<a id="Migrations"></a>

```go
var Migrations fs.FS = mustSub(migrationFiles, "migrations")
```

Migrations holds the module's goose migrations: the mail\_suppressions and mail\_webhook\_deliveries tables. Apps copy them into db/migrations; tests can apply them directly with pgtest.

*Since `v0.1.0`*

## Types

<a id="Entry"></a>
<a id="Entry.Email"></a>
<a id="Entry.Reason"></a>
<a id="Entry.Source"></a>
<a id="Entry.Detail"></a>

### type Entry

```go
type Entry struct {
	// Email is normalized before it is stored.
	Email  string
	Reason Reason
	// Source names who reported it, such as "resend": lower-case letters,
	// digits and underscores.
	Source string
	// Detail is the provider's classification, such as "Permanent/General".
	// Never put a server's message here: it can quote addresses.
	Detail string
}
```

An Entry is an address to suppress.

*Since `v0.1.0`*

<a id="Filter"></a>
<a id="Filter.Reason"></a>
<a id="Filter.Limit"></a>
<a id="Filter.Cursor"></a>

### type Filter

```go
type Filter struct {
	Reason Reason
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is Page.NextCursor from the previous page.
	Cursor string
}
```

Filter selects suppressions. Empty fields match everything.

*Since `v0.1.0`*

<a id="Page"></a>
<a id="Page.Suppressions"></a>
<a id="Page.NextCursor"></a>

### type Page

```go
type Page struct {
	Suppressions []Suppression
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}
```

Page is a page of suppressions, newest first.

*Since `v0.1.0`*

<a id="Reason"></a>

### type Reason

```go
type Reason string
```

A Reason is why an address is suppressed.

*Since `v0.1.0`*

<a id="ReasonBounce"></a>
<a id="ReasonComplaint"></a>

```go
const (
	// ReasonBounce is a permanent (hard) bounce.
	ReasonBounce Reason = "bounce"
	// ReasonComplaint is a recipient marking an email as spam.
	ReasonComplaint Reason = "complaint"
)
```

Reasons.

*Since `v0.1.0`*

<a id="Store"></a>

### type Store

```go
type Store struct {
	// contains filtered or unexported fields
}
```

Store is the suppression list. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewStore"></a>

#### func NewStore

```go
func NewStore(pool *pgxpool.Pool) (*Store, error)
```

NewStore returns a store on pool, which must have the module's migrations applied.

*Since `v0.1.0`*

<a id="Store.Add"></a>

#### func (*Store) Add

```go
func (s *Store) Add(ctx context.Context, entries ...Entry) ([]Suppression, error)
```

Add puts entries on the list and returns the addresses that weren't on it before. An address already on it keeps its ID and creation time, and takes the entry's reason, source and detail. Entries are applied in one transaction; at most 100 per call.

*Since `v0.1.0`*

<a id="Store.AddOnce"></a>

#### func (*Store) AddOnce

```go
func (s *Store) AddOnce(ctx context.Context, key string, expiresAt time.Time, entries ...Entry) ([]Suppression, error)
```

AddOnce is [Store.Add](#Store.Add) for one provider delivery, such as a signed webhook request: key names it (for example "resend:" and its ID) and is remembered until expiresAt. A key applied before and not yet expired changes nothing and returns [ErrDuplicateDelivery](#ErrDuplicateDelivery). The key and the entries commit together, so a delivery that failed can be retried.

*Since `v0.1.0`*

<a id="Store.List"></a>

#### func (*Store) List

```go
func (s *Store) List(ctx context.Context, f Filter) (Page, error)
```

List returns suppressions matching f, most recently added first. It returns [ErrInvalidCursor](#ErrInvalidCursor) for a cursor it didn't return.

*Since `v0.1.0`*

<a id="Store.Remove"></a>

#### func (*Store) Remove

```go
func (s *Store) Remove(ctx context.Context, id int64) (Suppression, error)
```

Remove takes the suppression id off the list and returns it, so the address receives email again until its next bounce or complaint. It returns [ErrNotFound](#ErrNotFound) for an unknown ID.

*Since `v0.1.0`*

<a id="Store.Suppressed"></a>

#### func (*Store) Suppressed

```go
func (s *Store) Suppressed(ctx context.Context, emails []string) ([]string, error)
```

Suppressed implements [mail.SuppressionList](mail.md#SuppressionList): it returns the addresses among emails that are on the list, normalized.

*Since `v0.1.0`*

<a id="Suppression"></a>
<a id="Suppression.ID"></a>
<a id="Suppression.Email"></a>
<a id="Suppression.Reason"></a>
<a id="Suppression.Source"></a>
<a id="Suppression.Detail"></a>
<a id="Suppression.CreatedAt"></a>
<a id="Suppression.UpdatedAt"></a>

### type Suppression

```go
type Suppression struct {
	ID     int64
	Email  string
	Reason Reason
	Source string
	Detail string
	// CreatedAt is when the address was first suppressed; UpdatedAt is the
	// latest event for it.
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

A Suppression is an address on the list.

*Since `v0.1.0`*
