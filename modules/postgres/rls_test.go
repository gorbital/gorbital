package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/postgres/pgtest"
)

// rlsRole is the role the row-level security tests connect as: not a
// superuser and without BYPASSRLS, since PostgreSQL applies no policy to
// those. Roles belong to the whole server, so every test shares it.
const rlsRole = "gorbital_postgres_rls_test"

// orgPolicy is the policy orb add rls creates (ADR-0061).
const orgPolicy = `
	USING (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on')
	WITH CHECK (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on')`

// rlsDatabase creates a database with organisations a and b, items in both
// (row-level security forced, owned by rlsRole, so only FORCE applies the
// policy to it), and returns an admin pool and a URL that connects as
// rlsRole.
func rlsDatabase(t testing.TB) (admin *pgx.Conn, roleURL string) {
	t.Helper()
	ctx := context.Background()
	dbURL := pgtest.NewDatabase(t)
	admin, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	createRole(t, admin, rlsRole)
	mustExec(t, admin, `
		GRANT ALL ON SCHEMA public TO `+rlsRole+`;
		SET ROLE `+rlsRole+`;
		CREATE TABLE orgs (id text PRIMARY KEY);
		CREATE TABLE items (
			id     text PRIMARY KEY,
			org_id text NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
			name   text NOT NULL
		);
		CREATE TABLE notes (id text PRIMARY KEY, org_id text NOT NULL);
		CREATE TABLE loose (id text PRIMARY KEY, org_id text NOT NULL);
		INSERT INTO orgs VALUES ('org_a'), ('org_b');
		INSERT INTO items VALUES ('i1', 'org_a', 'one'), ('i2', 'org_a', 'two'), ('i3', 'org_b', 'three');
		ALTER TABLE items ENABLE ROW LEVEL SECURITY;
		ALTER TABLE items FORCE ROW LEVEL SECURITY;
		CREATE POLICY org_isolation ON items `+orgPolicy+`;
		ALTER TABLE notes ENABLE ROW LEVEL SECURITY;
		CREATE POLICY org_isolation ON notes `+orgPolicy+`;
		RESET ROLE;`)

	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("options", "-c role="+rlsRole)
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20") // pgx reads + literally
	return admin, u.String()
}

// createRole creates role once for the whole server; test binaries run in
// parallel, so an advisory lock serialises them.
func createRole(t testing.TB, admin *pgx.Conn, role string) {
	t.Helper()
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, admin, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pgtest.role', 0))"); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil || exists {
			return err
		}
		_, err := tx.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" NOLOGIN NOSUPERUSER NOBYPASSRLS")
		return err
	})
	if err != nil {
		t.Fatalf("create role %s: %v", role, err)
	}
}

func countItems(t *testing.T, ctx context.Context, db postgres.DBTX) int {
	t.Helper()
	// Deliberately without an org_id filter: the policy is the only limit.
	var n int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM items").Scan(&n); err != nil {
		t.Fatalf("count items: %v", err)
	}
	return n
}

func asOrg(orgID string) context.Context {
	return actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1", OrgID: orgID})
}

