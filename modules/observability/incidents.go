package observability

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/actor"
	"gorbital.dev/modules/postgres"
)

// Severity is how bad an incident is, from sev1 (most severe) to sev4.
type Severity string

// Severities.
const (
	SeveritySev1 Severity = "sev1"
	SeveritySev2 Severity = "sev2"
	SeveritySev3 Severity = "sev3"
	SeveritySev4 Severity = "sev4"
)

// Status is where an incident is in its lifecycle.
type Status string

// Statuses. Open incidents move between the first three in any order;
// resolved is final.
const (
	StatusInvestigating Status = "investigating"
	StatusIdentified    Status = "identified"
	StatusMonitoring    Status = "monitoring"
	StatusResolved      Status = "resolved"
)

// Source is who opened an incident.
type Source string

// Sources.
const (
	SourceManual    Source = "manual"
	SourceAutomatic Source = "automatic"
)

// UpdateKind classifies a timeline update.
type UpdateKind string

// Update kinds.
const (
	UpdateOpened   UpdateKind = "opened"
	UpdateNote     UpdateKind = "update"
	UpdateResolved UpdateKind = "resolved"
	// UpdateRecovered and UpdateBreaching are added by detection when an
	// automatic incident's error rate recovers and when it is high again.
	UpdateRecovered UpdateKind = "recovered"
	UpdateBreaching UpdateKind = "breaching"
)

// Limits on incidents.
const (
	MaxTitleLength   = 200
	MaxSummaryLength = 5000
	MaxMessageLength = 5000
	// MaxIncidentUpdates is how many updates one incident keeps.
	MaxIncidentUpdates = 500
	// MaxStartedAge is how long before being opened an incident may have
	// started.
	MaxStartedAge = 90 * 24 * time.Hour
	// clockSkew is how far in the future a start time is accepted.
	clockSkew = time.Minute
)

// Incident is a period during which the application didn't work as it
// should.
type Incident struct {
	ID       int64
	Title    string
	Summary  string
	Severity Severity
	Status   Status
	Source   Source
	// StartedAt is when the incident began, which can be before it was
	// opened; ResolvedAt is set once it is resolved.
	StartedAt  time.Time
	ResolvedAt *time.Time
	// RecoveredAt is set on an open automatic incident while detection sees
	// the error rate recovered.
	RecoveredAt *time.Time
	// CreatedByKind and CreatedByID identify who opened it: a user, or the
	// system for automatic incidents.
	CreatedByKind actor.Kind
	CreatedByID   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// IncidentUpdate is one entry of an incident's timeline.
type IncidentUpdate struct {
	ID         int64
	IncidentID int64
	Kind       UpdateKind
	Message    string
	// Status and Severity are the incident's after the update.
	Status    Status
	Severity  Severity
	ActorKind actor.Kind
	ActorID   string
	CreatedAt time.Time
}

// NewIncident is an incident an operator opens.
type NewIncident struct {
	Title   string
	Summary string
	// Severity is required.
	Severity Severity
	// Status defaults to investigating; it can't be resolved.
	Status Status
	// StartedAt defaults to now; it can be up to [MaxStartedAge] earlier.
	StartedAt time.Time
	// Message is the first timeline update; default "Incident opened.".
	Message string
}

// IncidentChange is a timeline update, optionally changing the incident's
// status and severity.
type IncidentChange struct {
	Message string
	// Status and Severity are left unchanged when empty. Status can't be
	// resolved: use [Store.ResolveIncident].
	Status   Status
	Severity Severity
}

const incidentColumns = `id, title, summary, severity, status, source, started_at, resolved_at, recovered_at,
	created_by_kind, created_by_id, created_at, updated_at`

func (i *Incident) fields() []any {
	return []any{&i.ID, &i.Title, &i.Summary, &i.Severity, &i.Status, &i.Source, &i.StartedAt, &i.ResolvedAt, &i.RecoveredAt,
		&i.CreatedByKind, &i.CreatedByID, &i.CreatedAt, &i.UpdatedAt}
}

func (i Incident) utc() Incident {
	i.StartedAt, i.CreatedAt, i.UpdatedAt = i.StartedAt.UTC(), i.CreatedAt.UTC(), i.UpdatedAt.UTC()
	i.ResolvedAt, i.RecoveredAt = utcPtr(i.ResolvedAt), utcPtr(i.RecoveredAt)
	return i
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

const updateColumns = `id, incident_id, kind, message, status, severity, actor_kind, actor_id, created_at`

func (u *IncidentUpdate) fields() []any {
	return []any{&u.ID, &u.IncidentID, &u.Kind, &u.Message, &u.Status, &u.Severity, &u.ActorKind, &u.ActorID, &u.CreatedAt}
}

func scanUpdate(row pgx.CollectableRow) (IncidentUpdate, error) {
	var u IncidentUpdate
	err := row.Scan(u.fields()...)
	u.CreatedAt = u.CreatedAt.UTC()
	return u, err
}

const insertIncidentSQL = `
	INSERT INTO incidents (title, summary, severity, status, source, started_at, created_by_kind, created_by_id, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
	ON CONFLICT (source) WHERE source = 'automatic' AND status <> 'resolved' DO NOTHING
	RETURNING ` + incidentColumns

const insertUpdateSQL = `
	INSERT INTO incident_updates (incident_id, kind, message, status, severity, actor_kind, actor_id, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING ` + updateColumns

// OpenIncident opens an incident by the context's actor, with a first
// timeline update. It returns an error wrapping [ErrInvalidIncident] for a
// missing or out-of-bounds field.
func (s *Store) OpenIncident(ctx context.Context, n NewIncident) (Incident, IncidentUpdate, error) {
	now := s.now().UTC()
	n.Title, n.Summary, n.Message = strings.TrimSpace(n.Title), strings.TrimSpace(n.Summary), strings.TrimSpace(n.Message)
	if n.Status == "" {
		n.Status = StatusInvestigating
	}
	if n.StartedAt.IsZero() {
		n.StartedAt = now
	}
	if n.Message == "" {
		n.Message = "Incident opened."
	}
	if err := validateNew(n, now); err != nil {
		return Incident{}, IncidentUpdate{}, err
	}
	var (
		inc Incident
		upd IncidentUpdate
	)
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		inc, upd, err = openIncident(ctx, tx, n, SourceManual, actor.FromOrAnonymous(ctx), now)
		return err
	})
	return inc, upd, err
}

