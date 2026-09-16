package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Migrate applies every pending goose migration in the root of fsys (files
// such as 00001_create_users.sql) and returns the versions it applied, in
// order. It returns nil when fsys has no migrations or none are pending.
//
// A PostgreSQL advisory lock serialises concurrent callers, so several
// instances can run Migrate at once. Migrations run
// [WithoutRowLevelSecurity], so a data migration reaches every
// organisation's rows (ADR-0061). Apps run migrations from a separate
// command, never implicitly at startup (ADR-0017).
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]int64, error) {
	ctx = WithoutRowLevelSecurity(ctx, "migrate")
	var applied []int64
	err := withProvider(pool, fsys, func(p *goose.Provider) error {
		results, err := p.Up(ctx)
		var partial *goose.PartialError
		if errors.As(err, &partial) {
			results = partial.Applied
		}
		for _, r := range results {
			applied = append(applied, r.Source.Version)
		}
		return err
	})
	if errors.Is(err, goose.ErrNoMigrations) {
		return nil, nil
	}
	if err != nil {
		return applied, fmt.Errorf("postgres: migrate: %w", err)
	}
	return applied, nil
}

// MigrationState describes the schema version against a set of migrations.
type MigrationState struct {
	// Current is the highest applied version, 0 when none are applied.
	Current int64
	// Latest is the highest version in the migration files.
	Latest int64
	// Pending counts migration files not yet applied.
	Pending int
}

// Migrations reports the database's migration state for fsys, for readiness
// reports and doctor commands. It changes nothing.
func Migrations(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (MigrationState, error) {
	var state MigrationState
	err := withProvider(pool, fsys, func(p *goose.Provider) error {
		statuses, err := p.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			state.Latest = max(state.Latest, s.Source.Version)
			if s.State == goose.StatePending {
				state.Pending++
			}
		}
		state.Current, err = p.GetDBVersion(ctx)
		return err
	})
	if errors.Is(err, goose.ErrNoMigrations) {
		return MigrationState{}, nil
	}
	if err != nil {
		return MigrationState{}, fmt.Errorf("postgres: migration state: %w", err)
	}
	return state, nil
}

// Lock polling: goose's default retries every 5 seconds, so a second instance
// waits at least that long after the first finishes. Poll every second, still
// giving up after 5 minutes.
const (
	lockPollSeconds = 1
	lockMaxAttempts = 300
)

func withProvider(pool *pgxpool.Pool, fsys fs.FS, fn func(*goose.Provider) error) error {
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(lockPollSeconds, lockMaxAttempts))
	if err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		return errors.Join(err, db.Close())
	}
	return errors.Join(fn(p), p.Close())
}
