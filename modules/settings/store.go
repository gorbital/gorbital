package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/requestid"
)

// DefaultResyncInterval is how often a running [Store] reloads every value,
// covering notifications lost while disconnected.
const DefaultResyncInterval = 5 * time.Minute

// notifyChannel carries the key of each changed setting.
const notifyChannel = "gorbital_settings"

// dbtx is the query subset of *pgxpool.Pool and pgx.Tx the store uses.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store persists settings in PostgreSQL and keeps a [Registry] current. It
// is an app.Runner: run it so changes from other instances arrive. It is
// safe for concurrent use.
type Store struct {
	pool     *pgxpool.Pool
	reg      *Registry
	recorder audit.Recorder
	logger   *slog.Logger
	resync   time.Duration
}

// A StoreOption configures [NewStore].
type StoreOption interface{ applyStore(*Store) }

type storeOptionFunc func(*Store)

func (f storeOptionFunc) applyStore(s *Store) { f(s) }

// WithLogger sets the logger for listener and invalid-value warnings.
// Default: discard.
func WithLogger(logger *slog.Logger) StoreOption {
	return storeOptionFunc(func(s *Store) { s.logger = logger })
}

// WithResyncInterval sets how often [Store.Run] reloads every value.
// Default: [DefaultResyncInterval].
func WithResyncInterval(d time.Duration) StoreOption {
	return storeOptionFunc(func(s *Store) { s.resync = d })
}

