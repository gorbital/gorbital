package pgmeta

import (
	"context"
	"strings"
	"testing"
)

func TestStatsStatementsAndAdvice(t *testing.T) {
	c, schema := testClient(t)
	ctx := context.Background()
	parent, child := ident(schema, "adv_parent"), ident(schema, "adv_child")
	_, err := c.pool.Exec(ctx, `DROP TABLE IF EXISTS public.adv_child, public.adv_parent;
		CREATE TABLE `+parent+` (id bigint PRIMARY KEY);
		CREATE TABLE `+child+` (id bigint PRIMARY KEY, parent_id bigint NOT NULL REFERENCES `+parent+`(id), note text);
		INSERT INTO `+parent+` VALUES (1);
		INSERT INTO `+child+` (id, parent_id) SELECT g, 1 FROM generate_series(1, 2000) g;`)
	if err != nil {
		t.Fatal(err)
	}

	st, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if !strings.HasPrefix(st.Version, "PostgreSQL") || st.MaxConnections < 1 || st.Connections < 1 || st.Size <= 0 || st.CacheHitRatio < 0 || st.CacheHitRatio > 1 {
		t.Errorf("stats = %+v", st)
	}
	found := false
	for _, tbl := range st.Tables {
		if tbl.Schema == schema && tbl.Name == "adv_child" && tbl.Total > 0 && tbl.Indexes > 0 {
			found = true
		}
	}
	if !found || st.Locks == nil || st.LongRunning == nil || st.Clients == nil {
		t.Errorf("stats tables/locks = %+v", st)
	}

	adv, err := c.Advise(ctx)
	if err != nil {
		t.Fatalf("Advise() error = %v", err)
	}
	var fk *IndexAdvice
	for i := range adv.MissingFKIndexes {
		if adv.MissingFKIndexes[i].Schema == schema && adv.MissingFKIndexes[i].Table == "adv_child" {
			fk = &adv.MissingFKIndexes[i]
		}
	}
	if fk == nil || fk.Columns[0] != "parent_id" || !strings.Contains(fk.SQL, `CREATE INDEX "adv_child_parent_id_idx" ON `+ident(schema, "adv_child")+` ("parent_id");`) {
		t.Errorf("missing FK index advice = %+v", adv.MissingFKIndexes)
	}
	if adv.UnusedIndexes == nil || adv.SeqScanned == nil || adv.DeadRows == nil {
		t.Errorf("advice lists are nil: %+v", adv)
	}

	// Statements: available or not, the answer says which and why.
	stmts, err := c.Statements(ctx, "calls", 10)
	if err != nil {
		t.Fatalf("Statements() error = %v", err)
	}
	if stmts.Sort != "calls" || (!stmts.Available && stmts.Reason == "") || stmts.Statements == nil {
		t.Errorf("statements = %+v", stmts)
	}
	if _, err := c.Statements(ctx, "nope", 10); err == nil {
		t.Error("an unknown sort was accepted")
	}
	if stmts.Available {
		if err := c.ResetStatements(ctx); err != nil {
			t.Errorf("ResetStatements() error = %v", err)
		}
	} else if err := c.ResetStatements(ctx); err == nil {
		t.Error("ResetStatements() without the extension didn't fail")
	}
}