func TestRowLevelSecurityFollowsTheContext(t *testing.T) {
	admin, roleURL := rlsDatabase(t)
	var logs bytes.Buffer
	spans := tracetest.NewInMemoryExporter()
	// One connection, so every step reuses the connection the previous one
	// changed.
	pool, err := postgres.Open(context.Background(), config.NewSecret(roleURL),
		postgres.WithMaxConns(1),
		postgres.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
		postgres.WithTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	bypass := postgres.WithoutRowLevelSecurity(context.Background(), "test:maintenance")
	for _, step := range []struct {
		name string
		ctx  context.Context
		want int
	}{
		{"no organisation", context.Background(), 0},
		{"the actor's organisation", asOrg("org_a"), 2},
		{"after an organisation, none again", context.Background(), 0},
		{"another organisation", asOrg("org_b"), 1},
		{"WithOrg over the actor's", postgres.WithOrg(asOrg("org_b"), "org_a"), 2},
		{"a system actor without an organisation", actor.With(context.Background(), actor.System("job")), 0},
		{"bypass", bypass, 3},
		{"bypass again", bypass, 3},
		{"after the bypass", asOrg("org_b"), 1},
		{"an unknown organisation", asOrg("org_z"), 0},
	} {
		if got := countItems(t, step.ctx, pool); got != step.want {
			t.Errorf("%s: %d items visible, want %d", step.name, got, step.want)
		}
	}
	var org, on string
	if err := pool.QueryRow(asOrg("org_a"), "SELECT current_setting('gorbital.org_id'), current_setting('gorbital.rls_bypass')").Scan(&org, &on); err != nil || org != "org_a" || on != "" {
		t.Errorf("settings = %q, %q, %v; want org_a and no bypass", org, on, err)
	}

	// A transaction keeps the organisation of the context that began it.
	err = postgres.InTx(asOrg("org_a"), pool, func(tx pgx.Tx) error {
		if got := countItems(t, context.Background(), tx); got != 2 {
			t.Errorf("in a transaction for org_a: %d items, want 2", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Writes are checked too: a row can't be moved or added to another
	// organisation.
	_, err = pool.Exec(asOrg("org_a"), "INSERT INTO items VALUES ('i4', 'org_b', 'four')")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("insert into another organisation: %v, want a row-level security violation", err)
	}
	if tag, err := pool.Exec(asOrg("org_a"), "UPDATE items SET name = 'moved' WHERE id = 'i3'"); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("update of another organisation's row = %v, %v; want nothing updated", tag, err)
	}

	// Deleting an organisation removes its rows through the foreign key,
	// without a bypass: referential actions don't apply policies.
	if _, err := pool.Exec(context.Background(), "DELETE FROM orgs WHERE id = 'org_b'"); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := admin.QueryRow(context.Background(), "SELECT count(*) FROM items WHERE org_id = 'org_b'").Scan(&left); err != nil || left != 0 {
		t.Errorf("org_b items after deleting org_b = %d, %v; want 0", left, err)
	}

	// The bypass is logged once per context, and its spans name it.
	if n := strings.Count(logs.String(), "row-level security bypassed"); n != 1 || !strings.Contains(logs.String(), "reason=test:maintenance") {
		t.Errorf("logs = %q, want one bypass line with its reason", logs.String())
	}
	var bypassed, others int
	for _, s := range spans.GetSpans() {
		if attr(s.Attributes, attribute.Key("gorbital.rls_bypass")) == "test:maintenance" {
			bypassed++
		} else {
			others++
		}
	}
	if bypassed != 2 || others == 0 {
		t.Errorf("spans with the bypass reason = %d (others %d), want the 2 bypassed queries", bypassed, others)
	}
}

func TestRowLevelSecuritySettingFailureDropsTheConnection(t *testing.T) {
	_, roleURL := rlsDatabase(t)
	pool, err := postgres.Open(context.Background(), config.NewSecret(roleURL), postgres.WithMaxConns(1))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if countItems(t, asOrg("org_a"), pool) != 2 {
		t.Fatal("org_a doesn't see its items")
	}
	created := pool.Stat().NewConnsCount()
	// PostgreSQL refuses a NUL byte in a setting, so setting the organisation
	// fails after the connection held org_a.
	var n int
	if err := pool.QueryRow(postgres.WithOrg(context.Background(), "org_\x00b"), "SELECT count(*) FROM items").Scan(&n); err == nil {
		t.Fatalf("query with an organisation that can't be set succeeded (%d items)", n)
	}
	if got := countItems(t, context.Background(), pool); got != 0 {
		t.Errorf("after a failed setting, no organisation sees %d items, want 0", got)
	}
	if pool.Stat().NewConnsCount() != created+1 {
		t.Errorf("connections created = %d, want the failed one replaced (%d)", pool.Stat().NewConnsCount(), created+1)
	}
}

func TestMigrateBypassesRowLevelSecurity(t *testing.T) {
	admin, roleURL := rlsDatabase(t)
	pool, err := postgres.Open(context.Background(), config.NewSecret(roleURL))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	applied, err := postgres.Migrate(context.Background(), pool, fstest.MapFS{
		"00001_rename_items.sql": {Data: []byte("-- +goose Up\nUPDATE items SET name = upper(name);\n")},
	})
	if err != nil || len(applied) != 1 {
		t.Fatalf("Migrate() = %v, %v", applied, err)
	}
	var upper int
	if err := admin.QueryRow(context.Background(), "SELECT count(*) FROM items WHERE name = upper(name)").Scan(&upper); err != nil || upper != 3 {
		t.Errorf("items changed by the data migration = %d, %v; want all 3", upper, err)
	}
	// The migration's connections go back to the pool without the bypass.
	if got := countItems(t, context.Background(), pool); got != 0 {
		t.Errorf("after Migrate, a context without an organisation sees %d items", got)
	}
}

func TestCheckRowLevelSecurity(t *testing.T) {
	admin, roleURL := rlsDatabase(t)
	ctx := context.Background()
	role, err := pgx.Connect(ctx, roleURL)
	if err != nil {
		t.Fatal(err)
	}
	defer role.Close(ctx)

	r, err := postgres.CheckRowLevelSecurity(ctx, role)
	if err != nil {
		t.Fatal(err)
	}
	want := postgres.RowLevelSecurityReport{Role: rlsRole, Forced: []string{"items"}, NotForced: []string{"notes"}, Unprotected: []string{"loose"}}
	if !r.On() || !equalReports(r, want) {
		t.Errorf("CheckRowLevelSecurity() as %s = %+v, want %+v", rlsRole, r, want)
	}
	superuser, err := postgres.CheckRowLevelSecurity(ctx, admin)
	if err != nil || !superuser.Bypasses {
		t.Errorf("CheckRowLevelSecurity() as the test server's superuser = %+v, %v; want Bypasses", superuser, err)
	}

	empty, err := postgres.CheckRowLevelSecurity(ctx, pgtest.New(t))
	if err != nil || empty.On() || len(empty.Unprotected) != 0 {
		t.Errorf("CheckRowLevelSecurity() on an empty database = %+v, %v", empty, err)
	}
}

func equalReports(a, b postgres.RowLevelSecurityReport) bool {
	eq := func(x, y []string) bool { return strings.Join(x, ",") == strings.Join(y, ",") }
	return a.Role == b.Role && a.Bypasses == b.Bypasses && eq(a.Forced, b.Forced) && eq(a.NotForced, b.NotForced) && eq(a.Unprotected, b.Unprotected)
}

func TestWithoutRowLevelSecurityNeedsAReason(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("WithoutRowLevelSecurity with a blank reason didn't panic")
		}
	}()
	postgres.WithoutRowLevelSecurity(context.Background(), " ")
}

func TestOpenRejectsNilLogger(t *testing.T) {
	_, err := postgres.Open(context.Background(), config.NewSecret("postgres://u@127.0.0.1:1/db"), postgres.WithLogger(nil))
	if err == nil || !strings.Contains(err.Error(), "logger") {
		t.Errorf("Open(WithLogger(nil)) error = %v", err)
	}
}