// openIncident inserts an incident and its opened update. An automatic
// incident while another is open returns pgx.ErrNoRows.
func openIncident(ctx context.Context, tx pgx.Tx, n NewIncident, source Source, by actor.Actor, now time.Time) (Incident, IncidentUpdate, error) {
	var inc Incident
	err := tx.QueryRow(ctx, insertIncidentSQL, n.Title, n.Summary, n.Severity, n.Status, source, n.StartedAt.UTC(),
		by.Kind, by.ID, now).Scan(inc.fields()...)
	if postgres.IsNoRows(err) {
		return Incident{}, IncidentUpdate{}, err
	}
	if err != nil {
		return Incident{}, IncidentUpdate{}, dbError("open incident", err)
	}
	upd, err := insertUpdate(ctx, tx, inc, UpdateOpened, n.Message, by, now)
	return inc.utc(), upd, err
}

func insertUpdate(ctx context.Context, tx pgx.Tx, inc Incident, kind UpdateKind, message string, by actor.Actor, now time.Time) (IncidentUpdate, error) {
	var upd IncidentUpdate
	err := tx.QueryRow(ctx, insertUpdateSQL, inc.ID, kind, message, inc.Status, inc.Severity, by.Kind, by.ID, now).Scan(upd.fields()...)
	if err != nil {
		return IncidentUpdate{}, dbError("add incident update", err)
	}
	upd.CreatedAt = upd.CreatedAt.UTC()
	return upd, nil
}

const selectIncidentForUpdateSQL = `SELECT ` + incidentColumns + `, (SELECT count(*) FROM incident_updates WHERE incident_id = $1)
	FROM incidents WHERE id = $1 FOR UPDATE`

const updateIncidentSQL = `
	UPDATE incidents SET status = $2, severity = $3, resolved_at = $4, recovered_at = $5, updated_at = $6
	WHERE id = $1
	RETURNING ` + incidentColumns

// UpdateIncident adds a timeline update to an open incident by the
// context's actor, changing its status and severity when the change sets
// them. It returns [ErrIncidentNotFound], [ErrIncidentResolved],
// [ErrTooManyUpdates], or an error wrapping [ErrInvalidIncident].
func (s *Store) UpdateIncident(ctx context.Context, id int64, c IncidentChange) (Incident, IncidentUpdate, error) {
	c.Message = strings.TrimSpace(c.Message)
	if err := validateChange(c); err != nil {
		return Incident{}, IncidentUpdate{}, err
	}
	return s.change(ctx, id, UpdateNote, c.Message, func(inc *Incident, _ time.Time) {
		if c.Status != "" {
			inc.Status = c.Status
		}
		if c.Severity != "" {
			inc.Severity = c.Severity
		}
	})
}

