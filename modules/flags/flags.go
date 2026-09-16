// Package flags provides feature flags: on/off switches declared in Go,
// targeted at organisations and users, rolled out to a stable percentage of
// them, stored in PostgreSQL only when changed, and applied on every
// instance without a restart (ADR-0057).
//
//	reg := flags.NewRegistry()
//	newCheckout := flags.Bool(reg, "checkout.new_flow",
//		flags.Describe("The redesigned checkout."),
//		flags.Client(), // listed to signed-in clients by GET /v1/flags
//	)
//	store, err := flags.NewStore(ctx, pool, reg, recorder)
//	// Run store with the app's runners; ask the flag where it matters:
//	if newCheckout.Enabled(ctx) { ... }
//
// # Evaluation
//
// A flag's [State] is its declared default until an operator changes it with
// [Store.Set]. [Flag.Enabled] reads the state from memory and the actor from
// ctx (actor.From), and decides, first rule that applies:
//
//  1. The flag is disabled: false.
//  2. The actor acts in an organisation (actor.Actor.OrgID, set by
//     orgs.RequireMember): false if the organisation is in Orgs.Deny, true
//     if it is in Orgs.Allow.
//  3. The actor is authenticated (any kind but anonymous, with an ID):
//     false if its ID is in Users.Deny, true if it is in Users.Allow.
//  4. A rollout percentage is set: true when the subject's [Bucket] is below
//     it. The subject is the organisation when the actor acts in one, so
//     every member gets the same answer, and the actor's ID otherwise.
//     Anonymous callers have no subject: a percentage of 0 or 100 still
//     applies to them, and any other percentage leaves them to the default.
//  5. The state's default.
//
// Evaluation is deterministic: the same flag, state and subject always give
// the same answer, on every instance, and raising a percentage only adds
// subjects. It never touches the database.
//
// Stability: stable (ADR-0015, ADR-0054).
package flags

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/config"
)

// keyPattern matches keys such as checkout.new_flow, the same shape as
// setting keys and audit action names.
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

type definition struct {
	key         string
	description string
	group       string
	client      bool
	defaultOn   bool
	// declared is the compiled declared default state.
	declared *compiled
}

// stored is a flag's persisted state. A nil state means the declared default.
type stored struct {
	state     *compiled
	version   int64
	updatedAt time.Time
	updatedBy string
	invalid   bool
}

// A Registry holds declared flags and their current states. Declare every
// flag before calling [NewStore]. A Registry is safe for concurrent use.
type Registry struct {
	mu     sync.Mutex
	defs   map[string]*definition
	keys   []string
	frozen bool

	snapMu  sync.Mutex
	live    atomic.Pointer[map[string]stored]
	unknown atomic.Pointer[[]string]
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: make(map[string]*definition)}
}

// A Flag is a handle to one declared flag. It implements
// [config.Value][bool], so library modules can accept a flag as a live
// boolean option without importing this package.
type Flag struct {
	reg *Registry
	def *definition
}

var _ config.Value[bool] = (*Flag)(nil)

// Key returns the flag's key.
func (f *Flag) Key() string { return f.def.key }

// Enabled reports whether the flag is on for the actor in ctx, by the rules
// in the package documentation. It never touches the database.
func (f *Flag) Enabled(ctx context.Context) bool {
	return f.Evaluate(ctx).Enabled
}

// Get returns [Flag.Enabled], so a Flag is a [config.Value][bool].
func (f *Flag) Get(ctx context.Context) bool { return f.Enabled(ctx) }

// Evaluate returns the flag's answer for the actor in ctx and the rule that
// decided it.
func (f *Flag) Evaluate(ctx context.Context) Evaluation {
	return f.reg.evaluate(ctx, f.def)
}

// Reason is the rule that decided an evaluation.
type Reason string

