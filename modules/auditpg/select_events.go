package auditpg

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"apistock.dev/actor"
	"apistock.dev/audit"
	"apistock.dev/modules/postgres"
)

// Filter selects events to list. Empty fields match everything.
type Filter struct {
	ActorKind actor.Kind
	ActorID   string
	// Action matches one action exactly; ActionPrefix matches every action
	// starting with it, such as "jobs." or "auth.session.".
	Action       string
	ActionPrefix string
	ResourceType string
	ResourceID   string
	OrgID        string
	Outcome      audit.Outcome
	RequestID    string
	// From and To bound OccurredAt: From inclusive, To exclusive.
	From time.Time
	To   time.Time
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is Page.NextCursor from the previous page.
	Cursor string
}

// Page is a page of events, newest first.
type Page struct {
	Events []StoredEvent
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}

var actionPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)

// List returns events matching f, newest first. It returns an error wrapping
// [ErrInvalidFilter], or [ErrInvalidCursor].
func (s *Store) List(ctx context.Context, f Filter) (Page, error) {
	if err := f.validate(); err != nil {
		return Page{}, err
	}
	limit := f.Limit
	if limit == 0 {
		limit = 50
	}
	limit = min(max(limit, 1), 100)
	var before int64
	if f.Cursor != "" {
		id, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil || id < 1 {
			return Page{}, ErrInvalidCursor
		}
		before = id
	}

	events, err := selectEvents(ctx, s.pool, f, before, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("auditpg: list events: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	page := Page{Events: events}
	if len(events) > limit {
		page.Events = events[:limit]
		page.NextCursor = strconv.FormatInt(page.Events[limit-1].ID, 10)
	}
	return page, nil
}

func (f Filter) validate() error {
	switch f.Outcome {
	case "", audit.OutcomeSuccess, audit.OutcomeFailure, audit.OutcomeDenied:
	default:
		return fmt.Errorf("%w: outcome %q is not success, failure or denied", ErrInvalidFilter, f.Outcome)
	}
	if f.ActionPrefix != "" && !actionPrefixPattern.MatchString(f.ActionPrefix) {
		return fmt.Errorf("%w: action prefix must be lowercase letters, digits, underscores and dots", ErrInvalidFilter)
	}
	if !f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To) {
		return fmt.Errorf("%w: from must be before to", ErrInvalidFilter)
	}
	return nil
}

const selectEventsSQL = `SELECT ` + eventColumns + ` FROM audit_events`

// selectEvents appends one fixed condition per set filter, rather than
// matching empty parameters inside the SQL, so PostgreSQL plans each query
// with the matching index.
func selectEvents(ctx context.Context, db postgres.DBTX, f Filter, before int64, limit int) ([]StoredEvent, error) {
	where, args := f.conditions()
	if before > 0 {
		args = append(args, before)
		where = append(where, fmt.Sprintf("id < $%d", len(args)))
	}

	sql := selectEventsSQL
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit)
	sql += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanEvent)
}

// conditions returns one SQL condition per set filter field, numbered from
// $1, with their arguments. Limit and Cursor aren't conditions.
func (f Filter) conditions() (where []string, args []any) {
	add := func(condition string, arg any) {
		args = append(args, arg)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	if f.ActorKind != "" {
		add("actor_kind = $%d", string(f.ActorKind))
	}
	if f.ActorID != "" {
		add("actor_id = $%d", f.ActorID)
	}
	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.ActionPrefix != "" {
		add("starts_with(action, $%d)", f.ActionPrefix)
	}
	if f.ResourceType != "" {
		add("resource_type = $%d", f.ResourceType)
	}
	if f.ResourceID != "" {
		add("resource_id = $%d", f.ResourceID)
	}
	if f.OrgID != "" {
		add("org_id = $%d", f.OrgID)
	}
	if f.Outcome != "" {
		add("outcome = $%d", string(f.Outcome))
	}
	if f.RequestID != "" {
		add("request_id = $%d", f.RequestID)
	}
	if !f.From.IsZero() {
		add("occurred_at >= $%d", f.From)
	}
	if !f.To.IsZero() {
		add("occurred_at < $%d", f.To)
	}
	return where, args
}
