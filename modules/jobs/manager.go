package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/requestid"
)

// DefaultResyncInterval is how often a running [Manager] reloads every
// override, covering notifications lost while disconnected.
const DefaultResyncInterval = 5 * time.Minute

// notifyChannel carries the name of each changed job definition.
const notifyChannel = "gorbital_jobs"

// Manager is the admin-panel backend for jobs (ADR-0033): it stores
// operator overrides of job definitions, keeps every instance's definitions
// and schedules current, and inspects and controls runs and queues. It is an
// app.Runner. It is safe for concurrent use.
type Manager struct {
	pool     *pgxpool.Pool
	client   *Client
	defs     *Definitions
	recorder audit.Recorder
	logger   *slog.Logger
	resync   time.Duration
	now      func() time.Time
}

// A ManagerOption configures [NewManager].
type ManagerOption interface{ applyManager(*Manager) }

type managerOptionFunc func(*Manager)

func (f managerOptionFunc) applyManager(m *Manager) { f(m) }

// WithManagerLogger sets the logger for listener and invalid-override
// warnings. Default: discard.
func WithManagerLogger(logger *slog.Logger) ManagerOption {
	return managerOptionFunc(func(m *Manager) { m.logger = logger })
}

// WithResyncInterval sets how often [Manager.Run] reloads every override.
// Default: [DefaultResyncInterval].
func WithResyncInterval(d time.Duration) ManagerOption {
	return managerOptionFunc(func(m *Manager) { m.resync = d })
}

