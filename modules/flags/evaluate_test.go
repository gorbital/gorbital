package flags

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"gorbital.dev/actor"
)

// withState declares a flag and applies state to it in memory, as a store
// would after loading it.
func withState(t *testing.T, key string, state State) *Flag {
	t.Helper()
	reg := NewRegistry()
	f := Bool(reg, key)
	reg.apply(key, stored{state: compile(state), version: 1})
	return f
}

func as(a actor.Actor) context.Context { return actor.With(context.Background(), a) }

func user(id string) context.Context { return as(actor.Actor{Kind: actor.KindUser, ID: id}) }

func member(id, org string) context.Context {
	return as(actor.Actor{Kind: actor.KindUser, ID: id, OrgID: org})
}

func TestBucketIsStable(t *testing.T) {
	// Pinned values: changing the hash moves every subject in every rollout,
	// which would be a breaking change (ADR-0057).
	for subject, want := range map[string]int{"usr_1": 26, "usr_2": 8, "org_1": 21, "org_2": 63} {
		if got := Bucket("checkout.new_flow", subject); got != want {
			t.Errorf("Bucket(checkout.new_flow, %s) = %d, want %d", subject, got, want)
		}
	}
}

func TestRulePrecedence(t *testing.T) {
	targeted := State{
		Enabled: true,
		Orgs:    Targets{Allow: []string{"org_allowed"}, Deny: []string{"org_denied"}},
		Users:   Targets{Allow: []string{"usr_allowed"}, Deny: []string{"usr_denied"}},
		// Every subject is in a 100% rollout, so reaching this rule shows.
		Percentage: Percent(100),
	}
	disabled := targeted
	disabled.Enabled = false
	noRollout := targeted
	noRollout.Percentage = nil
	defaultOn := noRollout
	defaultOn.Default = true

	tests := []struct {
		name       string
		state      State
		ctx        context.Context
		want       bool
		wantReason Reason
	}{
		{"disabled beats an allowed organisation", disabled, member("usr_allowed", "org_allowed"), false, ReasonDisabled},
		{"organisation deny beats user allow", targeted, member("usr_allowed", "org_denied"), false, ReasonOrgDenied},
		{"organisation allow beats user deny", targeted, member("usr_denied", "org_allowed"), true, ReasonOrgAllowed},
		{"user deny beats the rollout", targeted, member("usr_denied", "org_other"), false, ReasonUserDenied},
		{"user allow beats the rollout", withPercentage(targeted, 0), user("usr_allowed"), true, ReasonUserAllowed},
		{"organisation lists ignore users outside organisations", withPercentage(targeted, 0), user("org_allowed"), false, ReasonRollout},
		{"rollout", targeted, user("usr_other"), true, ReasonRollout},
		{"default off", noRollout, user("usr_other"), false, ReasonDefault},
		{"default on", defaultOn, user("usr_other"), true, ReasonDefault},
		{"service accounts are targeted by ID", noRollout, as(actor.Actor{Kind: actor.KindService, ID: "usr_allowed"}), true, ReasonUserAllowed},
		{"anonymous callers aren't users", noRollout, as(actor.Actor{Kind: actor.KindAnonymous, ID: "usr_allowed"}), false, ReasonDefault},
		{"no actor", noRollout, context.Background(), false, ReasonDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := withState(t, "checkout.new_flow", tt.state).Evaluate(tt.ctx)
			if e.Enabled != tt.want || e.Reason != tt.wantReason || e.Key != "checkout.new_flow" {
				t.Errorf("Evaluate() = %+v, want %v by %s", e, tt.want, tt.wantReason)
			}
		})
	}
}

func withPercentage(s State, p int) State {
	s.Percentage = Percent(p)
	return s
}

func TestDeclaredDefaults(t *testing.T) {
	reg := NewRegistry()
	off := Bool(reg, "checkout.new_flow")
	on := Bool(reg, "search.enabled", DefaultOn())
	ctx := user("usr_1")
	if e := off.Evaluate(ctx); e.Enabled || e.Reason != ReasonDisabled {
		t.Errorf("undeclared-default flag = %+v, want disabled", e)
	}
	if e := on.Evaluate(ctx); !e.Enabled || e.Reason != ReasonDefault || !on.Get(ctx) {
		t.Errorf("DefaultOn flag = %+v, want on by default", e)
	}
}

