package auditpg_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/auditpg"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/requestid"
)

func newStore(t *testing.T, opts ...auditpg.Option) (*auditpg.Store, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.New(t, pgtest.WithMigrations(auditpg.Migrations))
	store, err := auditpg.NewStore(pool, opts...)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store, pool
}

// only lists every event and requires exactly one.
func only(t *testing.T, store *auditpg.Store) auditpg.StoredEvent {
	t.Helper()
	page, err := store.List(context.Background(), auditpg.Filter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("List() returned %d events, want 1", len(page.Events))
	}
	return page.Events[0]
}

func TestNewStoreValidates(t *testing.T) {
	if _, err := auditpg.NewStore(nil); err == nil {
		t.Error("NewStore(nil) error = nil, want pool required")
	}
}

func TestRecordFillsFromContext(t *testing.T) {
	store, _ := newStore(t)
	ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1", Label: "Ada", OrgID: "org_1"})
	ctx = requestid.With(ctx, "req_1")
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: trace.SpanID{1}}))

	before := time.Now().Add(-time.Second)
	err := store.Record(ctx, audit.Event{
		Action:       "project.created",
		ResourceType: "project",
		ResourceID:   "prj_1",
		Outcome:      audit.OutcomeSuccess,
		IP:           "203.0.113.7",
		UserAgent:    "curl/8",
		Metadata:     map[string]any{"name": "Apollo", "version": 3},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	got := only(t, store)
	if got.ActorKind != actor.KindUser || got.ActorID != "usr_1" || got.ActorLabel != "Ada" || got.OrgID != "org_1" {
		t.Errorf("actor fields = %q %q %q org %q, want user usr_1 Ada org_1", got.ActorKind, got.ActorID, got.ActorLabel, got.OrgID)
	}
	if got.RequestID != "req_1" || got.TraceID != traceID.String() {
		t.Errorf("RequestID, TraceID = %q, %q, want req_1, %s", got.RequestID, got.TraceID, traceID)
	}
	if got.Action != "project.created" || got.ResourceType != "project" || got.ResourceID != "prj_1" || got.Outcome != audit.OutcomeSuccess {
		t.Errorf("event = %+v", got.Event)
	}
	if got.IP != "203.0.113.7" || got.UserAgent != "curl/8" {
		t.Errorf("IP, UserAgent = %q, %q", got.IP, got.UserAgent)
	}
	if got.Metadata["name"] != "Apollo" || got.Metadata["version"] != float64(3) {
		t.Errorf("Metadata = %v", got.Metadata)
	}
	if got.OccurredAt.Before(before) || got.RecordedAt.Before(before) || got.ID < 1 {
		t.Errorf("ID, OccurredAt, RecordedAt = %d, %v, %v, want set to now", got.ID, got.OccurredAt, got.RecordedAt)
	}

	byID, err := store.Get(context.Background(), got.ID)
	if err != nil || byID.Action != got.Action || byID.Metadata["name"] != "Apollo" {
		t.Errorf("Get(%d) = %+v, %v", got.ID, byID, err)
	}
}

func TestRecordKeepsExplicitFieldsAndAnonymous(t *testing.T) {
	store, _ := newStore(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	err := store.Record(context.Background(), audit.Event{
		OccurredAt: at, Action: "auth.login.failed", Outcome: audit.OutcomeFailure,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	got := only(t, store)
	if got.ActorKind != actor.KindAnonymous || got.OrgID != "" || !got.OccurredAt.Equal(at) {
		t.Errorf("event = %+v, want anonymous, no org, OccurredAt %v", got.Event, at)
	}
	if got.Metadata == nil || len(got.Metadata) != 0 {
		t.Errorf("Metadata = %#v, want empty map", got.Metadata)
	}
}

func TestRecordRejectsInvalidEvents(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	for name, e := range map[string]audit.Event{
		"bad action":   {Action: "Login", Outcome: audit.OutcomeSuccess},
		"no outcome":   {Action: "auth.login.succeeded"},
		"long action":  {Action: "a." + strings.Repeat("b", 300), Outcome: audit.OutcomeSuccess},
		"unknown kind": {Action: "auth.login.succeeded", Outcome: "maybe"},
	} {
		if err := store.Record(ctx, e); err == nil {
			t.Errorf("%s: Record() error = nil, want invalid event", name)
		}
	}
	if page, _ := store.List(ctx, auditpg.Filter{}); len(page.Events) != 0 {
		t.Errorf("invalid events were stored: %+v", page.Events)
	}
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func TestRecordRedactsAndSanitizes(t *testing.T) {
	store, _ := newStore(t, auditpg.WithRedactedKeys("ssn"))
	err := store.Record(context.Background(), audit.Event{
		Action:     "auth.password.changed",
		Outcome:    audit.OutcomeSuccess,
		ActorLabel: "Ada\x00 Lovelace",
		IP:         "fe80::1%eth0",
		UserAgent:  strings.Repeat("u", 600),
		Metadata: map[string]any{
			"password":      "hunter2",
			"accessToken":   "tok",
			"Refresh-Token": "tok",
			"tokenizer":     "kept",
			"footprint":     "kept",
			"customer_ssn":  "123",
			// Plural, camelCase and newer names (security review OPS-4).
			"tokens":         []any{"a", "b"},
			"refresh_tokens": "tok",
			"sessionTokens":  "tok",
			"secrets":        "s",
			"client_secrets": "s",
			"passwords":      "p",
			"recovery_codes": []any{"c1"},
			"apiKeys":        "k",
			"totp":           "123456",
			"totp_code":      123456,
			"reset_code":     "123456",
			"jwt":            "eyJ",
			"bearer":         "b",
			"code":           "invalid_credentials",
			"status":         "kept",
			"secretary_note": "kept",
			"nested":         map[string]any{"client_secret": "s", "note": "a\x00b"},
			"list":           []any{map[string]any{"api_key": "k"}},
			"login":          credentials{Username: "ada", Password: "hunter2"},
		},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	got := only(t, store)
	m := got.Metadata
	for _, key := range []string{"password", "accessToken", "Refresh-Token", "customer_ssn",
		"tokens", "refresh_tokens", "sessionTokens", "secrets", "client_secrets", "passwords", "recovery_codes",
		"apiKeys", "totp", "totp_code", "reset_code", "jwt", "bearer"} {
		if m[key] != "[REDACTED]" {
			t.Errorf("Metadata[%q] = %v, want redacted", key, m[key])
		}
	}
	if m["tokenizer"] != "kept" || m["footprint"] != "kept" || m["status"] != "kept" || m["secretary_note"] != "kept" || m["code"] != "invalid_credentials" {
		t.Errorf("keys only resembling sensitive names were redacted: %v", m)
	}
	nested := m["nested"].(map[string]any)
	if nested["client_secret"] != "[REDACTED]" || nested["note"] != "ab" {
		t.Errorf("Metadata[nested] = %v", nested)
	}
	if item := m["list"].([]any)[0].(map[string]any); item["api_key"] != "[REDACTED]" {
		t.Errorf("Metadata[list] = %v", m["list"])
	}
	if login := m["login"].(map[string]any); login["password"] != "[REDACTED]" || login["username"] != "ada" {
		t.Errorf("Metadata[login] = %v, want struct fields redacted", login)
	}
	if got.ActorLabel != "Ada Lovelace" || got.IP != "fe80::1" || len(got.UserAgent) != 512 {
		t.Errorf("ActorLabel, IP, len(UserAgent) = %q, %q, %d", got.ActorLabel, got.IP, len(got.UserAgent))
	}
}

func TestRecordDropsUnusableMetadata(t *testing.T) {
	store, _ := newStore(t, auditpg.WithMaxMetadataBytes(64))
	ctx := context.Background()
	if err := store.Record(ctx, audit.Event{Action: "a.big", Outcome: audit.OutcomeSuccess, Metadata: map[string]any{"text": strings.Repeat("x", 100)}}); err != nil {
		t.Fatalf("Record(large metadata) error = %v", err)
	}
	if err := store.Record(ctx, audit.Event{Action: "a.func", Outcome: audit.OutcomeSuccess, Metadata: map[string]any{"fn": func() {}}, IP: "not an ip"}); err != nil {
		t.Fatalf("Record(unencodable metadata) error = %v", err)
	}
	page, err := store.List(ctx, auditpg.Filter{})
	if err != nil || len(page.Events) != 2 {
		t.Fatalf("List() = %+v, %v", page, err)
	}
	if got := page.Events[0]; got.Metadata["metadata_dropped"] != "not_json" || got.IP != "" {
		t.Errorf("unencodable event = %+v", got.Event)
	}
	if got := page.Events[1]; got.Metadata["metadata_dropped"] != "too_large" {
		t.Errorf("large event metadata = %v", got.Metadata)
	}
}

func TestRecordTxCommitsWithTheChange(t *testing.T) {
	store, pool := newStore(t)
	ctx := context.Background()
	event := audit.Event{Action: "project.deleted", Outcome: audit.OutcomeSuccess}

	errRollback := errors.New("change failed")
	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := store.RecordTx(ctx, tx, event); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("InTx() error = %v, want rollback", err)
	}
	if page, _ := store.List(ctx, auditpg.Filter{}); len(page.Events) != 0 {
		t.Fatalf("rolled back event was stored: %+v", page.Events)
	}

	err = postgres.InTx(ctx, pool, func(tx pgx.Tx) error { return store.RecordTx(ctx, tx, event) })
	if err != nil {
		t.Fatalf("InTx() error = %v", err)
	}
	only(t, store)
}

func TestEventsAreAppendOnly(t *testing.T) {
	store, pool := newStore(t)
	ctx := context.Background()
	if err := store.Record(ctx, audit.Event{Action: "project.created", Outcome: audit.OutcomeSuccess}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE audit_events SET outcome = 'failure'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE audit_events error = %v, want append-only rejection", err)
	}
	if tag, err := pool.Exec(ctx, "DELETE FROM audit_events"); err != nil || tag.RowsAffected() != 1 {
		t.Errorf("DELETE audit_events = %v, %v, want retention deletes allowed", tag, err)
	}
}

func TestListFiltersAndPages(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	user := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"})
	service := requestid.With(actor.With(context.Background(), actor.Actor{Kind: actor.KindService, ID: "ops-token"}), "req_ops")
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	events := []struct {
		ctx context.Context
		e   audit.Event
	}{
		{user, audit.Event{OccurredAt: t0, Action: "auth.session.created", Outcome: audit.OutcomeSuccess, ResourceType: "session", ResourceID: "ses_1"}},
		{user, audit.Event{OccurredAt: t0.Add(time.Hour), Action: "auth.session.revoked", Outcome: audit.OutcomeSuccess, ResourceType: "session", ResourceID: "ses_1"}},
		{user, audit.Event{OccurredAt: t0.Add(2 * time.Hour), Action: "auth.login.failed", Outcome: audit.OutcomeFailure, OrgID: "org_1"}},
		{service, audit.Event{OccurredAt: t0.Add(3 * time.Hour), Action: "settings.value.changed", Outcome: audit.OutcomeSuccess, ResourceType: "setting", ResourceID: "a.b"}},
		{service, audit.Event{OccurredAt: t0.Add(4 * time.Hour), Action: "jobs.run.cancelled", Outcome: audit.OutcomeDenied}},
	}
	for _, ev := range events {
		if err := store.Record(ev.ctx, ev.e); err != nil {
			t.Fatalf("Record(%s) error = %v", ev.e.Action, err)
		}
	}

	tests := []struct {
		name   string
		filter auditpg.Filter
		want   []string
	}{
		{"all, newest first", auditpg.Filter{}, []string{"jobs.run.cancelled", "settings.value.changed", "auth.login.failed", "auth.session.revoked", "auth.session.created"}},
		{"action", auditpg.Filter{Action: "auth.login.failed"}, []string{"auth.login.failed"}},
		{"action prefix", auditpg.Filter{ActionPrefix: "auth.session."}, []string{"auth.session.revoked", "auth.session.created"}},
		{"prefix underscore is literal", auditpg.Filter{ActionPrefix: "auth_"}, nil},
		{"actor", auditpg.Filter{ActorKind: actor.KindService, ActorID: "ops-token"}, []string{"jobs.run.cancelled", "settings.value.changed"}},
		{"resource", auditpg.Filter{ResourceType: "session", ResourceID: "ses_1"}, []string{"auth.session.revoked", "auth.session.created"}},
		{"org", auditpg.Filter{OrgID: "org_1"}, []string{"auth.login.failed"}},
		{"outcome", auditpg.Filter{Outcome: audit.OutcomeDenied}, []string{"jobs.run.cancelled"}},
		{"request", auditpg.Filter{RequestID: "req_ops"}, []string{"jobs.run.cancelled", "settings.value.changed"}},
		{"time range", auditpg.Filter{From: t0.Add(time.Hour), To: t0.Add(3 * time.Hour)}, []string{"auth.login.failed", "auth.session.revoked"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := store.List(ctx, tt.filter)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if got := actions(page.Events); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("List() actions = %v, want %v", got, tt.want)
			}
			if page.NextCursor != "" {
				t.Errorf("NextCursor = %q, want empty", page.NextCursor)
			}
		})
	}

	var pages [][]string
	f := auditpg.Filter{Limit: 2}
	for {
		page, err := store.List(ctx, f)
		if err != nil {
			t.Fatalf("List(cursor %q) error = %v", f.Cursor, err)
		}
		pages = append(pages, actions(page.Events))
		if page.NextCursor == "" {
			break
		}
		f.Cursor = page.NextCursor
	}
	if len(pages) != 3 || len(pages[0]) != 2 || len(pages[2]) != 1 || pages[2][0] != "auth.session.created" {
		t.Errorf("pages = %v, want 2, 2, 1 events ending with the oldest", pages)
	}
}

func actions(events []auditpg.StoredEvent) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.Action)
	}
	return out
}

func TestListAndGetErrors(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	now := time.Now()
	for name, f := range map[string]auditpg.Filter{
		"outcome":     {Outcome: "maybe"},
		"prefix":      {ActionPrefix: "Auth%"},
		"empty range": {From: now, To: now},
	} {
		if _, err := store.List(ctx, f); !errors.Is(err, auditpg.ErrInvalidFilter) {
			t.Errorf("%s: List() error = %v, want ErrInvalidFilter", name, err)
		}
	}
	for _, cursor := range []string{"abc", "0", "-5"} {
		if _, err := store.List(ctx, auditpg.Filter{Cursor: cursor}); !errors.Is(err, auditpg.ErrInvalidCursor) {
			t.Errorf("List(cursor %q) error = %v, want ErrInvalidCursor", cursor, err)
		}
	}
	if _, err := store.Get(ctx, 42); !errors.Is(err, auditpg.ErrEventNotFound) {
		t.Errorf("Get(42) error = %v, want ErrEventNotFound", err)
	}
}

func TestRecordConcurrently(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			errs <- store.Record(ctx, audit.Event{Action: "project.viewed", Outcome: audit.OutcomeSuccess})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Record() error = %v", err)
		}
	}
	page, err := store.List(ctx, auditpg.Filter{Limit: 100})
	if err != nil || len(page.Events) != 20 {
		t.Errorf("List() = %d events, %v, want 20", len(page.Events), err)
	}
}

// A listing no index serves can't hold a connection indefinitely (security
// review OPS-6): the query timeout cancels it.
func TestListAndStatsTimeOut(t *testing.T) {
	store, pool := newStore(t, auditpg.WithQueryTimeout(200*time.Millisecond))
	ctx := context.Background()

	// A lock held by another transaction stands in for a slow scan.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "LOCK TABLE audit_events IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 2)
	go func() {
		_, err := store.List(ctx, auditpg.Filter{Outcome: audit.OutcomeDenied, ActionPrefix: "zzz"})
		done <- err
	}()
	go func() {
		_, err := store.Stats(ctx, auditpg.StatsFilter{GroupBy: auditpg.StatsByOutcome})
		done <- err
	}()
	for range 2 {
		select {
		case err := <-done:
			if !errors.Is(err, auditpg.ErrQueryTimeout) {
				t.Errorf("List()/Stats() on a locked table error = %v, want ErrQueryTimeout", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("List()/Stats() still running after 10s, want the query timeout to cancel it")
		}
	}

	if _, err := auditpg.NewStore(pool, auditpg.WithQueryTimeout(0)); err == nil {
		t.Error("NewStore(WithQueryTimeout(0)) error = nil")
	}
}