// ResolveIncident resolves an open incident now, by the context's actor,
// with a final timeline update. It returns [ErrIncidentNotFound],
// [ErrIncidentResolved], [ErrTooManyUpdates], or an error wrapping
// [ErrInvalidIncident] for a missing message.
func (s *Store) ResolveIncident(ctx context.Context, id int64, message string) (Incident, IncidentUpdate, error) {
	message = strings.TrimSpace(message)
	if err := validateMessage(message); err != nil {
		return Incident{}, IncidentUpdate{}, err
	}
	return s.change(ctx, id, UpdateResolved, message, func(inc *Incident, now time.Time) {
		inc.Status, inc.ResolvedAt, inc.RecoveredAt = StatusResolved, &now, nil
	})
}

// change locks an open incident, applies apply, stores it and adds an
// update.
func (s *Store) change(ctx context.Context, id int64, kind UpdateKind, message string, apply func(*Incident, time.Time)) (Incident, IncidentUpdate, error) {
	now := s.now().UTC()
	by := actor.FromOrAnonymous(ctx)
	var (
		inc Incident
		upd IncidentUpdate
	)
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		if inc, err = lockOpenIncident(ctx, tx, id, MaxIncidentUpdates); err != nil {
			return err
		}
		apply(&inc, now)
		if inc, err = storeIncident(ctx, tx, inc, now); err != nil {
			return err
		}
		upd, err = insertUpdate(ctx, tx, inc, kind, message, by, now)
		return err
	})
	return inc, upd, err
}

// lockOpenIncident locks an incident that is open and has fewer than
// maxUpdates updates.
func lockOpenIncident(ctx context.Context, tx pgx.Tx, id int64, maxUpdates int) (Incident, error) {
	var (
		inc     Incident
		updates int
	)
	err := tx.QueryRow(ctx, selectIncidentForUpdateSQL, id).Scan(append(inc.fields(), &updates)...)
	switch {
	case postgres.IsNoRows(err):
		return Incident{}, ErrIncidentNotFound
	case err != nil:
		return Incident{}, dbError("lock incident", err)
	case inc.Status == StatusResolved:
		return Incident{}, ErrIncidentResolved
	case updates >= maxUpdates:
		return Incident{}, ErrTooManyUpdates
	}
	return inc.utc(), nil
}

func storeIncident(ctx context.Context, tx pgx.Tx, inc Incident, now time.Time) (Incident, error) {
	var out Incident
	err := tx.QueryRow(ctx, updateIncidentSQL, inc.ID, inc.Status, inc.Severity, inc.ResolvedAt, inc.RecoveredAt, now).Scan(out.fields()...)
	if err != nil {
		return Incident{}, dbError("update incident", err)
	}
	return out.utc(), nil
}

const selectIncidentSQL = `SELECT ` + incidentColumns + ` FROM incidents WHERE id = $1`

// Incident returns an incident, or [ErrIncidentNotFound].
func (s *Store) Incident(ctx context.Context, id int64) (Incident, error) {
	var inc Incident
	err := s.pool.QueryRow(ctx, selectIncidentSQL, id).Scan(inc.fields()...)
	switch {
	case postgres.IsNoRows(err):
		return Incident{}, ErrIncidentNotFound
	case err != nil:
		return Incident{}, dbError("read incident", err)
	}
	return inc.utc(), nil
}

const selectUpdatesSQL = `SELECT ` + updateColumns + ` FROM incident_updates WHERE incident_id = $1 ORDER BY id LIMIT $2`

// IncidentUpdates returns an incident's timeline, oldest first: at most
// [MaxIncidentUpdates] updates. An unknown incident has none.
func (s *Store) IncidentUpdates(ctx context.Context, id int64) ([]IncidentUpdate, error) {
	rows, err := s.pool.Query(ctx, selectUpdatesSQL, id, MaxIncidentUpdates)
	if err != nil {
		return nil, dbError("list incident updates", err)
	}
	updates, err := pgx.CollectRows(rows, scanUpdate)
	if err != nil {
		return nil, dbError("list incident updates", err)
	}
	return updates, nil
}

// IncidentFilter selects incidents. Empty fields match everything.
type IncidentFilter struct {
	// Open selects incidents that aren't resolved; Status one status.
	Open     bool
	Status   Status
	Severity Severity
	Source   Source
	// StartedFrom and StartedTo bound StartedAt: From inclusive, To
	// exclusive.
	StartedFrom time.Time
	StartedTo   time.Time
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is IncidentPage.NextCursor from the previous page.
	Cursor string
}

