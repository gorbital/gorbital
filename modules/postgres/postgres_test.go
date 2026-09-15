package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"gorbital.dev/config"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/postgres/pgtest"
)

func TestOpenRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	valid := config.NewSecret("postgres://u:hunter2@127.0.0.1:1/db")
	tests := []struct {
		name string
		url  config.Secret
		opts []postgres.Option
		want string
	}{
		{"empty URL", config.Secret{}, nil, "database URL is required"},
		{"malformed URL", config.NewSecret("postgres://u:hunter2@host:notaport/db"), nil, "not a valid PostgreSQL connection string"},
		{"negative max conns", valid, []postgres.Option{postgres.WithMaxConns(-1)}, "max connections"},
		{"min above max", valid, []postgres.Option{postgres.WithMinConns(5), postgres.WithMaxConns(2)}, "exceed max connections"},
		{"zero connect timeout", valid, []postgres.Option{postgres.WithConnectTimeout(0)}, "connect timeout"},
		{"nil tracer provider", valid, []postgres.Option{postgres.WithTracerProvider(nil)}, "tracer provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := postgres.Open(ctx, tt.url, tt.opts...)
			if err == nil {
				pool.Close()
				t.Fatal("Open() error = nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Open() error = %q, want it to contain %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("Open() error leaks the password: %q", err)
			}
		})
	}
}

func TestOpenUnreachableServerFailsWithoutLeakingPassword(t *testing.T) {
	start := time.Now()
	_, err := postgres.Open(context.Background(), config.NewSecret("postgres://u:hunter2@127.0.0.1:1/db"),
		postgres.WithConnectTimeout(2*time.Second))
	if err == nil {
		t.Fatal("Open() error = nil for a closed port")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("Open() error leaks the password: %q", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Open() took %v, want the connect timeout to bound it", elapsed)
	}
}

func TestOpenConfiguresPoolAndTracesQueries(t *testing.T) {
	ctx := context.Background()
	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	pool, err := postgres.Open(ctx, config.NewSecret(pgtest.URL(t)),
		postgres.WithTracerProvider(tp),
		postgres.WithApplicationName("postgres-module-test"),
		postgres.WithMaxConns(3),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	if got := pool.Config().MaxConns; got != 3 {
		t.Errorf("MaxConns = %d, want 3", got)
	}
	var appName string
	if err := pool.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName); err != nil {
		t.Fatalf("query application_name: %v", err)
	}
	if appName != "postgres-module-test" {
		t.Errorf("application_name = %q, want postgres-module-test", appName)
	}
	if _, err := pool.Exec(ctx, "SELECT * FROM table_that_does_not_exist WHERE id = $1", 42); err == nil {
		t.Fatal("query on a missing table succeeded")
	}

	got := spans.GetSpans()
	if len(got) != 2 {
		t.Fatalf("recorded %d spans, want 2", len(got))
	}
	ok, failed := got[0], got[1]
	if ok.Name != "SELECT" || ok.SpanKind.String() != "client" {
		t.Errorf("span = %q (%s), want SELECT client span", ok.Name, ok.SpanKind)
	}
	if attr(ok.Attributes, "db.system.name") != "postgresql" || attr(ok.Attributes, "db.query.text") == "" {
		t.Errorf("span attributes = %v, want db.system.name and db.query.text", ok.Attributes)
	}
	if failed.Status.Code != codes.Error || attr(failed.Attributes, "db.response.status_code") != "42P01" {
		t.Errorf("failed span status = %v, attributes = %v; want error with SQLSTATE 42P01", failed.Status, failed.Attributes)
	}
	for _, s := range got {
		for _, a := range s.Attributes {
			if strings.Contains(a.Value.String(), "42") && a.Key != "db.response.status_code" && a.Key != "db.query.text" {
				t.Errorf("span attribute %s = %q may contain a query argument", a.Key, a.Value.String())
			}
		}
	}
}

func TestInTx(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	mustExec(t, pool, "CREATE TABLE items (name text PRIMARY KEY)")
	insert := func(name string) func(pgx.Tx) error {
		return func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO items VALUES ($1)", name)
			return err
		}
	}

	if err := postgres.InTx(ctx, pool, insert("committed")); err != nil {
		t.Fatalf("InTx() error = %v", err)
	}

	errBoom := errors.New("boom")
	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := insert("rolled-back")(tx); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Errorf("InTx() error = %v, want fn's error", err)
	}

	func() {
		defer func() {
			if r := recover(); r != "kaboom" {
				t.Errorf("recovered %v, want the panic to continue", r)
			}
		}()
		_ = postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
			_ = insert("panicked")(tx)
			panic("kaboom")
		})
	}()

	err = postgres.InTxWithOptions(ctx, pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, insert("read-only"))
	if err == nil {
		t.Error("insert in a read-only transaction succeeded")
	}

	rows, err := pool.Query(ctx, "SELECT name FROM items ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "committed" {
		t.Errorf("rows = %v, want only the committed row", names)
	}
}

