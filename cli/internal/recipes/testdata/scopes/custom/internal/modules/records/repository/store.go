// Package repository stores the records module's records in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/app/internal/modules/records/domain"
	"example.com/app/internal/modules/records/usecase"
)

// A Filter narrows a list query to the rows the caller may see. The
// module's policy.go implements it, and this package never decides for
// itself which rows a list returns.
type Filter interface {
	Filter(ctx context.Context, q *Query) error
}

// A Query is a list query before it runs: the conditions the policy adds
// to it and their arguments. An empty Query returns every record in the
// table, so a policy that narrows nothing publishes everything.
type Query struct {
	conditions []string
	args       []any
}

// And narrows the query with an SQL condition whose ? placeholders take
// args, in order:
//
//	q.And("created_by = ?", actorID)
//	q.And("state = ANY(?)", []string{"open", "closed"})
//
// Conditions are joined with AND and only the condition text reaches the
// database as SQL; every argument is a placeholder.
func (q *Query) And(condition string, args ...any) {
	q.conditions = append(q.conditions, condition)
	q.args = append(q.args, args...)
}

// where returns the conditions as SQL, numbering their placeholders from
// $first, and the arguments they take.
func (q *Query) where(first int) (string, []any) {
	var b strings.Builder
	next := first
	for _, condition := range q.conditions {
		b.WriteString("\n\t  AND (")
		for {
			before, after, found := strings.Cut(condition, "?")
			b.WriteString(before)
			if !found {
				break
			}
			b.WriteString("$" + strconv.Itoa(next))
			next, condition = next+1, after
		}
		b.WriteString(")")
	}
	return b.String(), q.args
}

// Conditions returns what the policy added, for policy_test.go: the
// conditions in order and the arguments they take.
func (q *Query) Conditions() ([]string, []any) { return q.conditions, q.args }

// Store implements usecase.Store. It runs on the pool, or on a transaction
// inside InTx, and asks filter which rows a list may return.
type Store struct {
	db     postgres.DBTX
	pool   *pgxpool.Pool // nil inside a transaction
	filter Filter
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on pool that narrows every list with filter.
func NewStore(pool *pgxpool.Pool, filter Filter) *Store {
	return &Store{db: pool, pool: pool, filter: filter}
}

// InTx runs fn with a store bound to one transaction. Inside a transaction,
// fn joins it.
func (s *Store) InTx(ctx context.Context, fn func(tx usecase.Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{db: tx, filter: s.filter})
	})
}

// recordColumns are the columns scanRecord reads, in its order.
const recordColumns = `id, created_by, title, note, state, version, created_at, updated_at`

func scanRecord(row pgx.CollectableRow) (domain.Record, error) {
	var record domain.Record
	err := row.Scan(&record.ID, &record.CreatedBy, &record.Title, &record.Note, &record.State, &record.Version, &record.CreatedAt, &record.UpdatedAt)
	record.CreatedAt, record.UpdatedAt = record.CreatedAt.UTC(), record.UpdatedAt.UTC()
	return record, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok {
		switch constraint {
		case "records_title":
			return domain.ErrRecordTitleTaken
		}
	}
	return err
}
