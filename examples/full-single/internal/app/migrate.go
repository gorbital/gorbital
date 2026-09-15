package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres"

	"example.com/acme-api/db/migrations"
)

// Migrate applies pending goose migrations from db/migrations, then River's
// schema migrations, reporting each applied version to w.
func Migrate(ctx context.Context, cfg Config, w io.Writer) error {
	if cfg.DatabaseURL.IsZero() {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.WithApplicationName(ServiceName+"-migrate"))
	if err != nil {
		return err
	}
	defer pool.Close()

	applied, err := postgres.Migrate(ctx, pool, migrations.FS)
	for _, v := range applied {
		fmt.Fprintf(w, "applied migration %d\n", v)
	}
	if err != nil {
		return err
	}
	riverApplied, err := jobs.Migrate(ctx, pool)
	for _, v := range riverApplied {
		fmt.Fprintf(w, "applied job queue migration %d\n", v)
	}
	return err
}