func TestErrorClassification(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	mustExec(t, pool, `
		CREATE TABLE parents (id int PRIMARY KEY);
		CREATE TABLE children (
			id int PRIMARY KEY,
			parent_id int NOT NULL CONSTRAINT children_parent_fk REFERENCES parents (id),
			qty int CONSTRAINT children_qty_positive CHECK (qty > 0),
			CONSTRAINT children_parent_qty_key UNIQUE (parent_id, qty)
		);
		INSERT INTO parents VALUES (1);
		INSERT INTO children VALUES (1, 1, 1);`)
	insert := func(id, parent int, qty any) error {
		_, err := pool.Exec(ctx, "INSERT INTO children VALUES ($1, $2, $3)", id, parent, qty)
		return err
	}

	if c, ok := postgres.UniqueViolation(insert(2, 1, 1)); !ok || c != "children_parent_qty_key" {
		t.Errorf("UniqueViolation() = %q, %v; want children_parent_qty_key, true", c, ok)
	}
	if c, ok := postgres.ForeignKeyViolation(insert(3, 99, 1)); !ok || c != "children_parent_fk" {
		t.Errorf("ForeignKeyViolation() = %q, %v; want children_parent_fk, true", c, ok)
	}
	if c, ok := postgres.CheckViolation(insert(4, 1, 0)); !ok || c != "children_qty_positive" {
		t.Errorf("CheckViolation() = %q, %v; want children_qty_positive, true", c, ok)
	}
	_, err := pool.Exec(ctx, "INSERT INTO children (id, qty) VALUES (5, 1)")
	if col, ok := postgres.NotNullViolation(err); !ok || col != "parent_id" {
		t.Errorf("NotNullViolation() = %q, %v; want parent_id, true", col, ok)
	}

	var id int
	err = pool.QueryRow(ctx, "SELECT id FROM children WHERE id = 42").Scan(&id)
	if !postgres.IsNoRows(err) {
		t.Errorf("IsNoRows(%v) = false", err)
	}

	other := errors.New("other")
	if _, ok := postgres.UniqueViolation(other); ok {
		t.Error("UniqueViolation(non-PostgreSQL error) = true")
	}
	if _, ok := postgres.UniqueViolation(nil); ok {
		t.Error("UniqueViolation(nil) = true")
	}
	if postgres.IsNoRows(nil) || postgres.IsRetryable(other) {
		t.Error("IsNoRows(nil) or IsRetryable(other) = true")
	}
	for _, code := range []string{"40001", "40P01"} {
		if !postgres.IsRetryable(&pgconn.PgError{Code: code}) {
			t.Errorf("IsRetryable(SQLSTATE %s) = false", code)
		}
	}
}

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	fsys := fstest.MapFS{
		"00001_create_widgets.sql":  {Data: []byte("-- +goose Up\nCREATE TABLE widgets (id int PRIMARY KEY);\n\n-- +goose Down\nDROP TABLE widgets;\n")},
		"00002_add_widget_name.sql": {Data: []byte("-- +goose Up\nALTER TABLE widgets ADD COLUMN name text;\n")},
	}

	applied, err := postgres.Migrate(ctx, pool, fsys)
	if err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if len(applied) != 2 || applied[0] != 1 || applied[1] != 2 {
		t.Errorf("Migrate() applied %v, want [1 2]", applied)
	}
	if applied, err := postgres.Migrate(ctx, pool, fsys); err != nil || len(applied) != 0 {
		t.Errorf("second Migrate() = %v, %v; want nothing applied", applied, err)
	}
	if state, err := postgres.Migrations(ctx, pool, fsys); err != nil || state != (postgres.MigrationState{Current: 2, Latest: 2}) {
		t.Errorf("Migrations() = %+v, %v; want current 2, latest 2, none pending", state, err)
	}

	fsys["00003_index_widget_name.sql"] = &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE INDEX widgets_name_idx ON widgets (name);\n")}
	if state, err := postgres.Migrations(ctx, pool, fsys); err != nil || state != (postgres.MigrationState{Current: 2, Latest: 3, Pending: 1}) {
		t.Errorf("Migrations() = %+v, %v; want current 2, latest 3, 1 pending", state, err)
	}

	// The pool stays usable after migrating through database/sql.
	mustExec(t, pool, "INSERT INTO widgets (id, name) VALUES (1, 'gear')")
}

