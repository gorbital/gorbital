// Package repository stores the orgs module's organisations, members and
// invitations in PostgreSQL with hand-written SQL, one file per operation
// (ADR-0032). The tables come from db/migrations. Member and invitation
// queries read email addresses from the auth module's auth_users table,
// which org_members references.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
	orgsusecase "example.com/plateful/internal/modules/orgs/usecase"
)

// Store implements the orgs use cases' storage port. It runs on the pool, or
// on a transaction inside InTx.
type Store struct {
	db   postgres.DBTX
	pool *pgxpool.Pool // nil inside a transaction
}

var _ orgsusecase.Store = (*Store)(nil)

// NewStore returns a store on pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool, pool: pool}
}

// InTx runs fn with a store bound to one transaction. Inside a transaction,
// fn joins it.
func (s *Store) InTx(ctx context.Context, fn func(tx orgsusecase.Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{db: tx})
	})
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok {
		switch constraint {
		case "org_members_pkey":
			return orgsdomain.ErrAlreadyMember
		case "org_invitations_open":
			return orgsdomain.ErrAlreadyInvited
		case "orgs_restaurant_name":
			return orgsdomain.ErrOrgNameTaken
		}
	}
	return err
}