// Evaluation reasons, in the order the rules apply.
const (
	ReasonDisabled    Reason = "disabled"
	ReasonOrgDenied   Reason = "org_denied"
	ReasonOrgAllowed  Reason = "org_allowed"
	ReasonUserDenied  Reason = "user_denied"
	ReasonUserAllowed Reason = "user_allowed"
	ReasonRollout     Reason = "rollout"
	ReasonDefault     Reason = "default"
)

// Evaluation is a flag's answer for one actor.
type Evaluation struct {
	Key     string
	Enabled bool
	Reason  Reason
}

// Bucket returns subject's rollout bucket for key, from 0 to 99: the first
// eight bytes of SHA-256 of key, a zero byte and subject, as a big-endian
// unsigned integer, modulo 100. A subject is in a rollout of p percent when
// its bucket is below p. Each flag buckets subjects independently, so the
// same 10% of users don't get every new feature first.
func Bucket(key, subject string) int {
	h := sha256.New()
	h.Write([]byte(key))
	h.Write([]byte{0})
	h.Write([]byte(subject))
	var sum [sha256.Size]byte
	return int(binary.BigEndian.Uint64(h.Sum(sum[:0])[:8]) % 100)
}

// evaluate applies the rules to the flag's current state.
func (r *Registry) evaluate(ctx context.Context, d *definition) Evaluation {
	st := d.declared
	if snap := r.live.Load(); snap != nil {
		if s, ok := (*snap)[d.key]; ok && s.state != nil {
			st = s.state
		}
	}
	enabled, reason := st.evaluate(d.key, actor.FromOrAnonymous(ctx))
	return Evaluation{Key: d.key, Enabled: enabled, Reason: reason}
}

func (c *compiled) evaluate(key string, a actor.Actor) (bool, Reason) {
	if !c.enabled {
		return false, ReasonDisabled
	}
	if a.OrgID != "" {
		if _, ok := c.orgDeny[a.OrgID]; ok {
			return false, ReasonOrgDenied
		}
		if _, ok := c.orgAllow[a.OrgID]; ok {
			return true, ReasonOrgAllowed
		}
	}
	authenticated := a.Kind != actor.KindAnonymous && a.ID != ""
	if authenticated {
		if _, ok := c.userDeny[a.ID]; ok {
			return false, ReasonUserDenied
		}
		if _, ok := c.userAllow[a.ID]; ok {
			return true, ReasonUserAllowed
		}
	}
	if c.percentage >= 0 {
		subject := a.OrgID
		if subject == "" && authenticated {
			subject = a.ID
		}
		switch {
		case subject != "":
			return Bucket(key, subject) < c.percentage, ReasonRollout
		case c.percentage == 0:
			return false, ReasonRollout
		case c.percentage == 100:
			return true, ReasonRollout
		}
	}
	return c.def, ReasonDefault
}

// Bool declares an on/off flag. It is off for everyone until declared with
// [DefaultOn] or turned on with [Store.Set]. Invalid declarations are
// programming errors found at startup, so they panic.
func Bool(r *Registry, key string, opts ...Option) *Flag {
	if !keyPattern.MatchString(key) {
		panic(fmt.Sprintf("flags: key %q must be dotted lowercase, like module.name", key))
	}
	d := &definition{key: key}
	for _, opt := range opts {
		opt.apply(d)
	}
	d.declared = compile(State{Enabled: d.defaultOn, Default: d.defaultOn})

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		panic(fmt.Sprintf("flags: %s: declared after NewStore; declare every flag first", key))
	}
	if _, dup := r.defs[key]; dup {
		panic(fmt.Sprintf("flags: %s: declared twice", key))
	}
	r.defs[key] = d
	r.keys = append(r.keys, key)
	return &Flag{reg: r, def: d}
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

// Keys returns the key of every declared flag, in declaration order. Flag
// keys are public API (ADR-0015): clients read them from GET /v1/flags, and
// apps record them in their surface inventory (ADR-0054).
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

// apply stores one flag's state unless a newer version is already known, so
// a late notification never overwrites a newer change. Flags are few, so
// copying the map per change is cheap.
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
// applied.
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
	r.unknown.Store(&unknown)
}