func TestMigrateReportsFailureAndPartialProgress(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	fsys := fstest.MapFS{
		"00001_ok.sql":     {Data: []byte("-- +goose Up\nCREATE TABLE ok (id int);\n")},
		"00002_broken.sql": {Data: []byte("-- +goose Up\nCREATE TABLE broken (id nonexistent_type);\n")},
	}
	applied, err := postgres.Migrate(ctx, pool, fsys)
	if err == nil {
		t.Fatal("Migrate() error = nil for a broken migration")
	}
	if len(applied) != 1 || applied[0] != 1 {
		t.Errorf("Migrate() applied %v, want [1] before the failure", applied)
	}
	if state, err := postgres.Migrations(ctx, pool, fsys); err != nil || state.Current != 1 || state.Pending != 1 {
		t.Errorf("Migrations() = %+v, %v; want current 1, 1 pending", state, err)
	}
}

func TestMigrateConcurrentCallersApplyOnce(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t, pgtest.WithMaxConns(8))
	fsys := fstest.MapFS{
		"00001_counter.sql": {Data: []byte("-- +goose Up\nCREATE TABLE counter (n int);\nINSERT INTO counter VALUES (1);\n")},
	}

	const callers = 4
	var wg sync.WaitGroup
	results := make([][]int64, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Go(func() { results[i], errs[i] = postgres.Migrate(ctx, pool, fsys) })
	}
	wg.Wait()

	total := 0
	for i := range callers {
		if errs[i] != nil {
			t.Errorf("caller %d: Migrate() error = %v", i, errs[i])
		}
		total += len(results[i])
	}
	if total != 1 {
		t.Errorf("migration applied %d times, want exactly once", total)
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM counter").Scan(&rows); err != nil || rows != 1 {
		t.Errorf("counter rows = %d, %v; want 1", rows, err)
	}
}

func TestMigrateWithoutMigrationFiles(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	if applied, err := postgres.Migrate(ctx, pool, fstest.MapFS{}); err != nil || applied != nil {
		t.Errorf("Migrate(empty) = %v, %v; want nil, nil", applied, err)
	}
	if state, err := postgres.Migrations(ctx, pool, fstest.MapFS{}); err != nil || state != (postgres.MigrationState{}) {
		t.Errorf("Migrations(empty) = %+v, %v; want zero state", state, err)
	}
}

func TestHealthCheck(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Open(ctx, config.NewSecret(pgtest.URL(t)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	check := postgres.HealthCheck(pool)
	if check.Name != "postgres" || check.Timeout <= 0 {
		t.Errorf("HealthCheck() = %q, timeout %v; want postgres with a timeout", check.Name, check.Timeout)
	}
	if err := check.Func(ctx); err != nil {
		t.Errorf("check on an open pool: %v", err)
	}
	pool.Close()
	if err := check.Func(ctx); err == nil {
		t.Error("check on a closed pool succeeded")
	}
}

func mustExec(t *testing.T, db postgres.DBTX, sql string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func attr(attrs []attribute.KeyValue, key attribute.Key) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value.String()
		}
	}
	return ""
}
