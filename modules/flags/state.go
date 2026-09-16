package flags

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Bounds of a [State], checked by [Store.Set] and when states load.
const (
	// MaxTargets bounds each allow and deny list. Longer lists belong in a
	// rollout percentage, or in the app's own data.
	MaxTargets = 1000
	// MaxIDLen bounds an organisation or user ID in a list.
	MaxIDLen = 100
)

// State is how a flag decides, in the order of the rules in the package
// documentation.
type State struct {
	// Enabled false turns the flag off for everyone, whatever the other
	// fields say: a kill switch that keeps the targeting for later.
	Enabled bool
	// Default is the answer when no other rule applies.
	Default bool
	// Orgs and Users list IDs that always get false (Deny) or true (Allow).
	// An ID can't be in both lists of the same kind.
	Orgs  Targets
	Users Targets
	// Percentage, from 0 to 100, turns the flag on for that share of
	// subjects (see [Bucket]); nil means no rollout, so Default decides.
	Percentage *int
}

// Targets are IDs that get a fixed answer.
type Targets struct {
	Allow []string
	Deny  []string
}

// Percent returns a pointer to p, for [State.Percentage].
func Percent(p int) *int { return &p }

// stateJSON is a state's stored form. Unknown fields are ignored when
// loading, so an instance of an earlier release keeps working with a state a
// later release wrote.
type stateJSON struct {
	Enabled    bool        `json:"enabled"`
	Default    bool        `json:"default"`
	Orgs       targetsJSON `json:"orgs"`
	Users      targetsJSON `json:"users"`
	Percentage *int        `json:"percentage"`
}

type targetsJSON struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// normalize returns s with every list sorted, without duplicates and never
// nil, so equal states compare and store equally.
func (s State) normalize() State {
	norm := func(ids []string) []string {
		out := slices.Clone(ids)
		slices.Sort(out)
		return append([]string{}, slices.Compact(out)...)
	}
	s.Orgs = Targets{Allow: norm(s.Orgs.Allow), Deny: norm(s.Orgs.Deny)}
	s.Users = Targets{Allow: norm(s.Users.Allow), Deny: norm(s.Users.Deny)}
	if s.Percentage != nil {
		s.Percentage = Percent(*s.Percentage)
	}
	return s
}

// validate checks a normalised state against the bounds. The error never
// contains an ID.
func (s State) validate() error {
	if s.Percentage != nil && (*s.Percentage < 0 || *s.Percentage > 100) {
		return errors.New("percentage must be between 0 and 100")
	}
	lists := []struct {
		name string
		ids  []string
	}{
		{"orgs.allow", s.Orgs.Allow}, {"orgs.deny", s.Orgs.Deny},
		{"users.allow", s.Users.Allow}, {"users.deny", s.Users.Deny},
	}
	for _, l := range lists {
		if len(l.ids) > MaxTargets {
			return fmt.Errorf("%s has %d IDs; the limit is %d", l.name, len(l.ids), MaxTargets)
		}
		for i, id := range l.ids {
			if !validID(id) {
				return fmt.Errorf("%s[%d] must be 1 to %d visible ASCII characters", l.name, i, MaxIDLen)
			}
		}
	}
	if overlaps(s.Orgs) || overlaps(s.Users) {
		return errors.New("an ID can't be in both the allow and deny list")
	}
	return nil
}

func validID(id string) bool {
	if id == "" || len(id) > MaxIDLen {
		return false
	}
	for i := range len(id) {
		if c := id[i]; c <= ' ' || c > '~' {
			return false
		}
	}
	return true
}

// overlaps reports an ID in both sorted lists.
func overlaps(t Targets) bool {
	for _, id := range t.Allow {
		if _, found := slices.BinarySearch(t.Deny, id); found {
			return true
		}
	}
	return false
}

// encodeState returns the stored form of a normalised state.
func encodeState(s State) json.RawMessage {
	b, err := json.Marshal(stateJSON{
		Enabled: s.Enabled, Default: s.Default, Percentage: s.Percentage,
		Orgs:  targetsJSON{Allow: s.Orgs.Allow, Deny: s.Orgs.Deny},
		Users: targetsJSON{Allow: s.Users.Allow, Deny: s.Users.Deny},
	})
	if err != nil {
		panic(fmt.Sprintf("flags: encode state: %v", err)) // only strings, bools and ints
	}
	return b
}

// decodeState parses and validates a stored state.
func decodeState(raw json.RawMessage) (State, error) {
	var j stateJSON
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&j); err != nil {
		return State{}, errors.New("state must be a JSON object of a flag state")
	}
	s := State{
		Enabled: j.Enabled, Default: j.Default, Percentage: j.Percentage,
		Orgs:  Targets{Allow: j.Orgs.Allow, Deny: j.Orgs.Deny},
		Users: Targets{Allow: j.Users.Allow, Deny: j.Users.Deny},
	}.normalize()
	if err := s.validate(); err != nil {
		return State{}, err
	}
	return s, nil
}

// clone returns a copy of s the caller may modify.
func (s State) clone() State {
	s.Orgs = Targets{Allow: slices.Clone(s.Orgs.Allow), Deny: slices.Clone(s.Orgs.Deny)}
	s.Users = Targets{Allow: slices.Clone(s.Users.Allow), Deny: slices.Clone(s.Users.Deny)}
	if s.Percentage != nil {
		s.Percentage = Percent(*s.Percentage)
	}
	return s
}

// compiled is a normalised, valid state with sets for evaluation.
type compiled struct {
	state      State
	enabled    bool
	def        bool
	percentage int // -1: no rollout
	orgAllow   map[string]struct{}
	orgDeny    map[string]struct{}
	userAllow  map[string]struct{}
	userDeny   map[string]struct{}
}

func compile(s State) *compiled {
	s = s.normalize()
	c := &compiled{
		state: s, enabled: s.Enabled, def: s.Default, percentage: -1,
		orgAllow: set(s.Orgs.Allow), orgDeny: set(s.Orgs.Deny),
		userAllow: set(s.Users.Allow), userDeny: set(s.Users.Deny),
	}
	if s.Percentage != nil {
		c.percentage = *s.Percentage
	}
	return c
}

func set(ids []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}
