package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Migrate applies River's pending schema migrations with River's own
// migrator and returns the versions applied. Run it from the app's migrate
// command after goose migrations (ADR-0033); never at startup (ADR-0017).
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]int, error) {
	m, err := newMigrator(pool)
	if err != nil {
		return nil, err
	}
	res, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return nil, fmt.Errorf("jobs: migrate river schema: %w", err)
	}
	versions := make([]int, len(res.Versions))
	for i, v := range res.Versions {
		versions[i] = v.Version
	}
	return versions, nil
}

// MigrationsPending describes River migrations not yet applied, empty when
// the schema is current. It changes nothing.
func MigrationsPending(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	m, err := newMigrator(pool)
	if err != nil {
		return nil, err
	}
	res, err := m.Validate(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("jobs: check river schema: %w", err)
	}
	if res.OK {
		return nil, nil
	}
	return res.Messages, nil
}

func newMigrator(pool *pgxpool.Pool) (*rivermigrate.Migrator[pgx.Tx], error) {
	m, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		return nil, fmt.Errorf("jobs: river migrator: %w", err)
	}
	return m, nil
}