func TestRolloutIsDeterministic(t *testing.T) {
	f := withState(t, "checkout.new_flow", State{Enabled: true, Percentage: Percent(50)})
	for i := range 200 {
		id := fmt.Sprintf("usr_%d", i)
		first := f.Enabled(user(id))
		if first != (Bucket("checkout.new_flow", id) < 50) {
			t.Fatalf("%s: Enabled() = %v, not what its bucket says", id, first)
		}
		for range 3 {
			if f.Enabled(user(id)) != first {
				t.Fatalf("%s: Enabled() changed between calls", id)
			}
		}
	}
	// Another instance, loading the same state, gives the same answers.
	other := withState(t, "checkout.new_flow", State{Enabled: true, Percentage: Percent(50)})
	for i := range 200 {
		ctx := user(fmt.Sprintf("usr_%d", i))
		if f.Enabled(ctx) != other.Enabled(ctx) {
			t.Fatalf("usr_%d: instances disagree", i)
		}
	}
}

// TestRolloutDistribution checks, over 10 000 subjects, that each
// percentage turns the flag on for that share within 1.5 points (more than
// four standard deviations at 50%), that raising it only adds subjects, and
// that two flags pick independent subjects.
func TestRolloutDistribution(t *testing.T) {
	const subjects = 10000
	ids := make([]string, subjects)
	for i := range ids {
		ids[i] = fmt.Sprintf("usr_%08d", i)
	}
	previous := make([]bool, subjects)
	for _, p := range []int{0, 1, 10, 25, 50, 75, 90, 99, 100} {
		f := withState(t, "checkout.new_flow", State{Enabled: true, Percentage: Percent(p)})
		on := 0
		for i, id := range ids {
			got := f.Enabled(user(id))
			if got {
				on++
			}
			if previous[i] && !got {
				t.Fatalf("%s was in the %d%% rollout but not in a larger one", id, p)
			}
			previous[i] = got
		}
		if share := float64(on) * 100 / subjects; math.Abs(share-float64(p)) > 1.5 {
			t.Errorf("%d%% rollout turned the flag on for %.2f%% of %d subjects", p, share, subjects)
		}
	}

	a := withState(t, "checkout.new_flow", State{Enabled: true, Percentage: Percent(50)})
	b := withState(t, "search.new_ranking", State{Enabled: true, Percentage: Percent(50)})
	both := 0
	for _, id := range ids {
		if a.Enabled(user(id)) && b.Enabled(user(id)) {
			both++
		}
	}
	if share := float64(both) * 100 / subjects; math.Abs(share-25) > 1.5 {
		t.Errorf("%.2f%% of subjects are in both 50%% rollouts, want about 25%%: flags must bucket independently", share)
	}
}

// TestOrganisationIsTheSubject checks that members acting in an organisation
// share its answer, and outside it get their own.
func TestOrganisationIsTheSubject(t *testing.T) {
	f := withState(t, "checkout.new_flow", State{Enabled: true, Percentage: Percent(50)})
	// org_1's bucket is 21 and org_2's is 63 (TestBucketIsStable).
	for i := range 100 {
		id := fmt.Sprintf("usr_%d", i)
		if !f.Enabled(member(id, "org_1")) || f.Enabled(member(id, "org_2")) {
			t.Fatalf("%s: in org_1 = %v, in org_2 = %v; want the organisations' answers", id, f.Enabled(member(id, "org_1")), f.Enabled(member(id, "org_2")))
		}
		if got, want := f.Enabled(user(id)), Bucket("checkout.new_flow", id) < 50; got != want {
			t.Fatalf("%s outside organisations = %v, want its own bucket's %v", id, got, want)
		}
	}
}

