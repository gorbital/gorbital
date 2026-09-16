// Package settings provides runtime settings: non-secret tunables declared in
// Go with a default and bounds, stored in PostgreSQL only when changed, and
// applied on every instance without a restart (ADR-0031).
//
// Secrets and infrastructure (database URL, API keys, listen addresses) stay
// in the environment (ADR-0020); they can't be declared here.
//
//	reg := settings.NewRegistry()
//	codeTTL := settings.Duration(reg, "auth.verification_code_ttl", 15*time.Minute,
//		settings.Describe("How long email verification codes stay valid."),
//		settings.Range(5*time.Minute, time.Hour),
//		settings.ReasonRequired(),
//	)
//	store, err := settings.NewStore(ctx, pool, reg, recorder)
//	// Run store with the app's runners; pass codeTTL, a config.Value, to modules.
//
// A [Store] loads every stored value at startup, applies changes made through
// [Store.Set] and [Store.Reset] immediately, and learns about changes made by
// other instances through PostgreSQL LISTEN/NOTIFY, with a periodic full
// reload as a fallback.
//
// Stability: stable (ADR-0015, ADR-0054).
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"gorbital.dev/config"
)

// Kind is a setting's value type.
type Kind string

// Setting kinds.
const (
	KindBool       Kind = "bool"
	KindInt        Kind = "int"
	KindFloat      Kind = "float"
	KindString     Kind = "string"
	KindEnum       Kind = "enum"
	KindDuration   Kind = "duration"
	KindStringList Kind = "string_list"
)

// keyPattern matches keys such as auth.verification_code_ttl, the same
// shape as audit action names.
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

type definition struct {
	key             string
	kind            Kind
	description     string
	group           string
	reasonRequired  bool
	restartRequired bool
	def             any
	decode          func(json.RawMessage) (any, error)
	encode          func(any) json.RawMessage
	validators      []func(any) error
	constraints     map[string]any
}

// parse decodes raw and runs every validator.
func (d *definition) parse(raw json.RawMessage) (any, error) {
	v, err := d.decode(raw)
	if err != nil {
		return nil, err
	}
	for _, validate := range d.validators {
		if err := validate(v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// stored is a setting's persisted state. A nil value means the default.
type stored struct {
	value     any
	raw       json.RawMessage
	version   int64
	updatedAt time.Time
	updatedBy string
	invalid   bool
}

// A Registry holds declared settings and their current values. Declare every
// setting before calling [NewStore]. A Registry is safe for concurrent use.
type Registry struct {
	mu     sync.Mutex
	defs   map[string]*definition
	keys   []string
	frozen bool

	snapMu  sync.Mutex
	live    atomic.Pointer[map[string]stored]
	boot    atomic.Pointer[map[string]stored]
	unknown atomic.Pointer[[]string]
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: make(map[string]*definition)}
}

// A Setting is a typed handle to one declared setting. It implements
// [config.Value], so library modules can accept it for live options.
type Setting[T any] struct {
	reg *Registry
	def *definition
}

var _ config.Value[int] = (*Setting[int])(nil)

// Key returns the setting's key.
func (s *Setting[T]) Key() string { return s.def.key }

// Get returns the setting's current value. It returns the default while the
// setting is unchanged, when its stored value fails validation, and before a
// store has loaded. Settings declared with [RestartRequired] keep the value
// loaded at startup. Get never touches the database.
func (s *Setting[T]) Get(context.Context) T {
	snap := s.reg.live.Load()
	if s.def.restartRequired {
		snap = s.reg.boot.Load()
	}
	if snap != nil {
		if st, ok := (*snap)[s.def.key]; ok && st.value != nil {
			return cloneValue(st.value).(T)
		}
	}
	return cloneValue(s.def.def).(T)
}

func cloneValue(v any) any {
	if list, ok := v.([]string); ok {
		return slices.Clone(list)
	}
	return v
}

// declare registers a setting. Invalid declarations are programming errors
// found at startup, so they panic.
func declare[T any](r *Registry, key string, kind Kind, def T,
	decode func(json.RawMessage) (T, error), encode func(T) json.RawMessage, opts []Option,
) *Setting[T] {
	if !keyPattern.MatchString(key) {
		panic(fmt.Sprintf("settings: key %q must be dotted lowercase, like module.name", key))
	}
	d := &definition{
		key:  key,
		kind: kind,
		def:  def,
		decode: func(raw json.RawMessage) (any, error) {
			v, err := decode(raw)
			if err != nil {
				return nil, err
			}
			return v, nil
		},
		encode:      func(v any) json.RawMessage { return encode(v.(T)) },
		constraints: make(map[string]any),
	}
	for _, opt := range opts {
		if err := opt.apply(d); err != nil {
			panic(fmt.Sprintf("settings: %s: %v", key, err))
		}
	}
	if _, err := d.parse(encode(def)); err != nil {
		panic(fmt.Sprintf("settings: %s: default is invalid: %v", key, err))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		panic(fmt.Sprintf("settings: %s: declared after NewStore; declare every setting first", key))
	}
	if _, dup := r.defs[key]; dup {
		panic(fmt.Sprintf("settings: %s: declared twice", key))
	}
	r.defs[key] = d
	r.keys = append(r.keys, key)
	return &Setting[T]{reg: r, def: d}
}

func (r *Registry) freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

func (r *Registry) lookup(key string) (*definition, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.defs[key]
	return d, ok
}

// Keys returns the key of every declared setting, in declaration order.
// Setting keys are public API (ADR-0015); apps record them in their surface
// inventory (ADR-0054).
func (r *Registry) Keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.keys)
}

// definitions returns every definition in declaration order.
func (r *Registry) definitions() []*definition {
	r.mu.Lock()
	defer r.mu.Unlock()
	defs := make([]*definition, len(r.keys))
	for i, key := range r.keys {
		defs[i] = r.defs[key]
	}
	return defs
}

// apply stores one setting's state unless a newer version is already known,
// so a late notification never overwrites a newer change.
func (r *Registry) apply(key string, st stored) {
	r.snapMu.Lock()
	defer r.snapMu.Unlock()
	next := make(map[string]stored)
	if cur := r.live.Load(); cur != nil {
		if old, ok := (*cur)[key]; ok && old.version > st.version {
			return
		}
		maps.Copy(next, *cur)
	}
	next[key] = st
	r.live.Store(&next)
}

// replaceAll installs a full reload, keeping any newer version already
// applied, and records the first load as the startup snapshot.
func (r *Registry) replaceAll(loaded map[string]stored, unknown []string) {
	r.snapMu.Lock()
	defer r.snapMu.Unlock()
	if cur := r.live.Load(); cur != nil {
		for key, old := range *cur {
			if st, ok := loaded[key]; !ok || old.version > st.version {
				loaded[key] = old
			}
		}
	}
	r.live.Store(&loaded)
	if r.boot.Load() == nil {
		boot := maps.Clone(loaded)
		r.boot.Store(&boot)
	}
	r.unknown.Store(&unknown)
}