// NewManager loads stored overrides for client's definitions and returns
// the manager. Changes are recorded as audit events through recorder. The
// tables come from [Migrations].
func NewManager(ctx context.Context, pool *pgxpool.Pool, client *Client, recorder audit.Recorder, opts ...ManagerOption) (*Manager, error) {
	m := &Manager{
		pool:     pool,
		client:   client,
		recorder: recorder,
		logger:   slog.New(slog.DiscardHandler),
		resync:   DefaultResyncInterval,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt.applyManager(m)
	}
	var errs []error
	if pool == nil {
		errs = append(errs, errors.New("pool is required"))
	}
	if client == nil || client.defs == nil {
		errs = append(errs, errors.New("a client built with WithDefinitions is required"))
	} else {
		m.defs = client.defs
	}
	if recorder == nil {
		errs = append(errs, errors.New("audit recorder is required"))
	}
	if m.logger == nil {
		errs = append(errs, errors.New("logger must not be nil"))
	}
	if m.resync <= 0 {
		errs = append(errs, errors.New("resync interval must be positive"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("jobs: invalid manager: %w", err)
	}
	if err := m.Reload(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// Reload reads every override from the database and reapplies schedules.
func (m *Manager) Reload(ctx context.Context) error {
	rows, err := selectDefinitions(ctx, m.pool)
	if err != nil {
		return fmt.Errorf("jobs: load definitions: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	loaded := make(map[string]override, len(rows))
	for _, row := range rows {
		if d, ok := m.defs.lookup(row.name); ok {
			loaded[row.name] = m.checked(ctx, d, row.override())
		}
	}
	m.defs.replaceOverrides(loaded)
	return m.client.applySchedules()
}

func (m *Manager) reloadDefinition(ctx context.Context, name string) error {
	d, ok := m.defs.lookup(name)
	if !ok {
		return nil
	}
	row, found, err := selectDefinition(ctx, m.pool, name, false)
	if err != nil || !found {
		return err
	}
	m.defs.applyOverride(name, m.checked(ctx, d, row.override()))
	return m.client.applySchedules()
}

// checked flags an override whose merged configuration is out of bounds.
func (m *Manager) checked(ctx context.Context, d *definition, o override) override {
	if err := o.apply(d.defaults).Validate(); err != nil {
		o.invalid = true
		m.logger.WarnContext(ctx, "stored job definition override is invalid; using code defaults",
			"definition", d.name, "version", o.version, "reason", err.Error())
	}
	return o
}

// DefinitionView is a job definition's configuration and status, for the
// admin panel.
type DefinitionView struct {
	Name        string
	Description string
	// Config is the effective configuration; Defaults is the code's.
	Config   Config
	Defaults Config
	// Modified reports a valid override; InvalidOverride one that no longer
	// passes validation, so the defaults apply.
	Modified        bool
	InvalidOverride bool
	// Version increases with every change; pass it back to change the
	// definition. It is 0 for a definition never changed.
	Version   int64
	UpdatedAt time.Time
	UpdatedBy string
	// NextRunAt is the next scheduled time, approximately; zero when the job
	// is disabled or has no schedule.
	NextRunAt time.Time
	// LastRun is the most recent job of this definition, if any.
	LastRun *JobRun
}

// Definitions returns every definition in declaration order.
func (m *Manager) Definitions(ctx context.Context) ([]DefinitionView, error) {
	defs := m.defs.list()
	views := make([]DefinitionView, 0, len(defs))
	for _, d := range defs {
		v, err := m.view(ctx, d)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

// Scheduled returns enabled definitions with a schedule, soonest first.
func (m *Manager) Scheduled(ctx context.Context) ([]DefinitionView, error) {
	all, err := m.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	scheduled := all[:0]
	for _, v := range all {
		if !v.NextRunAt.IsZero() {
			scheduled = append(scheduled, v)
		}
	}
	sortByNextRun(scheduled)
	return scheduled, nil
}

// Definition returns one definition, or [ErrUnknownDefinition].
func (m *Manager) Definition(ctx context.Context, name string) (DefinitionView, error) {
	d, ok := m.defs.lookup(name)
	if !ok {
		return DefinitionView{}, ErrUnknownDefinition
	}
	return m.view(ctx, d)
}

func (m *Manager) view(ctx context.Context, d *definition) (DefinitionView, error) {
	v := DefinitionView{
		Name:        d.name,
		Description: d.description,
		Config:      m.defs.effective(d),
		Defaults:    d.defaults,
	}
	if o, ok := m.defs.override(d.name); ok {
		v.Version, v.UpdatedAt, v.UpdatedBy = o.version, o.updatedAt, o.updatedBy
		v.InvalidOverride = o.invalid
		v.Modified = !o.invalid && !o.isEmpty()
	}
	if v.Config.Enabled {
		if schedule, err := parseSchedule(v.Config.Schedule); err == nil && schedule != nil {
			v.NextRunAt = schedule.Next(m.now())
		}
	}
	last, err := m.lastRun(ctx, d.name)
	if err != nil {
		return DefinitionView{}, err
	}
	v.LastRun = last
	return v, nil
}

// ConfigPatch changes selected fields; nil fields keep their current value.
// Setting a field to its code default removes the override for that field.
type ConfigPatch struct {
	Enabled     *bool
	Schedule    *string
	Timeout     *time.Duration
	MaxAttempts *int
	Queue       *string
	Priority    *int
}

// Change carries what a caller supplies with a change. The actor comes from
// the context.
type Change struct {
	// Version is the DefinitionView.Version the caller last read.
	Version int64
	// Reason explains the change; required to disable or reschedule a job,
	// or to change its timeout, max attempts or queue.
	Reason string
}

// Update applies patch to name's configuration. The change reaches every
// instance within moments; a new schedule takes effect on the leader. It
// returns [ErrUnknownDefinition], an [*InvalidConfigError], [ErrUnknownQueue],
// [ErrReasonRequired], [ErrActorRequired] or [ErrVersionConflict].
func (m *Manager) Update(ctx context.Context, name string, patch ConfigPatch, change Change) (DefinitionView, error) {
	d, ok := m.defs.lookup(name)
	if !ok {
		return DefinitionView{}, ErrUnknownDefinition
	}
	return m.write(ctx, d, "updated", func(current override) override { return current.patched(patch, d.defaults) }, change)
}

// Reset returns name to its code defaults. It returns the same errors as
// [Manager.Update].
func (m *Manager) Reset(ctx context.Context, name string, change Change) (DefinitionView, error) {
	d, ok := m.defs.lookup(name)
	if !ok {
		return DefinitionView{}, ErrUnknownDefinition
	}
	return m.write(ctx, d, "reset", func(override) override { return override{} }, change)
}

func (m *Manager) write(ctx context.Context, d *definition, action string, next func(override) override, change Change) (DefinitionView, error) {
	a, err := requireActor(ctx)
	if err != nil {
		return DefinitionView{}, err
	}
	reason := strings.TrimSpace(change.Reason)

	var (
		result  definitionRow
		changed bool
	)
	err = pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		row, exists, err := selectDefinition(ctx, tx, d.name, true)
		if err != nil {
			return err
		}
		if change.Version != row.version {
			return ErrVersionConflict
		}
		current := override{}
		if exists {
			current = row.override()
		}
		updated := next(current)
		// Compare field values, not pointers: fieldsJSON is canonical.
		if bytes.Equal(updated.fieldsJSON(), current.fieldsJSON()) {
			result = row
			return nil
		}

		before, after := current.apply(d.defaults), updated.apply(d.defaults)
		if err := after.Validate(); err != nil {
			return &InvalidConfigError{Name: d.name, Reason: err.Error()}
		}
		if after.Queue != before.Queue && after.Queue != d.defaults.Queue {
			if err := m.requireActiveQueue(ctx, after.Queue); err != nil {
				return err
			}
		}
		if needsReason(before, after) && reason == "" {
			return ErrReasonRequired
		}

		result, err = writeDefinition(ctx, tx, d.name, updated, exists, a.ID)
		if err != nil {
			return err
		}
		err = insertDefinitionHistory(ctx, tx, historyRow{
			name:      d.name,
			action:    action,
			oldConfig: current.fieldsJSON(),
			newConfig: updated.fieldsJSON(),
			version:   result.version,
			reason:    reason,
			actorKind: string(a.Kind),
			actorID:   a.ID,
			requestID: requestid.From(ctx),
		})
		if err != nil {
			return err
		}
		changed = true
		return notifyDefinition(ctx, tx, d.name)
	})
	if err != nil {
		var invalid *InvalidConfigError
		if errors.As(err, &invalid) || errors.Is(err, ErrVersionConflict) || errors.Is(err, ErrReasonRequired) || errors.Is(err, ErrUnknownQueue) {
			return DefinitionView{}, err
		}
		return DefinitionView{}, fmt.Errorf("jobs: save definition %s: %v", d.name, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	if changed {
		m.defs.applyOverride(d.name, m.checked(ctx, d, result.override()))
		if err := m.client.applySchedules(); err != nil {
			m.logger.ErrorContext(ctx, "apply job schedules", "definition", d.name, "err", err)
		}
		m.audit(ctx, audit.Event{
			Action:       "jobs.definition.changed",
			ResourceType: "job_definition",
			ResourceID:   d.name,
			Metadata:     map[string]any{"version": result.version, "action": action, "reason": reason},
		})
	}
	return m.view(ctx, d)
}

// needsReason reports a change that can stop a job doing its work, so it
// needs a reason in the history (threat 24): disabling or rescheduling it,
// or changing its timeout (a 1-second timeout fails every run), max attempts
// or queue. Enabling a job and changing its priority don't.
func needsReason(before, after Config) bool {
	return (before.Enabled && !after.Enabled) ||
		(after.Enabled && before.Schedule != after.Schedule) ||
		before.Timeout != after.Timeout ||
		before.MaxAttempts != after.MaxAttempts ||
		before.Queue != after.Queue
}

// patched applies patch, dropping fields equal to the code default.
func (o override) patched(p ConfigPatch, defaults Config) override {
	o.enabled = patchField(o.enabled, p.Enabled, defaults.Enabled)
	o.schedule = patchField(o.schedule, p.Schedule, defaults.Schedule)
	o.timeout = patchField(o.timeout, p.Timeout, defaults.Timeout)
	o.maxAttempts = patchField(o.maxAttempts, p.MaxAttempts, defaults.MaxAttempts)
	o.queue = patchField(o.queue, p.Queue, defaults.Queue)
	o.priority = patchField(o.priority, p.Priority, defaults.Priority)
	return o
}

func patchField[T comparable](current, patch *T, def T) *T {
	if patch == nil {
		return current
	}
	if *patch == def {
		return nil
	}
	v := *patch
	return &v
}

// DefinitionChange is one change to a job definition. OldConfig and NewConfig
// are the overridden fields as JSON objects; {} means the code defaults.
type DefinitionChange struct {
	ID        int64
	Name      string
	Action    string
	OldConfig json.RawMessage
	NewConfig json.RawMessage
	Version   int64
	Reason    string
	ActorKind string
	ActorID   string
	RequestID string
	ChangedAt time.Time
}

// History returns changes to name, newest first. For the next page, pass the
// last entry's ID as before (0 starts from the newest). limit is clamped to
// 1–100.
func (m *Manager) History(ctx context.Context, name string, before int64, limit int) ([]DefinitionChange, error) {
	if _, ok := m.defs.lookup(name); !ok {
		return nil, ErrUnknownDefinition
	}
	changes, err := selectDefinitionHistory(ctx, m.pool, name, before, min(max(limit, 1), 100))
	if err != nil {
		return nil, fmt.Errorf("jobs: definition history %s: %v", name, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	return changes, nil
}

func requireActor(ctx context.Context) (actor.Actor, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind == actor.KindAnonymous || a.ID == "" {
		return actor.Actor{}, ErrActorRequired
	}
	return a, nil
}

// audit records an event after the change is committed; a failed audit
// write is logged, not returned.
func (m *Manager) audit(ctx context.Context, e audit.Event) {
	e.Outcome = audit.OutcomeSuccess
	if err := m.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		m.logger.ErrorContext(ctx, "record jobs audit event", "action", e.Action, "resource_id", e.ResourceID, "err", err)
	}
}