// NewStore loads every stored value into reg and returns the store. After
// NewStore, reg accepts no new declarations. Changes are recorded as
// "settings.value.changed" audit events through recorder.
//
// The settings tables come from [Migrations]; apply them first.
func NewStore(ctx context.Context, pool *pgxpool.Pool, reg *Registry, recorder audit.Recorder, opts ...StoreOption) (*Store, error) {
	s := &Store{
		pool:     pool,
		reg:      reg,
		recorder: recorder,
		logger:   slog.New(slog.DiscardHandler),
		resync:   DefaultResyncInterval,
	}
	for _, opt := range opts {
		opt.applyStore(s)
	}
	var errs []error
	if pool == nil {
		errs = append(errs, errors.New("pool is required"))
	}
	if reg == nil {
		errs = append(errs, errors.New("registry is required"))
	}
	if recorder == nil {
		errs = append(errs, errors.New("audit recorder is required"))
	}
	if s.logger == nil {
		errs = append(errs, errors.New("logger must not be nil"))
	}
	if s.resync <= 0 {
		errs = append(errs, errors.New("resync interval must be positive"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("settings: invalid store: %w", err)
	}

	reg.freeze()
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload reads every stored value from the database, organisation values
// included.
func (s *Store) Reload(ctx context.Context) error {
	since := s.reg.currentOrgSeq()
	rows, err := selectValues(ctx, s.pool)
	if err != nil {
		return fmt.Errorf("settings: load: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	loaded := make(map[string]stored, len(rows))
	var unknown []string
	for _, row := range rows {
		d, ok := s.reg.lookup(row.key)
		if !ok {
			unknown = append(unknown, row.key)
			continue
		}
		loaded[row.key] = s.decodeRow(ctx, d, row)
	}
	s.reg.replaceAll(loaded, unknown)
	return s.reloadOrgs(ctx, since)
}

// reloadKey reloads the value a notification names: a key, or a key, a space
// and an organisation ID.
func (s *Store) reloadKey(ctx context.Context, payload string) error {
	key, orgID, _ := strings.Cut(payload, " ")
	d, ok := s.reg.lookup(key)
	if !ok {
		return nil
	}
	if orgID != "" {
		return s.reloadOrgKey(ctx, d, orgID)
	}
	row, found, err := selectValue(ctx, s.pool, key)
	if err != nil || !found {
		return err
	}
	s.reg.apply(key, s.decodeRow(ctx, d, row))
	return nil
}

// decodeRow converts a row, logging a stored value that fails validation.
func (s *Store) decodeRow(ctx context.Context, d *definition, row valueRow) stored {
	st, err := toStored(d, row)
	if err != nil {
		s.logger.WarnContext(ctx, "stored setting value is invalid; using the default",
			"setting", d.key, "org_id", row.orgID, "version", row.version, "reason", err.Error())
	}
	return st
}

// toStored converts a row. A value that fails validation is marked invalid,
// and the error says why.
func toStored(d *definition, row valueRow) (stored, error) {
	st := stored{version: row.version, updatedAt: row.updatedAt, updatedBy: row.updatedBy}
	if row.value == nil {
		return st, nil
	}
	v, err := d.parse(row.value)
	if err != nil {
		st.invalid = true
		return st, err
	}
	st.value = v
	return st, nil
}

// UnknownKeys returns stored keys that no declaration matches, for example
// settings removed from the code. Their rows are kept and ignored.
func (s *Store) UnknownKeys() []string {
	if keys := s.reg.unknown.Load(); keys != nil {
		return append([]string(nil), (*keys)...)
	}
	return nil
}

// View is a setting's declaration and current state, for operator APIs.
type View struct {
	Key         string
	Kind        Kind
	Group       string
	Description string

	// Value is the effective value as JSON; Default is the declared default.
	Value   json.RawMessage
	Default json.RawMessage
	// Modified reports whether a valid stored value overrides the default,
	// or, in a view for an organisation, the platform value.
	Modified bool
	// InvalidStoredValue reports a stored value that fails validation, so
	// the default is in effect.
	InvalidStoredValue bool

	// Version increases with every change; pass it back to change the
	// setting. It is 0 for a setting that has never been changed. In a view
	// for an organisation, it is the version of the organisation's value.
	Version   int64
	UpdatedAt time.Time
	UpdatedBy string

	ReasonRequired  bool
	RestartRequired bool
	// RestartPending reports a restart-required setting changed since this
	// instance started.
	RestartPending bool

	// Constraints summarises validation, such as min, max, one_of, max_len,
	// max_items and format. Treat it as read-only.
	Constraints map[string]any

	// OrgOverridable reports a setting declared with [OrgOverridable].
	OrgOverridable bool
	// OrgID is the organisation of a view returned by [Store.ListForOrg],
	// [Store.GetForOrg], [Store.SetForOrg], [Store.ResetForOrg] or
	// [Store.Overrides]; empty for the platform-wide value.
	OrgID string
	// PlatformValue, in a view for an organisation, is the value the
	// organisation gets without its own: the effective platform-wide value.
	PlatformValue json.RawMessage
}

// List returns every declared setting in declaration order. It reads memory
// only.
func (s *Store) List() []View {
	defs := s.reg.definitions()
	views := make([]View, len(defs))
	for i, d := range defs {
		views[i] = s.view(d)
	}
	return views
}

// Get returns one setting, or [ErrUnknownSetting].
func (s *Store) Get(key string) (View, error) {
	d, ok := s.reg.lookup(key)
	if !ok {
		return View{}, ErrUnknownSetting
	}
	return s.view(d), nil
}

func (s *Store) view(d *definition) View {
	group := d.group
	if group == "" {
		group, _, _ = strings.Cut(d.key, ".")
	}
	def := d.encode(d.def)
	v := View{
		Key:             d.key,
		Kind:            d.kind,
		Group:           group,
		Description:     d.description,
		Value:           def,
		Default:         def,
		ReasonRequired:  d.reasonRequired,
		RestartRequired: d.restartRequired,
		Constraints:     maps.Clone(d.constraints),
		OrgOverridable:  d.orgOverridable,
	}
	if live := s.reg.live.Load(); live != nil {
		if st, ok := (*live)[d.key]; ok {
			v.Version, v.UpdatedAt, v.UpdatedBy, v.InvalidStoredValue = st.version, st.updatedAt, st.updatedBy, st.invalid
			if st.value != nil {
				v.Value, v.Modified = d.encode(st.value), true
			}
		}
	}
	if d.restartRequired {
		var bootVersion int64
		if boot := s.reg.boot.Load(); boot != nil {
			bootVersion = (*boot)[d.key].version
		}
		v.RestartPending = v.Version != bootVersion
	}
	return v
}

// Change carries what a caller supplies with a change. The actor comes from
// the context.
type Change struct {
	// Version is the View.Version the caller last read.
	Version int64
	// Reason explains the change; required for ReasonRequired settings.
	Reason string
}

// Set validates value (JSON, such as `"30m"` or `42`) and stores it for key.
// The change applies to this instance immediately and to others within
// moments. It returns [ErrUnknownSetting], an [*InvalidValueError],
// [ErrReasonRequired], [ErrActorRequired] or [ErrVersionConflict]. Setting
// the current value again changes nothing.
func (s *Store) Set(ctx context.Context, key string, value json.RawMessage, change Change) (View, error) {
	d, ok := s.reg.lookup(key)
	if !ok {
		return View{}, ErrUnknownSetting
	}
	v, err := d.parse(value)
	if err != nil {
		return View{}, &InvalidValueError{Key: key, Reason: err.Error()}
	}
	return s.write(ctx, d, "", d.encode(v), change)
}

// Reset returns key to its default. It returns the same errors as
// [Store.Set], except for invalid values.
func (s *Store) Reset(ctx context.Context, key string, change Change) (View, error) {
	d, ok := s.reg.lookup(key)
	if !ok {
		return View{}, ErrUnknownSetting
	}
	return s.write(ctx, d, "", nil, change)
}

// write stores newRaw (nil for the default) with a version check, history row
// and notification in one transaction: platform-wide when orgID is empty,
// else for that organisation.
func (s *Store) write(ctx context.Context, d *definition, orgID string, newRaw json.RawMessage, change Change) (View, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind == actor.KindAnonymous || a.ID == "" {
		return View{}, ErrActorRequired
	}
	if d.reasonRequired && strings.TrimSpace(change.Reason) == "" {
		return View{}, ErrReasonRequired
	}

	var (
		result  valueRow
		changed bool
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var current valueRow
		var err error
		if orgID != "" {
			current, _, err = selectOrgValueForUpdate(ctx, tx, orgID, d.key)
		} else {
			current, _, err = selectValueForUpdate(ctx, tx, d.key)
		}
		if err != nil {
			return err
		}
		if change.Version != current.version {
			return ErrVersionConflict
		}
		if s.sameValue(d, current.value, newRaw) {
			result = current
			return nil
		}
		if current.version == 0 {
			result, err = insertValue(ctx, tx, d.key, orgID, newRaw, a.ID)
		} else {
			result, err = updateValue(ctx, tx, d.key, orgID, newRaw, a.ID)
		}
		if err != nil {
			return err
		}
		err = insertHistory(ctx, tx, historyRow{
			key:       d.key,
			orgID:     orgID,
			oldValue:  current.value,
			newValue:  newRaw,
			version:   result.version,
			reason:    strings.TrimSpace(change.Reason),
			actorKind: string(a.Kind),
			actorID:   a.ID,
			requestID: requestid.From(ctx),
		})
		if err != nil {
			return err
		}
		changed = true
		return notify(ctx, tx, d.key, orgID)
	})
	if errors.Is(err, ErrVersionConflict) {
		return View{}, err
	}
	if err != nil {
		return View{}, fmt.Errorf("settings: save %s: %v", d.key, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	if changed {
		if orgID != "" {
			s.reg.applyOrg(orgID, d.key, s.decodeRow(ctx, d, result))
		} else {
			s.reg.apply(d.key, s.decodeRow(ctx, d, result))
		}
		s.recordChange(ctx, d, orgID, result.version, newRaw == nil, change)
	}
	if orgID != "" {
		return s.orgView(d, orgID), nil
	}
	return s.view(d), nil
}

// recordChange records a committed change as a "settings.value.changed"
// audit event; organisation values carry the organisation in the event and
// its metadata.
func (s *Store) recordChange(ctx context.Context, d *definition, orgID string, version int64, reset bool, change Change) {
	event := audit.Event{
		Action:       "settings.value.changed",
		ResourceType: "setting",
		ResourceID:   d.key,
		OrgID:        orgID,
		Outcome:      audit.OutcomeSuccess,
		Metadata:     map[string]any{"version": version, "reset": reset, "reason": strings.TrimSpace(change.Reason)},
	}
	if orgID != "" {
		event.Metadata["org_id"] = orgID
	}
	// The change is committed; a failed audit write must not report failure.
	if err := s.recorder.Record(context.WithoutCancel(ctx), event); err != nil {
		s.logger.ErrorContext(ctx, "record setting change audit event", "setting", d.key, "org_id", orgID, "version", version, "err", err)
	}
}

// sameValue compares stored JSON semantically: PostgreSQL reformats jsonb.
func (s *Store) sameValue(d *definition, a, b json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	va, errA := d.decode(a)
	vb, errB := d.decode(b)
	return errA == nil && errB == nil && reflect.DeepEqual(va, vb)
}

// HistoryEntry is one change to a setting. A nil value means the default.
type HistoryEntry struct {
	ID  int64
	Key string
	// OrgID is the organisation whose value changed; empty for a
	// platform-wide change.
	OrgID     string
	OldValue  json.RawMessage
	NewValue  json.RawMessage
	Version   int64
	Reason    string
	ActorKind actor.Kind
	ActorID   string
	RequestID string
	ChangedAt time.Time
}

// History returns changes to key, newest first. For the next page, pass the
// last entry's ID as before (0 starts from the newest). limit is clamped to
// 1–100.
func (s *Store) History(ctx context.Context, key string, before int64, limit int) ([]HistoryEntry, error) {
	if _, ok := s.reg.lookup(key); !ok {
		return nil, ErrUnknownSetting
	}
	limit = min(max(limit, 1), 100)
	entries, err := selectHistory(ctx, s.pool, key, "", before, limit)
	if err != nil {
		return nil, fmt.Errorf("settings: history %s: %v", key, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	return entries, nil
}
