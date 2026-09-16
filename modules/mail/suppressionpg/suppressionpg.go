// Package suppressionpg stores the email suppression list in PostgreSQL
// (ADR-0062): addresses that bounced permanently or marked an email as spam,
// which must not receive email again. [Store] implements
// [mail.SuppressionList], so the mail worker skips them:
//
//	store, err := suppressionpg.NewStore(pool)
//	jobs.AddMailWorker(workers, mail.WithSuppressionList(sender, store))
//
// Provider webhooks add entries with [Store.AddOnce], which applies each
// delivery once; operators list and remove them with [Store.List] and
// [Store.Remove]. Addresses are personal data: they are stored normalized
// with [mail.NormalizeAddress] and kept until removed.
//
// Stability: stable (ADR-0015, ADR-0054).
package suppressionpg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/mail"
	"gorbital.dev/modules/postgres"
)

// A Reason is why an address is suppressed.
type Reason string

// Reasons.
const (
	// ReasonBounce is a permanent (hard) bounce.
	ReasonBounce Reason = "bounce"
	// ReasonComplaint is a recipient marking an email as spam.
	ReasonComplaint Reason = "complaint"
)

// Limits on stored values.
const (
	// MaxEmailLength is the longest address stored.
	MaxEmailLength = 320
	// maxSourceLength and maxDetailLength bound Entry.Source and
	// Entry.Detail; a longer detail is truncated.
	maxSourceLength = 32
	maxDetailLength = 200
	// maxKeyLength bounds a delivery key.
	maxKeyLength = 300
	// maxEntries bounds one call's entries.
	maxEntries = 100
)

// Errors returned by [Store] methods. Check them with [errors.Is].
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

// An Entry is an address to suppress.
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

// A Suppression is an address on the list.
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

// Store is the suppression list. It is safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

var _ mail.SuppressionList = (*Store)(nil)

// NewStore returns a store on pool, which must have the module's migrations
// applied.
func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("suppressionpg: a pool is required")
	}
	return &Store{pool: pool}, nil
}

// dbError hides driver errors, which aren't API (ADR-0018).
func dbError(op string, err error) error {
	return fmt.Errorf("suppressionpg: %s: %v", op, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
}

const suppressedSQL = `SELECT email FROM mail_suppressions WHERE email = ANY($1)`

// Suppressed implements [mail.SuppressionList]: it returns the addresses
// among emails that are on the list, normalized.
func (s *Store) Suppressed(ctx context.Context, emails []string) ([]string, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	normalized := make([]string, len(emails))
	for i, e := range emails {
		normalized[i] = mail.NormalizeAddress(e)
	}
	rows, err := s.pool.Query(ctx, suppressedSQL, normalized)
	if err != nil {
		return nil, dbError("check addresses", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, dbError("check addresses", err)
	}
	return found, nil
}

// validate returns e normalized, or an error wrapping ErrInvalidEntry.
func (e Entry) validate() (Entry, error) {
	e.Email = mail.NormalizeAddress(e.Email)
	switch {
	case e.Email == "" || len(e.Email) > MaxEmailLength || !strings.Contains(e.Email, "@") || strings.ContainsAny(e.Email, " \t\r\n"):
		return e, fmt.Errorf("%w: the address is not an email address", ErrInvalidEntry)
	case e.Reason != ReasonBounce && e.Reason != ReasonComplaint:
		return e, fmt.Errorf("%w: reason %q is not bounce or complaint", ErrInvalidEntry, e.Reason)
	case !validSource(e.Source):
		return e, fmt.Errorf("%w: source %q must be 1 to %d lower-case letters, digits or underscores", ErrInvalidEntry, e.Source, maxSourceLength)
	}
	e.Detail = strings.TrimSpace(e.Detail)
	if len(e.Detail) > maxDetailLength {
		e.Detail = strings.ToValidUTF8(e.Detail[:maxDetailLength], "")
	}
	return e, nil
}

func validSource(source string) bool {
	if source == "" || len(source) > maxSourceLength {
		return false
	}
	for _, r := range source {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// inTx runs fn in a transaction through postgres.InTx.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return postgres.InTx(ctx, s.pool, fn)
}