func TestAnonymousCallersGetOnlyWholeRollouts(t *testing.T) {
	anon := as(actor.Anonymous)
	for _, tt := range []struct {
		state      State
		want       bool
		wantReason Reason
	}{
		{State{Enabled: true, Percentage: Percent(0), Default: true}, false, ReasonRollout},
		{State{Enabled: true, Percentage: Percent(100)}, true, ReasonRollout},
		{State{Enabled: true, Percentage: Percent(50)}, false, ReasonDefault},
		{State{Enabled: true, Percentage: Percent(50), Default: true}, true, ReasonDefault},
		{State{Enabled: true, Percentage: Percent(99), Users: Targets{Allow: []string{""}}}, false, ReasonDefault},
	} {
		for _, ctx := range []context.Context{anon, context.Background()} {
			if e := withState(t, "checkout.new_flow", tt.state).Evaluate(ctx); e.Enabled != tt.want || e.Reason != tt.wantReason {
				t.Errorf("anonymous with percentage %d, default %v = %+v, want %v by %s", *tt.state.Percentage, tt.state.Default, e, tt.want, tt.wantReason)
			}
		}
	}
}

func TestStateBounds(t *testing.T) {
	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("usr_%d", i)
		}
		return out
	}
	valid := []State{
		{},
		{Enabled: true, Percentage: Percent(0)},
		{Enabled: true, Percentage: Percent(100)},
		{Orgs: Targets{Allow: ids(MaxTargets)}, Users: Targets{Deny: ids(MaxTargets)}},
		{Users: Targets{Allow: []string{strings.Repeat("x", MaxIDLen)}}},
		// Duplicates are removed, not refused.
		{Users: Targets{Allow: []string{"usr_1", "usr_1"}}},
	}
	for _, s := range valid {
		if err := s.normalize().validate(); err != nil {
			t.Errorf("validate(%+v) error = %v", s, err)
		}
	}
	invalid := []struct {
		state State
		want  string
	}{
		{State{Percentage: Percent(-1)}, "percentage"},
		{State{Percentage: Percent(101)}, "percentage"},
		{State{Orgs: Targets{Allow: ids(MaxTargets + 1)}}, "orgs.allow has 1001 IDs"},
		{State{Users: Targets{Deny: ids(MaxTargets + 1)}}, "users.deny has 1001 IDs"},
		{State{Users: Targets{Allow: []string{""}}}, "users.allow[0]"},
		{State{Users: Targets{Allow: []string{"usr secret"}}}, "users.allow[0]"},
		{State{Orgs: Targets{Deny: []string{strings.Repeat("x", MaxIDLen+1)}}}, "orgs.deny[0]"},
		{State{Users: Targets{Allow: []string{"usr_secret"}, Deny: []string{"usr_secret"}}}, "both"},
	}
	for _, tt := range invalid {
		err := tt.state.normalize().validate()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("validate(%+v) error = %v, want one mentioning %q", tt.state, err, tt.want)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("validate() error %q contains an ID", err)
		}
	}
}

func TestDeclarationsPanic(t *testing.T) {
	reg := NewRegistry()
	Bool(reg, "checkout.new_flow")
	for name, declare := range map[string]func(){
		"a key without a module": func() { Bool(reg, "newflow") },
		"an uppercase key":       func() { Bool(reg, "Checkout.new_flow") },
		"a duplicate key":        func() { Bool(reg, "checkout.new_flow") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("declaring %s didn't panic", name)
				}
			}()
			declare()
		}()
	}
	if keys := reg.Keys(); len(keys) != 1 || keys[0] != "checkout.new_flow" {
		t.Errorf("Keys() = %v", keys)
	}
}

func BenchmarkEnabled(b *testing.B) {
	reg := NewRegistry()
	f := Bool(reg, "checkout.new_flow")
	reg.apply("checkout.new_flow", stored{version: 1, state: compile(State{
		Enabled: true, Percentage: Percent(50),
		Orgs: Targets{Allow: []string{"org_1"}}, Users: Targets{Deny: []string{"usr_2"}},
	})})
	ctx := member("usr_1", "org_2")
	b.ReportAllocs()
	for b.Loop() {
		f.Enabled(ctx)
	}
}
