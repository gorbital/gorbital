package postgres_test

import (
	"context"
	"testing"

	"gorbital.dev/config"
	"gorbital.dev/modules/postgres"
)

// BenchmarkRowLevelSecurity measures what carrying the organisation costs
// (ADR-0061), on one connection so every acquire reuses it:
//
//   - same-org: every acquire has the organisation the connection holds, so
//     nothing is sent; the baseline for a query.
//   - switching-org: every acquire changes it, adding one round trip.
//   - query-plain and query-policy: a list query with its org_id filter on
//     20 000 rows in 200 organisations, without and with the policy.
func BenchmarkRowLevelSecurity(b *testing.B) {
	admin, roleURL := rlsDatabase(b)
	mustExec(b, admin, `
		SET ROLE `+rlsRole+`;
		SET gorbital.rls_bypass = 'on';
		INSERT INTO orgs SELECT 'org_' || g FROM generate_series(1, 200) g;
		INSERT INTO items SELECT 'b' || g, 'org_' || (g % 200 + 1), 'item ' || g FROM generate_series(1, 20000) g;
		CREATE TABLE plain_items AS SELECT * FROM items;
		CREATE INDEX items_org ON items (org_id, id);
		CREATE INDEX plain_items_org ON plain_items (org_id, id);
		ANALYZE items;
		ANALYZE plain_items;
		RESET gorbital.rls_bypass;
		RESET ROLE;`)
	pool, err := postgres.Open(context.Background(), config.NewSecret(roleURL), postgres.WithMaxConns(1))
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	orgs := []context.Context{asOrg("org_1"), asOrg("org_2")}

	query := func(b *testing.B, ctx context.Context, sql string, args ...any) {
		rows, err := pool.Query(ctx, sql, args...)
		if err != nil {
			b.Fatal(err)
		}
		rows.Close()
		if rows.Err() != nil {
			b.Fatal(rows.Err())
		}
	}
	b.Run("same-org", func(b *testing.B) {
		for b.Loop() {
			query(b, orgs[0], "SELECT 1")
		}
	})
	b.Run("switching-org", func(b *testing.B) {
		i := 0
		for b.Loop() {
			i++
			query(b, orgs[i%2], "SELECT 1")
		}
	})
	b.Run("query-plain", func(b *testing.B) {
		for b.Loop() {
			query(b, orgs[0], "SELECT id, name FROM plain_items WHERE org_id = $1 ORDER BY id LIMIT 20", "org_1")
		}
	})
	b.Run("query-policy", func(b *testing.B) {
		for b.Loop() {
			query(b, orgs[0], "SELECT id, name FROM items WHERE org_id = $1 ORDER BY id LIMIT 20", "org_1")
		}
	})
}
