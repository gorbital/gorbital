package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"apistock.dev/modules/postgres"

	"example.com/acme-api/db/migrations"
)

// MigrationStatus is what go run ./cmd/migrate --status reports, for people
// and for aps doctor (ADR-0051).
type MigrationStatus struct {
	// ConfigError is why the configuration didn't load; the database isn't
	// checked then.
	ConfigError string `json:"config_error,omitempty"`
	// DatabaseError is why the database couldn't be read.
	DatabaseError string `json:"database_error,omitempty"`
	// Current is the database's migration version, Latest the newest
	// migration file's.
	Current int64 `json:"current"`
	Latest  int64 `json:"latest"`
	Pending int   `json:"pending"`
}

// statusTimeout bounds connecting to the database and reading its state.
const statusTimeout = 5 * time.Second

// WriteMigrationStatus reports the database's migration state to w and
// changes nothing. cfgErr is LoadConfig's error, reported instead. With
// asJSON it writes one JSON object and returns nil, so tools read problems
// from its fields; otherwise it returns them as errors.
func WriteMigrationStatus(ctx context.Context, cfg Config, cfgErr error, asJSON bool, w io.Writer) error {
	s := migrationStatus(ctx, cfg, cfgErr)
	if asJSON {
		return json.NewEncoder(w).Encode(s)
	}
	switch {
	case s.ConfigError != "":
		return errors.New(s.ConfigError)
	case s.DatabaseError != "":
		return errors.New(s.DatabaseError)
	}
	fmt.Fprintf(w, "migrations: database at %d, newest file %d, %d pending\n", s.Current, s.Latest, s.Pending)
	return nil
}

func migrationStatus(ctx context.Context, cfg Config, cfgErr error) MigrationStatus {
	switch {
	case cfgErr != nil:
		return MigrationStatus{ConfigError: cfgErr.Error()}
	case cfg.DatabaseURL.IsZero():
		return MigrationStatus{ConfigError: "DATABASE_URL is required"}
	}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	pool, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.WithApplicationName(ServiceName+"-status"), postgres.WithConnectTimeout(3*time.Second))
	if err != nil {
		return MigrationStatus{DatabaseError: "connect to DATABASE_URL: " + err.Error()}
	}
	defer pool.Close()
	state, err := postgres.Migrations(ctx, pool, migrations.FS)
	if err != nil {
		return MigrationStatus{DatabaseError: "read the migration state: " + err.Error()}
	}
	return MigrationStatus{Current: state.Current, Latest: state.Latest, Pending: state.Pending}
}