// IncidentPage is a page of incidents, most recently opened first.
type IncidentPage struct {
	Incidents []Incident
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}

const selectIncidentsSQL = `SELECT ` + incidentColumns + ` FROM incidents`

// Incidents returns incidents matching f, most recently opened first. It
// returns [ErrInvalidCursor] for a cursor it didn't return, and an error
// wrapping [ErrInvalidIncident] for an unknown status, severity or source.
func (s *Store) Incidents(ctx context.Context, f IncidentFilter) (IncidentPage, error) {
	if err := validateFilter(f); err != nil {
		return IncidentPage{}, err
	}
	limit := f.Limit
	switch {
	case limit <= 0:
		limit = 50
	case limit > 100:
		limit = 100
	}
	var (
		where []string
		args  []any
	)
	add := func(condition string, arg any) {
		args = append(args, arg)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	if f.Open {
		where = append(where, "status <> 'resolved'")
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.Severity != "" {
		add("severity = $%d", f.Severity)
	}
	if f.Source != "" {
		add("source = $%d", f.Source)
	}
	if !f.StartedFrom.IsZero() {
		add("started_at >= $%d", f.StartedFrom.UTC())
	}
	if !f.StartedTo.IsZero() {
		add("started_at < $%d", f.StartedTo.UTC())
	}
	if f.Cursor != "" {
		id, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil || id < 1 {
			return IncidentPage{}, ErrInvalidCursor
		}
		add("id < $%d", id)
	}
	sql := selectIncidentsSQL
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit+1)
	sql += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return IncidentPage{}, dbError("list incidents", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Incident, error) {
		var inc Incident
		err := row.Scan(inc.fields()...)
		return inc.utc(), err
	})
	if err != nil {
		return IncidentPage{}, dbError("list incidents", err)
	}
	page := IncidentPage{Incidents: list}
	if len(list) > limit {
		page.Incidents = list[:limit]
		page.NextCursor = strconv.FormatInt(page.Incidents[limit-1].ID, 10)
	}
	return page, nil
}

var (
	severities = []Severity{SeveritySev1, SeveritySev2, SeveritySev3, SeveritySev4}
	statuses   = []Status{StatusInvestigating, StatusIdentified, StatusMonitoring, StatusResolved}
	sources    = []Source{SourceManual, SourceAutomatic}
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidIncident}, args...)...)
}

func validateNew(n NewIncident, now time.Time) error {
	switch {
	case n.Title == "" || utf8.RuneCountInString(n.Title) > MaxTitleLength || strings.ContainsAny(n.Title, "\r\n"):
		return invalid("title must be one line of 1 to %d characters", MaxTitleLength)
	case utf8.RuneCountInString(n.Summary) > MaxSummaryLength:
		return invalid("summary must be at most %d characters", MaxSummaryLength)
	case !slices.Contains(severities, n.Severity):
		return invalid("severity must be sev1, sev2, sev3 or sev4")
	case n.Status == StatusResolved || !slices.Contains(statuses, n.Status):
		return invalid("status must be investigating, identified or monitoring")
	case n.StartedAt.After(now.Add(clockSkew)) || n.StartedAt.Before(now.Add(-MaxStartedAge)):
		return invalid("started_at must be in the last 90 days, not in the future")
	}
	return validateMessage(n.Message)
}

func validateChange(c IncidentChange) error {
	switch {
	case c.Status != "" && (c.Status == StatusResolved || !slices.Contains(statuses, c.Status)):
		return invalid("status must be investigating, identified or monitoring; resolve an incident to close it")
	case c.Severity != "" && !slices.Contains(severities, c.Severity):
		return invalid("severity must be sev1, sev2, sev3 or sev4")
	}
	return validateMessage(c.Message)
}

func validateMessage(m string) error {
	if m == "" || utf8.RuneCountInString(m) > MaxMessageLength {
		return invalid("message must be 1 to %d characters", MaxMessageLength)
	}
	return nil
}

func validateFilter(f IncidentFilter) error {
	switch {
	case f.Status != "" && !slices.Contains(statuses, f.Status):
		return invalid("unknown status %q", f.Status)
	case f.Severity != "" && !slices.Contains(severities, f.Severity):
		return invalid("unknown severity %q", f.Severity)
	case f.Source != "" && !slices.Contains(sources, f.Source):
		return invalid("unknown source %q", f.Source)
	}
	return nil
}
