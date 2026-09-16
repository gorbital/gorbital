package flags

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// DefaultResyncInterval is how often a running [Store] reloads every state,
// covering notifications lost while disconnected.
const DefaultResyncInterval = 5 * time.Minute

// Audit actions the store records. They are public API (ADR-0015).
const (
	ActionChanged = "flags.flag.changed"
	ActionReset   = "flags.flag.reset"
)

// dbtx is the query subset of *pgxpool.Pool and pgx.Tx the store uses.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store persists flag states in PostgreSQL and keeps a [Registry] current.
// It is an app.Runner: run it so changes from other instances arrive. It is
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

// WithLogger sets the logger for listener and invalid-state warnings.
// Default: discard.
func WithLogger(logger *slog.Logger) StoreOption {
	return storeOptionFunc(func(s *Store) { s.logger = logger })
}

// WithResyncInterval sets how often [Store.Run] reloads every state.
// Default: [DefaultResyncInterval].
func WithResyncInterval(d time.Duration) StoreOption {
	return storeOptionFunc(func(s *Store) { s.resync = d })
}

// NewStore loads every stored state into reg and returns the store. After
// NewStore, reg accepts no new declarations. Changes are recorded as
// "flags.flag.changed" and "flags.flag.reset" audit events through
// recorder.
//
// The flags tables come from [Migrations]; apply them first.
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
		return nil, fmt.Errorf("flags: invalid store: %w", err)
	}

	reg.freeze()
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload reads every stored state from the database.
func (s *Store) Reload(ctx context.Context) error {
	rows, err := selectStates(ctx, s.pool)
	if err != nil {
		return fmt.Errorf("flags: load: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	loaded := make(map[string]stored, len(rows))
	var unknown []string
	for _, row := range rows {
		if _, ok := s.reg.lookup(row.key); !ok {
			unknown = append(unknown, row.key)
			continue
		}
		loaded[row.key] = s.decodeRow(ctx, row)
	}
	s.reg.replaceAll(loaded, unknown)
	return nil
}

// reloadKey reloads the flag a notification names.
func (s *Store) reloadKey(ctx context.Context, key string) error {
	if _, ok := s.reg.lookup(key); !ok {
		return nil
	}
	row, found, err := selectState(ctx, s.pool, key)
	if err != nil || !found {
		return err
	}
	s.reg.apply(key, s.decodeRow(ctx, row))
	return nil
}

// decodeRow converts a row. A stored state that fails validation is marked
// invalid and logged, and the declared default applies.
func (s *Store) decodeRow(ctx context.Context, row stateRow) stored {
	st := stored{version: row.version, updatedAt: row.updatedAt, updatedBy: row.updatedBy}
	if row.state == nil {
		return st
	}
	state, err := decodeState(row.state)
	if err != nil {
		st.invalid = true
		s.logger.WarnContext(ctx, "stored flag state is invalid; using the declared default",
			"flag", row.key, "version", row.version, "reason", err.Error())
		return st
	}
	st.state = compile(state)
	return st
}

// UnknownKeys returns stored keys that no declaration matches, for example
// flags removed from the code. Their rows are kept and ignored.
func (s *Store) UnknownKeys() []string {
	if keys := s.reg.unknown.Load(); keys != nil {
		return append([]string(nil), (*keys)...)
	}
	return nil
}

// View is a flag's declaration and current state, for operator APIs.
type View struct {
	Key         string
	Group       string
	Description string
	// Client reports a flag declared with [Client].
	Client bool

	// State is the state in effect; Default is the declared state.
	State   State
	Default State
	// Modified reports whether a valid stored state replaces the declared
	// one.
	Modified bool
	// InvalidStoredValue reports a stored state that fails validation, so
	// the declared state is in effect.
	InvalidStoredValue bool

	// Version increases with every change; pass it back to change the flag.
	// It is 0 for a flag that has never been changed.
	Version   int64
	UpdatedAt time.Time
	UpdatedBy string
}

// List returns every declared flag in declaration order. It reads memory
// only.
func (s *Store) List() []View {
	defs := s.reg.definitions()
	views := make([]View, len(defs))
	for i, d := range defs {
		views[i] = s.view(d)
	}
	return views
}

// Get returns one flag, or [ErrUnknownFlag].
func (s *Store) Get(key string) (View, error) {
	d, ok := s.reg.lookup(key)
	if !ok {
		return View{}, ErrUnknownFlag
	}
	return s.view(d), nil
}

func (s *Store) view(d *definition) View {
	group := d.group
	if group == "" {
		group, _, _ = strings.Cut(d.key, ".")
	}
	v := View{
		Key:         d.key,
		Group:       group,
		Description: d.description,
		Client:      d.client,
		State:       d.declared.state.clone(),
		Default:     d.declared.state.clone(),
	}
	if live := s.reg.live.Load(); live != nil {
		if st, ok := (*live)[d.key]; ok {
			v.Version, v.UpdatedAt, v.UpdatedBy, v.InvalidStoredValue = st.version, st.updatedAt, st.updatedBy, st.invalid
			if st.state != nil {
				v.State, v.Modified = st.state.state.clone(), true
			}
		}
	}
	return v
}

// ClientFlags evaluates every flag declared with [Client] for the actor in
// ctx, in declaration order, for endpoints that tell clients which features
// they get. It reads memory only.
func (s *Store) ClientFlags(ctx context.Context) []Evaluation {
	var out []Evaluation
	for _, d := range s.reg.definitions() {
		if d.client {
			out = append(out, s.reg.evaluate(ctx, d))
		}
	}
	return out
}

// Change carries what a caller supplies with a change. The actor comes from
// the context.
type Change struct {
	// Version is the View.Version the caller last read.
	Version int64
	// Reason explains the change; always required.
	Reason string
}

// Set validates state and stores it for key. The change applies to this
// instance immediately and to others within moments. It returns
// [ErrUnknownFlag], an [*InvalidStateError], [ErrReasonRequired],
// [ErrActorRequired] or [ErrVersionConflict]. Setting the current state
// again changes nothing. Lists are stored sorted and without duplicates.
func (s *Store) Set(ctx context.Context, key string, state State, change Change) (View, error) {
	d, ok := s.reg.lookup(key)
	if !ok {
		return View{}, ErrUnknownFlag
	}
	state = state.normalize()
	if err := state.validate(); err != nil {
		return View{}, &InvalidStateError{Key: key, Reason: err.Error()}
	}
	return s.write(ctx, d, &state, change)
}

// Reset returns key to its declared state. It returns the same errors as
// [Store.Set], except for invalid states.
func (s *Store) Reset(ctx context.Context, key string, change Change) (View, error) {
	d, ok := s.reg.lookup(key)
	if !ok {
		return View{}, ErrUnknownFlag
	}
	return s.write(ctx, d, nil, change)
}

// write stores state (nil for the declared default) with a version check,
// history row and notification in one transaction.
func (s *Store) write(ctx context.Context, d *definition, state *State, change Change) (View, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind == actor.KindAnonymous || a.ID == "" {
		return View{}, ErrActorRequired
	}
	reason := strings.TrimSpace(change.Reason)
	if reason == "" {
		return View{}, ErrReasonRequired
	}
	var newRaw json.RawMessage
	if state != nil {
		newRaw = encodeState(*state)
	}

	var (
		result  stateRow
		changed bool
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		current, _, err := selectStateForUpdate(ctx, tx, d.key)
		if err != nil {
			return err
		}
		if change.Version != current.version {
			return ErrVersionConflict
		}
		if sameState(current.state, newRaw) {
			result = current
			return nil
		}
		if current.version == 0 {
			result, err = insertState(ctx, tx, d.key, newRaw, a.ID)
		} else {
			result, err = updateState(ctx, tx, d.key, newRaw, a.ID)
		}
		if err != nil {
			return err
		}
		err = insertHistory(ctx, tx, historyRow{
			key:       d.key,
			oldState:  current.state,
			newState:  newRaw,
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
		return notify(ctx, tx, d.key)
	})
	if errors.Is(err, ErrVersionConflict) {
		return View{}, err
	}
	if err != nil {
		return View{}, fmt.Errorf("flags: save %s: %v", d.key, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	if changed {
		s.reg.apply(d.key, s.decodeRow(ctx, result))
		s.recordChange(ctx, d, result.version, state, reason)
	}
	return s.view(d), nil
}

// recordChange records a committed change as a "flags.flag.changed" or,
// for a reset, "flags.flag.reset" audit event. The metadata summarises the
// state; the IDs in its lists are in the flag's history.
func (s *Store) recordChange(ctx context.Context, d *definition, version int64, state *State, reason string) {
	event := audit.Event{
		Action:       "flags.flag.reset", // ActionReset
		ResourceType: "flag",
		ResourceID:   d.key,
		Outcome:      audit.OutcomeSuccess,
		Metadata:     map[string]any{"version": version, "reason": reason},
	}
	if state != nil {
		event = audit.Event{
			Action:       "flags.flag.changed", // ActionChanged
			ResourceType: "flag",
			ResourceID:   d.key,
			Outcome:      audit.OutcomeSuccess,
			Metadata: map[string]any{
				"version":      version,
				"reason":       reason,
				"enabled":      state.Enabled,
				"default":      state.Default,
				"org_targets":  len(state.Orgs.Allow) + len(state.Orgs.Deny),
				"user_targets": len(state.Users.Allow) + len(state.Users.Deny),
			},
		}
		if state.Percentage != nil {
			event.Metadata["percentage"] = *state.Percentage
		}
	}
	// The change is committed; a failed audit write must not report failure.
	if err := s.recorder.Record(context.WithoutCancel(ctx), event); err != nil {
		s.logger.ErrorContext(ctx, "record flag change audit event", "flag", d.key, "version", version, "err", err)
	}
}

// sameState compares stored JSON semantically: PostgreSQL reformats jsonb.
func sameState(a, b json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	sa, errA := decodeState(a)
	sb, errB := decodeState(b)
	return errA == nil && errB == nil && reflect.DeepEqual(sa, sb)
}

// HistoryEntry is one change to a flag. A nil state means the declared
// default.
type HistoryEntry struct {
	ID       int64
	Key      string
	OldState *State
	NewState *State
	// OldInvalid and NewInvalid report a recorded state that no longer
	// passes validation, returned as nil.
	OldInvalid bool
	NewInvalid bool
	Version    int64
	Reason     string
	ActorKind  actor.Kind
	ActorID    string
	RequestID  string
	ChangedAt  time.Time
}

// History returns changes to key, newest first. For the next page, pass the
// last entry's ID as before (0 starts from the newest). limit is clamped to
// 1–100.
func (s *Store) History(ctx context.Context, key string, before int64, limit int) ([]HistoryEntry, error) {
	if _, ok := s.reg.lookup(key); !ok {
		return nil, ErrUnknownFlag
	}
	limit = min(max(limit, 1), 100)
	rows, err := selectHistory(ctx, s.pool, key, before, limit)
	if err != nil {
		return nil, fmt.Errorf("flags: history %s: %v", key, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	entries := make([]HistoryEntry, len(rows))
	for i, row := range rows {
		e := row.HistoryEntry
		e.OldState, e.OldInvalid = historyState(row.oldState)
		e.NewState, e.NewInvalid = historyState(row.newState)
		entries[i] = e
	}
	return entries, nil
}

func historyState(raw []byte) (*State, bool) {
	if raw == nil {
		return nil, false
	}
	state, err := decodeState(raw)
	if err != nil {
		return nil, true
	}
	return &state, false
}
